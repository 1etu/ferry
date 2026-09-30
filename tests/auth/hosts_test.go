package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
)

func TestCheckHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		port      int
		isDev     bool
		host      string
		isAllowed bool
	}{
		{"mdns name with port", 8080, false, "egetu-pc.local:8080", true},
		{"hostname with port", 8080, false, "egetu-pc:8080", true},
		{"names are case-insensitive", 8080, false, "EGETU-PC.Local:8080", true},
		{"LAN IP with port", 8080, false, "192.168.1.23:8080", true},
		{"localhost", 8080, false, "localhost:8080", true},
		{"IPv4 loopback", 8080, false, "127.0.0.1:8080", true},
		{"IPv6 loopback", 8080, false, "[::1]:8080", true},
		{"port mismatch", 8080, false, "egetu-pc.local:8081", false},
		{"missing port means 80", 8080, false, "egetu-pc.local", false},
		{"rebinding name", 8080, false, "attacker.example:8080", false},
		{"unknown LAN IP", 8080, false, "192.168.1.24:8080", false},
		{"suffix of an allowed name", 8080, false, "x.egetu-pc.local:8080", false},
		{"empty host", 8080, false, "", false},
		{"port only", 8080, false, ":8080", false},
		{"dev mode accepts any port", 8080, true, "egetu-pc.local:5173", true},
		{"dev mode still checks the name", 8080, true, "attacker.example:5173", false},
		{"port 80 without port", 80, false, "egetu-pc.local", true},
		{"port 80 with port", 80, false, "egetu-pc.local:80", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t, tc.port, tc.isDev)
			rec := httptest.NewRecorder()
			f.auth.CheckHost(reached).ServeHTTP(rec, newRequest(http.MethodGet, tc.host, lanPeer))
			if tc.isAllowed {
				if rec.Code != http.StatusNoContent {
					t.Fatalf("status %d, want the request to pass", rec.Code)
				}
				return
			}
			requireErrorResponse(t, rec, http.StatusMisdirectedRequest, api.CodeForbidden)
		})
	}
}

func TestCheckHostCachesHostsForTenSeconds(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t, 8080, false)
	handler := f.auth.CheckHost(reached)
	serve := func() {
		handler.ServeHTTP(httptest.NewRecorder(), newRequest(http.MethodGet, "egetu-pc.local:8080", lanPeer))
	}

	serve()
	f.clock.Advance(10*time.Second - time.Millisecond)
	serve()
	if got := f.hostsCalls.Load(); got != 1 {
		t.Fatalf("Hosts called %d times within ten seconds, want 1", got)
	}
	f.clock.Advance(time.Millisecond)
	serve()
	if got := f.hostsCalls.Load(); got != 2 {
		t.Fatalf("Hosts called %d times after ten seconds, want 2", got)
	}
}

func TestCheckOrigin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		isDev     bool
		method    string
		origin    string
		isAllowed bool
	}{
		{"GET with foreign origin", false, http.MethodGet, "http://attacker.example", true},
		{"HEAD with foreign origin", false, http.MethodHead, "http://attacker.example", true},
		{"OPTIONS with foreign origin", false, http.MethodOptions, "http://attacker.example", true},
		{"POST without origin", false, http.MethodPost, "", true},
		{"POST from mdns origin", false, http.MethodPost, "http://egetu-pc.local:8080", true},
		{"DELETE from loopback origin", false, http.MethodDelete, "http://127.0.0.1:8080", true},
		{"PATCH from IPv6 loopback origin", false, http.MethodPatch, "http://[::1]:8080", true},
		{"POST from LAN IP origin", false, http.MethodPost, "http://192.168.1.23:8080", true},
		{"POST from foreign origin", false, http.MethodPost, "http://attacker.example", false},
		{"DELETE from foreign origin on our port", false, http.MethodDelete, "http://attacker.example:8080", false},
		{"POST from null origin", false, http.MethodPost, "null", false},
		{"POST from https origin", false, http.MethodPost, "https://egetu-pc.local:8080", false},
		{"POST from other port", false, http.MethodPost, "http://egetu-pc.local:9999", false},
		{"POST from origin with path", false, http.MethodPost, "http://egetu-pc.local:8080/x", false},
		{"POST from origin with user info", false, http.MethodPost, "http://attacker@egetu-pc.local:8080", false},
		{"lowercase method is not safe", false, "get", "http://attacker.example", false},
		{"dev accepts our name on any port", true, http.MethodPost, "http://egetu-pc.local:5173", true},
		{"dev rejects foreign origin", true, http.MethodPost, "http://attacker.example:5173", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t, 8080, tc.isDev)
			r := newRequest(tc.method, "egetu-pc.local:8080", lanPeer)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			f.auth.CheckOrigin(reached).ServeHTTP(rec, r)
			if tc.isAllowed {
				if rec.Code != http.StatusNoContent {
					t.Fatalf("status %d, want the request to pass", rec.Code)
				}
				return
			}
			requireErrorResponse(t, rec, http.StatusForbidden, api.CodeForbidden)
		})
	}
}
