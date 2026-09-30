package auth_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
	"github.com/1etu/ferry/internal/store"
)

const (
	lanPeer      = "192.168.1.50:51000"
	loopbackPeer = "127.0.0.1:51000"
)

type authFixture struct {
	t          *testing.T
	clock      *fakeClock
	store      *store.Store
	auth       *auth.Auth
	hostsCalls *atomic.Int32
}

func newAuthFixture(t *testing.T, port int, isDev bool) *authFixture {
	t.Helper()
	clock := newFakeClock()
	st := openTestStore(t)
	hostsCalls := &atomic.Int32{}
	cfg := auth.Config{
		Hosts: func() []string {
			hostsCalls.Add(1)
			return []string{"egetu-pc", "egetu-pc.local", "192.168.1.23"}
		},
		Port: port,
		Dev:  isDev,
		Now:  clock.Now,
	}
	return &authFixture{
		t:          t,
		clock:      clock,
		store:      st,
		auth:       auth.New(cfg, st, slog.New(slog.DiscardHandler)),
		hostsCalls: hostsCalls,
	}
}

func newRequest(method, host, remote string) *http.Request {
	r := httptest.NewRequest(method, "http://"+host+"/api/session", http.NoBody)
	r.Host = host
	r.RemoteAddr = remote
	return r
}

func deviceCookies(rec *httptest.ResponseRecorder) []string {
	var found []string
	for _, v := range rec.Result().Header.Values("Set-Cookie") {
		if strings.HasPrefix(v, auth.CookieName+"=") {
			found = append(found, v)
		}
	}
	return found
}

func requireErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, status int, code api.ErrorCode) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d", rec.Code, status)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type %q", got)
	}
	var body api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if body.Error.Code != code || body.Error.Message == "" {
		t.Fatalf("body %s, want code %s with a message", rec.Body.String(), code)
	}
}

var reached = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
})
