package server

import (
	"errors"
	"net/http"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/platform"
)

func (s *server) network(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.Network())
}

func (s *server) allowNetwork(w http.ResponseWriter, r *http.Request) {
	err := s.AllowFirewall(r.Context())
	switch {
	case errors.Is(err, platform.ErrCanceled):
		s.writeError(w, http.StatusConflict, api.CodeConflict, "firewall prompt declined")
	case errors.Is(err, platform.ErrUnsupported):
		s.writeError(w, http.StatusNotImplemented, api.CodeUnsupported, "no firewall control on this platform")
	case err != nil:
		s.fail(w, r, err)
	default:
		noContent(w)
	}
}

func (s *server) showWindow(w http.ResponseWriter, _ *http.Request) {
	s.ShowWindow()
	noContent(w)
}

func (s *server) quit(w http.ResponseWriter, _ *http.Request) {
	s.Quit()
	noContent(w)
}
