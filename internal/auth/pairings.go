package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/store"
)

var (
	ErrInvalid     = errors.New("pairing invalid")
	ErrExpired     = errors.New("pairing expired")
	ErrRateLimited = errors.New("rate limited")
)

const (
	sessionLifetime     = 10 * time.Minute
	approvedGrace       = 24 * time.Hour
	maxWrongCodes       = 5
	redeemWindow        = time.Minute
	maxRedeemsPerWindow = 10
)

type Pairing struct {
	Token     string
	Code      string
	Secret    []byte
	ExpiresAt time.Time
}

type Redeem struct {
	Token     string
	Code      string
	Name      string
	RemoteIP  string
	HasSecret bool
}

type Pairings struct {
	store   *store.Store
	hub     *events.Hub
	origins func() (local, ip string)
	now     func() time.Time
	log     *slog.Logger

	OnRevoke func(deviceID string)

	transitions sync.Mutex

	mu             sync.Mutex
	sessions       map[sessionKey]*session
	displayed      *session
	deviceSessions map[string]deviceSession
	redeemsByIP    map[string][]time.Time
}

func NewPairings(st *store.Store, hub *events.Hub, origins func() (local, ip string), now func() time.Time, log *slog.Logger) *Pairings {
	return &Pairings{
		store:          st,
		hub:            hub,
		origins:        origins,
		now:            now,
		log:            log,
		sessions:       make(map[sessionKey]*session),
		deviceSessions: make(map[string]deviceSession),
		redeemsByIP:    make(map[string][]time.Time),
	}
}

func (p *Pairings) Current() Pairing {
	now := p.now()
	p.mu.Lock()
	p.pruneLocked(now)
	isRotated := p.displayed == nil || !p.displayed.isLive(now)
	if isRotated {
		p.rotateLocked(now)
	}
	current := p.displayed.pairing()
	p.mu.Unlock()

	if isRotated {
		p.publishPairing(current)
	}
	return current
}

func (p *Pairings) Rotate() Pairing {
	now := p.now()
	p.mu.Lock()
	p.pruneLocked(now)
	current := p.rotateLocked(now).pairing()
	p.mu.Unlock()

	p.log.Info("pairing rotated", "reason", "requested")
	p.publishPairing(current)
	return current
}

func (p *Pairings) View(pairing Pairing) api.Pairing {
	local, ip := p.origins()
	return api.Pairing{
		QRURL:     api.PairingURL(ip, pairing.Token, pairing.Secret),
		LocalURL:  api.PairingURL(local, pairing.Token, pairing.Secret),
		Code:      pairing.Code,
		ExpiresAt: api.Timestamp(pairing.ExpiresAt),
	}
}

func (p *Pairings) Redeem(ctx context.Context, r Redeem) (store.Device, string, error) {
	p.transitions.Lock()
	defer p.transitions.Unlock()

	now := p.now()
	plain, hash := NewToken()
	id, err := ulid.New(ulid.Timestamp(now), rand.Reader)
	if err != nil {
		return store.Device{}, "", fmt.Errorf("new device id: %w", err)
	}
	device := store.Device{
		ID:        id.String(),
		Name:      r.Name,
		TokenHash: hash,
		Status:    store.DevicePending,
		CreatedAt: storedTime(now),
	}

	var pairingSecret []byte
	p.mu.Lock()
	s, rotated, err := p.claimLocked(r, now)
	if err == nil {
		isByToken := r.Token != ""
		p.deviceSessions[device.ID] = deviceSession{session: s, isByToken: isByToken}
		if isByToken && s.approvedDeviceID != "" {
			if r.HasSecret {
				device.Status = store.DeviceApproved
				device.ApprovedAt = storedTime(now)
			}
			p.endGraceLocked(s)
		}
		if isByToken && r.HasSecret {
			pairingSecret = s.secret
		}
	}
	p.mu.Unlock()

	if rotated != nil {
		p.log.Info("pairing rotated", "reason", "wrong_codes")
		p.publishPairing(rotated.pairing())
	}
	if err != nil {
		return store.Device{}, "", err
	}

	if err := p.insertDevice(ctx, &device, pairingSecret); err != nil {
		p.mu.Lock()
		delete(p.deviceSessions, device.ID)
		p.mu.Unlock()
		return store.Device{}, "", fmt.Errorf("redeem pairing: %w", err)
	}

	p.log.Info("device paired", "device", device.ID, "status", string(device.Status))
	if device.Status == store.DeviceApproved {
		p.publishDevice(api.DeviceApproved, device)
	} else {
		p.publishDevice(api.DeviceRequested, device)
	}
	return device, plain, nil
}

