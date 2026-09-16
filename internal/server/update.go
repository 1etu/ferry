package server

import (
	"context"
	"net/http"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/update"
)

func updateStatusFrom(st update.Status) api.UpdateStatus {
	return api.UpdateStatus{
		Current:   st.Current,
		Available: st.Available,
		State:     string(st.State),
		CheckedAt: api.OptionalTimestamp(st.CheckedAt),
		Error:     st.Error,
	}
}

func (s *server) updateStatus(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, updateStatusFrom(s.Updater.Status()))
}

func (s *server) checkUpdate(w http.ResponseWriter, r *http.Request) {
	started := s.Updater.StartCheck(context.WithoutCancel(r.Context()))
	s.writeJSON(w, http.StatusAccepted, updateStatusFrom(started))
}

func (s *server) applyUpdate(w http.ResponseWriter, r *http.Request) {
	if s.Updater.Status().State != update.StateReady {
		s.writeError(w, http.StatusConflict, api.CodeConflict, "no update is ready")
		return
	}
	ctx := context.WithoutCancel(r.Context())
	go func() {
		if err := s.ApplyUpdate(ctx); err != nil {
			s.Log.Error("update not applied, previous version restarted", "err", err)
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}
