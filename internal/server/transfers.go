package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
	"github.com/1etu/ferry/internal/inbox"
	"github.com/1etu/ferry/internal/store"
)

const (
	defaultTransferLimit = 100
	maxTransferLimit     = 500
)

func (s *server) transfers(w http.ResponseWriter, r *http.Request) {
	limit, err := transferLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.rejectInvalid(w, r, err)
		return
	}
	transfers, err := s.Store.Transfers(r.Context(), deviceScope(auth.FromContext(r.Context())), limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	views := make([]api.Transfer, 0, len(transfers))
	for i := range transfers {
		t := &transfers[i]
		if t.Status == store.TransferActive {
			if done, ok := s.liveProgress(t); ok {
				t.Done = done
			}
		}
		views = append(views, api.TransferFrom(*t))
	}
	if err := sealNames(r, views, func(t *api.Transfer) *string { return &t.Name }); err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, views)
}

func transferLimit(query string) (int, error) {
	if query == "" {
		return defaultTransferLimit, nil
	}
	limit, err := strconv.Atoi(query)
	if err != nil || limit < 1 || limit > maxTransferLimit {
		return 0, fmt.Errorf("limit %q is not between 1 and %d", query, maxTransferLimit)
	}
	return limit, nil
}

func (s *server) liveProgress(t *store.Transfer) (done int64, ok bool) {
	if t.Direction == store.DirectionIn {
		return s.Inbox.Progress(t.ID)
	}
	return s.Outbox.Progress(t.ID)
}

func (s *server) clearTransfers(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteFinishedTransfers(r.Context(), deviceScope(auth.FromContext(r.Context()))); err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

func (s *server) deleteTransfer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.Store.Transfer(ctx, r.PathValue("transferId"))
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if scope := deviceScope(auth.FromContext(ctx)); scope != "" && scope != t.DeviceID {
		s.notFound(w)
		return
	}
	switch {
	case t.Status != store.TransferActive:
		err = s.Store.DeleteTransfer(ctx, t.ID)
	case t.Direction == store.DirectionIn:
		err = s.Inbox.Cancel(ctx, t.ID)
	default:
		err = s.Outbox.Cancel(ctx, t.ID)
	}
	if errors.Is(err, inbox.ErrNotActive) {
		s.writeError(w, http.StatusConflict, api.CodeConflict, "upload not active")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

func (s *server) openReceivedFolder(w http.ResponseWriter, r *http.Request) {
	var req api.OpenReceivedRequest
	if err := readJSON(w, r, &req); err != nil && !errors.Is(err, io.EOF) {
		s.rejectInvalid(w, r, err)
		return
	}
	path := s.Config.ReceivedDir
	if req.TransferID != "" {
		t, err := s.Store.Transfer(r.Context(), req.TransferID)
		if errors.Is(err, store.ErrNotFound) || (err == nil && !isReceivedFile(t)) {
			s.notFound(w)
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		path = t.Path
	}
	if err := s.OpenReceived(path); err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

func isReceivedFile(t store.Transfer) bool {
	return t.Direction == store.DirectionIn && t.Status == store.TransferDone && t.Path != ""
}
