package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	keepAliveInterval    = 20 * time.Second
	writeTimeout         = 30 * time.Second
	retryFrame           = "retry: 2000\n\n"
	keepAliveFrame       = ": keep-alive\n\n"
	emptyPayload         = "{}"
	unauthorizedResponse = `{"error":{"code":"unauthorized","message":"unauthorized"}}`
)

func (h *Hub) Handler(scope func(r *http.Request) (Scope, bool)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := scope(r)
		if !ok {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			if _, err := io.WriteString(w, unauthorizedResponse); err != nil {
				h.log.Debug("write unauthorized response", "err", err)
			}
			return
		}
		events, cancel := h.Subscribe(s)
		defer cancel()
		h.stream(r.Context(), w, events, s.Seal)
	})
}

func (h *Hub) stream(ctx context.Context, w http.ResponseWriter, events <-chan Event, seal func(string) (string, bool)) {
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	defer h.clearWriteDeadline(rc)
	if !h.send(w, rc, retryFrame) {
		return
	}

	ticker := time.NewTicker(h.keepAlive)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-events:
			if !ok {
				return
			}
			payload, ok := sealedPayload(e.Payload, seal)
			if !ok {
				h.log.Debug("event skipped, names not sealable", "kind", string(e.Kind))
				continue
			}
			frame, err := encodeFrame(e.Kind, payload)
			if err != nil {
				h.log.Error("event skipped, payload not encodable", "err", err)
				continue
			}
			if !h.send(w, rc, frame) {
				return
			}
		case <-ticker.C:
			if !h.send(w, rc, keepAliveFrame) {
				return
			}
		}
	}
}

func (h *Hub) send(w io.Writer, rc *http.ResponseController, frame string) bool {
	err := rc.SetWriteDeadline(time.Now().Add(writeTimeout))
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		h.log.Debug("event stream closed by client", "err", err)
		return false
	}
	if _, err := io.WriteString(w, frame); err != nil {
		h.log.Debug("event stream closed by client", "err", err)
		return false
	}
	if err := rc.Flush(); err != nil {
		if errors.Is(err, http.ErrNotSupported) {
			h.log.Error("event stream cannot flush", "err", err)
		} else {
			h.log.Debug("event stream closed by client", "err", err)
		}
		return false
	}
	return true
}

func (h *Hub) clearWriteDeadline(rc *http.ResponseController) {
	err := rc.SetWriteDeadline(time.Time{})
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		h.log.Debug("event stream write deadline not cleared", "err", err)
	}
}

func sealedPayload(payload any, seal func(string) (string, bool)) (any, bool) {
	sealable, isSealable := payload.(Sealable)
	if seal == nil || !isSealable {
		return payload, true
	}
	return sealable.WithSealedNames(seal)
}

func encodeFrame(kind Kind, payload any) (string, error) {
	data := emptyPayload
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return "", fmt.Errorf("encode %s event: %w", kind, err)
		}
		data = string(encoded)
	}
	return "event: " + string(kind) + "\ndata: " + data + "\n\n", nil
}
