package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/1etu/ferry/internal/api"
)

func TestStaticAndFallbackResponses(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	tests := []struct {
		method, target            string
		status                    int
		cacheControl, contentType string
		body                      string
	}{
		{"GET", "/", 200, "no-cache", "text/html; charset=utf-8", indexHTML},
		{"GET", "/?pair=abc", 200, "no-cache", "text/html; charset=utf-8", indexHTML},
		{"GET", "/some/deep/link", 200, "no-cache", "text/html; charset=utf-8", indexHTML},
		{"HEAD", "/", 200, "no-cache", "text/html; charset=utf-8", ""},
		{"GET", "/favicon.svg", 200, "no-cache", "image/svg+xml", "<svg></svg>"},
		{"GET", "/assets/app-1a2b.js", 200, "public, max-age=31536000, immutable", "", assetJS},
		{"GET", "/manifest.webmanifest", 200, "no-store", "application/manifest+json", manifestJSON},
		{"GET", "/assets/missing.js", 404, "", jsonContentType, ""},
		{"GET", "/api/nothing", 404, "", jsonContentType, ""},
		{"POST", "/api/health", 404, "", jsonContentType, ""},
		{"PUT", "/api/devices", 404, "", jsonContentType, ""},
	}
	for _, tc := range tests {
		resp := f.owner().send(tc.method, tc.target, nil)
		if resp.status != tc.status {
			t.Errorf("%s %s: status %d, want %d", tc.method, tc.target, resp.status, tc.status)
			continue
		}
		if got := resp.header.Get("Cache-Control"); got != tc.cacheControl {
			t.Errorf("%s %s: Cache-Control %q, want %q", tc.method, tc.target, got, tc.cacheControl)
		}
		if got := resp.header.Get("Content-Type"); tc.contentType != "" && got != tc.contentType {
			t.Errorf("%s %s: Content-Type %q, want %q", tc.method, tc.target, got, tc.contentType)
		}
		if tc.status == http.StatusNotFound {
			if code := errorCode(t, resp); code != api.CodeNotFound {
				t.Errorf("%s %s: code %q", tc.method, tc.target, code)
			}
		} else if string(resp.body) != tc.body {
			t.Errorf("%s %s: body %q, want %q", tc.method, tc.target, resp.body, tc.body)
		}
	}
}

func TestEmptyListsEncodeAsArrays(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	for _, target := range []string{"/api/transfers", "/api/files", "/api/devices"} {
		resp := f.owner().send(http.MethodGet, target, nil)
		expectStatus(t, resp, http.StatusOK)
		if got := strings.TrimSpace(string(resp.body)); got != "[]" {
			t.Errorf("%s: body %q, want []", target, got)
		}
	}
}

func TestHealthAndSessionDescribeTheServer(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	health := decode[api.Health](t, f.stranger().send(http.MethodGet, "/api/health", nil))
	if health != (api.Health{App: api.AppName, Name: testHostname, Version: testVersion}) {
		t.Fatalf("health %+v", health)
	}
	device, d := f.pendingDevice()
	tests := []struct {
		name     string
		client   *client
		role     api.Role
		deviceID string
	}{
		{"owner", f.owner(), api.RoleOwner, ""},
		{"stranger", f.stranger(), api.RoleNone, ""},
		{"device", device, api.RoleDevice, d.ID},
	}
	for _, tc := range tests {
		session := decode[api.Session](t, tc.client.send(http.MethodGet, "/api/session", nil))
		if session.Role != tc.role {
			t.Errorf("%s: role %q, want %q", tc.name, session.Role, tc.role)
		}
		gotDevice := ""
		if session.Device != nil {
			gotDevice = session.Device.ID
		}
		if gotDevice != tc.deviceID {
			t.Errorf("%s: device %q, want %q", tc.name, gotDevice, tc.deviceID)
		}
		if !strings.HasPrefix(session.Server.Origins.Local, "http://testpc.local:") ||
			!strings.HasPrefix(session.Server.Origins.IP, "http://"+testLANIP+":") {
			t.Errorf("%s: origins %+v", tc.name, session.Server.Origins)
		}
	}
}
