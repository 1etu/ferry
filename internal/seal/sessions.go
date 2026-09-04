package seal

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	FrameSize       = 1 << 20
	UploadNonceSize = 16

	keySize       = 32
	sessionIDSize = 16
	idleLimit     = 7 * 24 * time.Hour
)

var (
	ErrProof     = errors.New("seal proof rejected")
	ErrConfirm   = errors.New("seal confirm rejected")
	ErrPublicKey = errors.New("seal public key invalid")
	ErrFrame     = errors.New("seal frame rejected")
	ErrString    = errors.New("sealed string rejected")
)

type Session struct {
	ID       string
	DeviceID string
	Key      [keySize]byte
}

type Handshake struct {
	Session      Session
	ServerKey    []byte
	Confirm      []byte
	DeviceSecret []byte
}

type Sessions struct {
	now      func() time.Time
	mu       sync.Mutex
	byDevice map[string]liveSession
}

type liveSession struct {
	session  Session
	lastUsed time.Time
}

func NewSessions(now func() time.Time) *Sessions {
	return &Sessions{now: now, byDevice: map[string]liveSession{}}
}

func (s *Sessions) Handshake(deviceID string, deviceSecret, clientKey, proof []byte) (Handshake, error) {
	var serverPrivate [keySize]byte
	if _, err := rand.Read(serverPrivate[:]); err != nil {
		return Handshake{}, fmt.Errorf("generate server key: %w", err)
	}
	var id [sessionIDSize]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Handshake{}, fmt.Errorf("generate session id: %w", err)
	}
	result, err := handshake(deviceSecret, clientKey, proof, &serverPrivate)
	if err != nil {
		return Handshake{}, err
	}
	result.Session.ID = base64.RawURLEncoding.EncodeToString(id[:])
	result.Session.DeviceID = deviceID

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.dropIdle(now)
	s.byDevice[deviceID] = liveSession{session: result.Session, lastUsed: now}
	return result, nil
}

func (s *Sessions) Lookup(deviceID, sessionID string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	live, ok := s.byDevice[deviceID]
	if !ok || subtle.ConstantTimeCompare([]byte(live.session.ID), []byte(sessionID)) != 1 {
		return Session{}, false
	}
	live.lastUsed = s.now()
	s.byDevice[deviceID] = live
	return live.session, true
}

func (s *Sessions) Sealer(deviceID string) func(string) (string, bool) {
	return func(plain string) (string, bool) {
		s.mu.Lock()
		live, ok := s.byDevice[deviceID]
		s.mu.Unlock()
		if !ok {
			return "", false
		}
		sealed, err := SealString(&live.session.Key, plain)
		if err != nil {
			return "", false
		}
		return sealed, true
	}
}

func (s *Sessions) Drop(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byDevice, deviceID)
}

func (s *Sessions) dropIdle(now time.Time) {
	for deviceID, live := range s.byDevice {
		if now.Sub(live.lastUsed) > idleLimit {
			delete(s.byDevice, deviceID)
		}
	}
}
