package server_test

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/platform"
)

func TestSettingsPatchValidatesAndApplies(t *testing.T) {
	t.Parallel()
	newDir := filepath.Join(t.TempDir(), "Ferry Inbox")
	fileInTheWay := writeFile(t, "occupied", "x")
	tests := []struct {
		name   string
		body   any
		status int
		want   func(api.Settings) bool
	}{
		{"trimmed name", `{"name":"  Studio PC  "}`, http.StatusOK, func(s api.Settings) bool { return s.Name == "Studio PC" }},
		{"blank name", `{"name":"   "}`, http.StatusBadRequest, nil},
		{"name of 65 characters", api.SettingsPatch{Name: new(strings.Repeat("a", 65))}, http.StatusBadRequest, nil},
		{"new received dir is created", api.SettingsPatch{ReceivedDir: &newDir}, http.StatusOK, func(s api.Settings) bool { return s.ReceivedDir == newDir }},
		{"relative received dir", `{"receivedDir":"Downloads"}`, http.StatusBadRequest, nil},
		{"received dir over a file", api.SettingsPatch{ReceivedDir: &fileInTheWay}, http.StatusBadRequest, nil},
		{"check updates off", `{"checkUpdates":false}`, http.StatusOK, func(s api.Settings) bool { return !s.CheckUpdates }},
		{"start at login on", `{"startAtLogin":true}`, http.StatusOK, func(s api.Settings) bool { return s.StartAtLogin }},
		{"unknown field", `{"port":9000}`, http.StatusBadRequest, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, options{})
			resp := f.owner().send(http.MethodPatch, "/api/settings", tc.body)
			expectStatus(t, resp, tc.status)
			if tc.want == nil {
				if _, _, applied, _ := f.hooks.calls(); applied != 0 || errorCode(t, resp) != api.CodeInvalidRequest {
					t.Fatalf("rejected patch applied %d times, body %s", applied, resp.body)
				}
				return
			}
			if got := decode[api.Settings](t, resp); !tc.want(got) || got != f.hooks.current() {
				t.Fatalf("settings %+v, applied %+v", got, f.hooks.current())
			}
		})
	}
}

func TestStartAtLoginOffWindowsIsUnsupported(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	f.hooks.set(func(h *hooks) { h.startAtLoginErr = platform.ErrUnsupported })
	expectError(t, f.owner().send(http.MethodPatch, "/api/settings", `{"startAtLogin":true}`), http.StatusNotImplemented, api.CodeUnsupported)
	expectStatus(t, f.owner().send(http.MethodPatch, "/api/settings", `{"name":"Den"}`), http.StatusOK)
	if got := decode[api.Settings](t, f.owner().send(http.MethodGet, "/api/settings", nil)); got.Name != "Den" || got.StartAtLogin {
		t.Fatalf("settings %+v", got)
	}
}

func TestPickReceivedDir(t *testing.T) {
	t.Parallel()
	chosen := filepath.Join(t.TempDir(), "Picked")
	tests := []struct {
		name   string
		pick   func(context.Context) (string, error)
		status int
		want   string
	}{
		{"unsupported", func(context.Context) (string, error) { return "", platform.ErrUnsupported }, http.StatusNotImplemented, ""},
		{"canceled dialog keeps the folder", func(context.Context) (string, error) { return "", nil }, http.StatusOK, ""},
		{"chosen folder", func(context.Context) (string, error) { return chosen, nil }, http.StatusOK, chosen},
		{"picker failed", func(context.Context) (string, error) { return "", errors.New("powershell missing") }, http.StatusInternalServerError, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, options{pickFolder: tc.pick})
			want := tc.want
			if want == "" {
				want = f.received
			}
			resp := f.owner().send(http.MethodPost, "/api/settings/received-dir/pick", nil)
			expectStatus(t, resp, tc.status)
			if tc.status == http.StatusOK && decode[api.Settings](t, resp).ReceivedDir != want {
				t.Fatalf("settings %s, want received dir %s", resp.body, want)
			}
		})
	}
}

func TestNameChangeIsLive(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	expectStatus(t, f.owner().send(http.MethodPatch, "/api/settings", `{"name":"Living Room"}`), http.StatusOK)
	if got := decode[api.Health](t, f.stranger().send(http.MethodGet, "/api/health", nil)); got.Name != "Living Room" {
		t.Fatalf("health %+v", got)
	}
}

func TestOwnerAppRoutesCallTheHooks(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, _ := f.approvedDevice()
	for _, target := range []string{"/api/app/show", "/api/app/quit", "/api/network/allow", "/api/update/apply", "/api/update/check", "/api/settings/received-dir/pick"} {
		expectError(t, device.send(http.MethodPost, target, nil), http.StatusForbidden, api.CodeForbidden)
	}
	expectStatus(t, f.owner().send(http.MethodPost, "/api/app/show", nil), http.StatusNoContent)
	expectStatus(t, f.owner().send(http.MethodPost, "/api/app/quit", nil), http.StatusNoContent)
	expectStatus(t, f.owner().send(http.MethodPost, "/api/network/allow", nil), http.StatusNoContent)
	if shows, quits, _, allows := f.hooks.calls(); shows != 1 || quits != 1 || allows != 1 {
		t.Fatalf("hooks called show %d, quit %d, allow %d", shows, quits, allows)
	}
	if got := decode[api.Network](t, f.owner().send(http.MethodGet, "/api/network", nil)); got != f.hooks.network() {
		t.Fatalf("network %+v", got)
	}
}

func TestAllowNetworkErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err    error
		status int
		code   api.ErrorCode
	}{
		{platform.ErrCanceled, http.StatusConflict, api.CodeConflict},
		{platform.ErrUnsupported, http.StatusNotImplemented, api.CodeUnsupported},
		{errors.New("netsh failed"), http.StatusInternalServerError, api.CodeInternal},
	}
	for _, tc := range tests {
		f := newFixture(t, options{})
		f.hooks.set(func(h *hooks) { h.firewallErr = tc.err })
		expectError(t, f.owner().send(http.MethodPost, "/api/network/allow", nil), tc.status, tc.code)
	}
}
