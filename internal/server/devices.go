package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
	"github.com/1etu/ferry/internal/store"
)

func (s *server) devices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.Store.Devices(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	views := make([]api.Device, 0, len(devices))
	for i := range devices {
		views = append(views, api.DeviceFrom(devices[i]))
	}
	s.writeJSON(w, http.StatusOK, views)
}

func (s *server) approveDevice(w http.ResponseWriter, r *http.Request) {
	device, err := s.Pairings.Approve(r.Context(), r.PathValue("deviceId"))
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, api.DeviceFrom(device))
}

func (s *server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := auth.FromContext(ctx)
	id := r.PathValue("deviceId")
	switch {
	case p.Role == auth.RoleNone:
		s.writeError(w, http.StatusUnauthorized, api.CodeUnauthorized, "unauthorized")
		return
	case p.Role == auth.RoleDevice && p.Device.ID != id:
		s.Log.Warn("revoke rejected, device is not the caller", "device", p.Device.ID, "target", id)
		s.writeError(w, http.StatusForbidden, api.CodeForbidden, "forbidden")
		return
	}
	if err := s.revoke(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w)
			return
		}
		s.fail(w, r, err)
		return
	}
	if err := s.Inbox.CancelDevice(ctx, id); err != nil {
		s.Log.Error("uploads of revoked device not canceled", "device", id, "err", err)
	}
	if p.Role == auth.RoleDevice {
		s.Auth.ClearCookie(w)
	}
	noContent(w)
}

func (s *server) revoke(ctx context.Context, id string) error {
	s.handshakeMu.Lock()
	defer s.handshakeMu.Unlock()
	_, err := s.Pairings.Revoke(ctx, id)
	return err
}
