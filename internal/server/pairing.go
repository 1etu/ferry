package server

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
)

const (
	maxDeviceNameRunes    = 64
	pairRetryAfterSeconds = "60"
)

var pairingCodePattern = regexp.MustCompile(`^\d{6}$`)

func (s *server) pair(w http.ResponseWriter, r *http.Request) {
	var req api.PairRequest
	if err := readJSON(w, r, &req); err != nil {
		s.rejectInvalid(w, r, err)
		return
	}
	name, err := validatePair(req)
	if err != nil {
		s.rejectInvalid(w, r, err)
		return
	}
	device, token, err := s.Pairings.Redeem(r.Context(), auth.Redeem{
		Token:     req.Token,
		Code:      req.Code,
		Name:      name,
		RemoteIP:  r.RemoteAddr,
		HasSecret: req.HasSecret,
	})
	switch {
	case errors.Is(err, auth.ErrInvalid):
		s.Log.Warn("pairing rejected, invalid secret", "remote", r.RemoteAddr)
		s.writeError(w, http.StatusNotFound, api.CodePairingInvalid, "pairing invalid")
	case errors.Is(err, auth.ErrExpired):
		s.Log.Warn("pairing rejected, session expired", "remote", r.RemoteAddr)
		s.writeError(w, http.StatusGone, api.CodePairingExpired, "pairing expired")
	case errors.Is(err, auth.ErrRateLimited):
		s.Log.Warn("pairing rejected, rate limited", "remote", r.RemoteAddr)
		w.Header().Set("Retry-After", pairRetryAfterSeconds)
		s.writeError(w, http.StatusTooManyRequests, api.CodeRateLimited, "rate limited")
	case err != nil:
		s.fail(w, r, err)
	default:
		s.Auth.SetCookie(w, token)
		s.writeJSON(w, http.StatusCreated, api.DeviceFrom(device))
	}
}

func validatePair(req api.PairRequest) (name string, err error) {
	if (req.Token == "") == (req.Code == "") {
		return "", errors.New("exactly one of token and code is required")
	}
	if req.Code != "" && !pairingCodePattern.MatchString(req.Code) {
		return "", errors.New("code is not six digits")
	}
	name = strings.TrimSpace(req.Name)
	if length := utf8.RuneCountInString(name); length < 1 || length > maxDeviceNameRunes {
		return "", errors.New("name is not 1 to 64 characters")
	}
	return name, nil
}

func (s *server) currentPairing(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.Pairings.View(s.Pairings.Current()))
}

func (s *server) rotatePairing(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.Pairings.View(s.Pairings.Rotate()))
}
