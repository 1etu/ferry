package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/1etu/ferry/internal/api"
)

const (
	jsonContentType  = "application/json; charset=utf-8"
	maxJSONBodyBytes = 64 << 10
)

func (s *server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.Log.Debug("response not written, client gone", "err", err)
	}
}

func (s *server) writeError(w http.ResponseWriter, status int, code api.ErrorCode, message string) {
	s.writeJSON(w, status, api.NewError(code, message))
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func (s *server) rejectInvalid(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Warn("request rejected, invalid input", "route", r.Pattern, "remote", r.RemoteAddr, "err", err)
	s.writeError(w, http.StatusBadRequest, api.CodeInvalidRequest, "invalid request")
}

func (s *server) notFound(w http.ResponseWriter) {
	s.writeError(w, http.StatusNotFound, api.CodeNotFound, "not found")
}

func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("request failed", "route", r.Pattern, "err", err)
	s.writeError(w, http.StatusInternalServerError, api.CodeInternal, "internal error")
}

func noContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