func (p *Pairings) insertDevice(ctx context.Context, device *store.Device, pairingSecret []byte) error {
	if pairingSecret != nil {
		secret, err := seal.DeviceSecret(pairingSecret)
		if err != nil {
			return err
		}
		device.Secret = secret
	}
	return p.store.InsertDevice(ctx, *device)
}

func (p *Pairings) Approve(ctx context.Context, deviceID string) (store.Device, error) {
	p.transitions.Lock()
	defer p.transitions.Unlock()

	device, err := p.store.Device(ctx, deviceID)
	if err != nil {
		return store.Device{}, fmt.Errorf("approve: %w", err)
	}
	if device.Status == store.DeviceApproved {
		return device, nil
	}
	if device.Status == store.DeviceRevoked {
		return store.Device{}, fmt.Errorf("approve revoked device %s: %w", deviceID, store.ErrNotFound)
	}

	now := p.now()
	if err := p.store.SetDeviceStatus(ctx, deviceID, store.DeviceApproved, now); err != nil {
		return store.Device{}, fmt.Errorf("approve: %w", err)
	}
	device.Status = store.DeviceApproved
	device.ApprovedAt = storedTime(now)

	p.mu.Lock()
	rotated := p.startGraceLocked(deviceID, now)
	p.mu.Unlock()

	p.log.Info("device approved", "device", deviceID)
	p.publishDevice(api.DeviceApproved, device)
	if rotated != nil {
		p.log.Info("pairing rotated", "reason", "approved")
		p.publishPairing(rotated.pairing())
	}
	p.hub.Disconnect(deviceID)
	return device, nil
}

func (p *Pairings) Revoke(ctx context.Context, deviceID string) (store.Device, error) {
	p.transitions.Lock()
	defer p.transitions.Unlock()

	device, err := p.store.Device(ctx, deviceID)
	if err != nil {
		return store.Device{}, fmt.Errorf("revoke: %w", err)
	}
	now := p.now()
	if err := p.store.SetDeviceStatus(ctx, deviceID, store.DeviceRevoked, now); err != nil {
		return store.Device{}, fmt.Errorf("revoke: %w", err)
	}
	device.Status = store.DeviceRevoked
	if p.OnRevoke != nil {
		p.OnRevoke(deviceID)
	}

	p.mu.Lock()
	rotated := p.endSessionLocked(deviceID, now)
	p.mu.Unlock()

	p.log.Info("device revoked", "device", deviceID)
	p.publishDevice(api.DeviceRevoked, device)
	if rotated != nil {
		p.log.Info("pairing rotated", "reason", "revoked")
		p.publishPairing(rotated.pairing())
	}
	p.hub.Disconnect(deviceID)
	return device, nil
}

func (p *Pairings) publishDevice(action string, d store.Device) {
	e := events.Event{
		Kind:    events.KindDevice,
		Payload: api.DeviceChange{Action: action, Device: api.DeviceFrom(d)},
	}
	if action == api.DeviceRequested {
		e.OwnerOnly = true
	} else {
		e.DeviceID = d.ID
	}
	p.hub.Publish(e)
}

func (p *Pairings) publishPairing(pairing Pairing) {
	p.hub.Publish(events.Event{Kind: events.KindPairing, OwnerOnly: true, Payload: p.View(pairing)})
}

func storedTime(t time.Time) time.Time {
	return time.UnixMilli(t.UnixMilli()).UTC()
}
