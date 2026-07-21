package auth

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"net/netip"
	"slices"
	"time"
)

type sessionKey [sha256.Size]byte

func keyOf(token string) sessionKey {
	return sha256.Sum256([]byte(token))
}

type session struct {
	key              sessionKey
	token            string
	code             string
	secret           []byte
	expiresAt        time.Time
	approvedDeviceID string
	wrongCodes       int
	isRevoked        bool
	isGraceUsed      bool
}

type deviceSession struct {
	session   *session
	isByToken bool
}

func (s *session) isLive(now time.Time) bool {
	return !s.isRevoked && !s.isGraceUsed && now.Before(s.expiresAt)
}

func (s *session) pairing() Pairing {
	return Pairing{Token: s.token, Code: s.code, Secret: bytes.Clone(s.secret), ExpiresAt: s.expiresAt}
}

func (p *Pairings) claimLocked(r Redeem, now time.Time) (claimed, rotated *session, err error) {
	p.pruneLocked(now)
	if !p.allowRedeemLocked(r.RemoteIP, now) {
		return nil, nil, ErrRateLimited
	}
	if r.Token != "" {
		s, err := p.sessionByTokenLocked(r.Token, now)
		return s, nil, err
	}
	return p.sessionByCodeLocked(r.Code, now)
}

func (p *Pairings) sessionByTokenLocked(token string, now time.Time) (*session, error) {
	s, ok := p.sessions[keyOf(token)]
	if !ok {
		return nil, ErrInvalid
	}
	if !s.isLive(now) {
		return nil, ErrExpired
	}
	return s, nil
}

func (p *Pairings) sessionByCodeLocked(code string, now time.Time) (claimed, rotated *session, err error) {
	s := p.displayed
	if s == nil {
		return nil, nil, ErrInvalid
	}
	if !s.isLive(now) {
		return nil, nil, ErrExpired
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(s.code)) == 1 {
		return s, nil, nil
	}
	s.wrongCodes++
	if s.wrongCodes >= maxWrongCodes {
		return nil, p.rotateLocked(now), ErrInvalid
	}
	return nil, nil, ErrInvalid
}

func (p *Pairings) allowRedeemLocked(remoteIP string, now time.Time) bool {
	key := rateLimitKey(remoteIP)
	recent := p.redeemsByIP[key]
	if len(recent) >= maxRedeemsPerWindow {
		return false
	}
	p.redeemsByIP[key] = append(recent, now)
	return true
}

func rateLimitKey(remote string) string {
	if addrPort, err := netip.ParseAddrPort(remote); err == nil {
		return addrPort.Addr().Unmap().String()
	}
	if addr, err := netip.ParseAddr(remote); err == nil {
		return addr.Unmap().String()
	}
	return remote
}

func (p *Pairings) startGraceLocked(deviceID string, now time.Time) (rotated *session) {
	paired, ok := p.deviceSessions[deviceID]
	if !ok || paired.session.isRevoked || paired.session.isGraceUsed {
		return nil
	}
	s := paired.session
	if paired.isByToken {
		if s.approvedDeviceID == "" {
			s.approvedDeviceID = deviceID
		}
		s.expiresAt = now.Add(approvedGrace)
		p.sessions[s.key] = s
	}
	if s == p.displayed {
		return p.rotateLocked(now)
	}
	return nil
}

func (p *Pairings) endGraceLocked(s *session) {
	s.isGraceUsed = true
	delete(p.sessions, s.key)
}

func (p *Pairings) endSessionLocked(deviceID string, now time.Time) (rotated *session) {
	paired, ok := p.deviceSessions[deviceID]
	if !ok {
		return nil
	}
	delete(p.deviceSessions, deviceID)
	s := paired.session
	s.isRevoked = true
	delete(p.sessions, s.key)
	if s == p.displayed {
		return p.rotateLocked(now)
	}
	return nil
}

func (p *Pairings) rotateLocked(now time.Time) *session {
	token := randomToken(pairingTokenBytes)
	s := &session{
		key:       keyOf(token),
		token:     token,
		code:      randomCode(),
		secret:    randomBytes(pairingSecretBytes),
		expiresAt: now.Add(sessionLifetime),
	}
	p.sessions[s.key] = s
	p.displayed = s
	return s
}

func (p *Pairings) pruneLocked(now time.Time) {
	for key, s := range p.sessions {
		if s != p.displayed && !s.isLive(now) {
			delete(p.sessions, key)
		}
	}
	for ip, attempts := range p.redeemsByIP {
		recent := slices.DeleteFunc(attempts, func(at time.Time) bool {
			return now.Sub(at) >= redeemWindow
		})
		if len(recent) == 0 {
			delete(p.redeemsByIP, ip)
		} else {
			p.redeemsByIP[ip] = recent
		}
	}
}
