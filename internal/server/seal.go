package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
	"github.com/1etu/ferry/internal/inbox"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/store"
)

const (
	sealHeader = "X-Ferry-Seal"
	sealParam  = "seal"
)

var errDeviceNotApproved = errors.New("device is no longer approved")

func (s *server) requireSeal(how sealing, next http.Handler) http.Handler {
	if how == unsealed {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := auth.FromContext(r.Context())
		if p.Role != auth.RoleDevice || p.Device.Status != store.DeviceApproved {
			next.ServeHTTP(w, r)
			return
		}
		id := r.Header.Get(sealHeader)
		if how == sealedByQuery {
			id = r.URL.Query().Get(sealParam)
		}
		session, ok := s.Sessions.Lookup(p.Device.ID, id)
		if id == "" || !ok {
			s.writeError(w, http.StatusForbidden, api.CodeSealExpired, "sealed session required")
			return
		}
		next.ServeHTTP(w, r.WithContext(inbox.ContextWithSession(r.Context(), session)))
	})
}

func (s *server) handshake(w http.ResponseWriter, r *http.Request) {
	var req api.SealRequest
	if err := readJSON(w, r, &req); err != nil {
		s.rejectInvalid(w, r, err)
		return
	}
	clientKey, proof, err := decodeSealRequest(req)
	if err != nil {
		s.rejectInvalid(w, r, err)
		return
	}
	deviceID := auth.FromContext(r.Context()).Device.ID
	result, err := s.openSession(r.Context(), deviceID, clientKey, proof)
	switch {
	case errors.Is(err, seal.ErrProof):
		s.Log.Warn("sealed session refused, proof rejected", "device", deviceID)
		s.writeError(w, http.StatusForbidden, api.CodeSealInvalid, "seal proof rejected")
	case errors.Is(err, seal.ErrPublicKey):
		s.rejectInvalid(w, r, err)
	case errors.Is(err, errDeviceNotApproved), errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusForbidden, api.CodeForbidden, "forbidden")
	case err != nil:
		s.fail(w, r, err)
	default:
		s.Log.Info("sealed session opened", "device", deviceID, "first_use", result.DeviceSecret != nil)
		s.writeJSON(w, http.StatusCreated, api.SealResponse{
			SessionID: result.Session.ID,
			ServerKey: base64.RawURLEncoding.EncodeToString(result.ServerKey),
			Confirm:   base64.RawURLEncoding.EncodeToString(result.Confirm),
		})
	}
}

func decodeSealRequest(req api.SealRequest) (clientKey, proof []byte, err error) {
	clientKey, err = base64.RawURLEncoding.DecodeString(req.ClientKey)
	if err != nil {
		return nil, nil, fmt.Errorf("decode client key: %w", err)
	}
	if req.Proof == "" {
		return clientKey, nil, nil
	}
	proof, err = base64.RawURLEncoding.DecodeString(req.Proof)
	if err != nil {
		return nil, nil, fmt.Errorf("decode proof: %w", err)
	}
	return clientKey, proof, nil
}

func (s *server) openSession(ctx context.Context, deviceID string, clientKey, proof []byte) (seal.Handshake, error) {
	s.handshakeMu.Lock()
	defer s.handshakeMu.Unlock()
	device, err := s.Store.Device(ctx, deviceID)
	if err != nil {
		return seal.Handshake{}, fmt.Errorf("load device for handshake: %w", err)
	}
	if device.Status != store.DeviceApproved {
		return seal.Handshake{}, errDeviceNotApproved
	}
	result, err := s.Sessions.Handshake(deviceID, device.Secret, clientKey, proof)
	if err != nil {
		return seal.Handshake{}, err
	}
	if result.DeviceSecret == nil {
		return result, nil
	}
	if err := s.Store.SetDeviceSecret(ctx, deviceID, result.DeviceSecret); err != nil {
		s.Sessions.Drop(deviceID)
		return seal.Handshake{}, fmt.Errorf("store first-use device secret: %w", err)
	}
	return result, nil
}

func sealNames[T any](r *http.Request, views []T, name func(*T) *string) error {
	session, ok := inbox.SessionFromContext(r.Context())
	if !ok {
		return nil
	}
	for i := range views {
		field := name(&views[i])
		sealed, err := seal.SealString(&session.Key, *field)
		if err != nil {
			return fmt.Errorf("seal name: %w", err)
		}
		*field = sealed
	}
	return nil
}
