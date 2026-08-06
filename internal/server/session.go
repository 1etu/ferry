package server

import (
	"net/http"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
)

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, api.Health{App: api.AppName, Name: s.Name(), Version: s.Version})
}

func (s *server) session(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	local, ip := s.Origins()
	session := api.Session{
		Role: roleOf(p),
		Server: api.ServerInfo{
			Name:    s.Name(),
			Version: s.Version,
			Origins: api.Origins{Local: local, IP: ip},
		},
	}
	if p.Role == auth.RoleDevice {
		device := api.DeviceFrom(p.Device)
		session.Device = &device
	}
	s.writeJSON(w, http.StatusOK, session)
}

func roleOf(p auth.Principal) api.Role {
	switch p.Role {
	case auth.RoleOwner:
		return api.RoleOwner
	case auth.RoleDevice:
		return api.RoleDevice
	default:
		return api.RoleNone
	}
}

func deviceScope(p auth.Principal) string {
	if p.Role == auth.RoleDevice {
		return p.Device.ID
	}
	return ""
}
