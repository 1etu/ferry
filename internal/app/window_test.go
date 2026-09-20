package app

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/config"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/store"
)

type fakeWindow struct {
	mu          sync.Mutex
	shows       int
	isAvailable bool
}

func (w *fakeWindow) Show() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.shows++
}

func (w *fakeWindow) Close() {}

func (w *fakeWindow) Available() bool {
	return w.isAvailable
}

func (w *fakeWindow) showCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.shows
}

type windowedMachine struct {
	*machine
	window *fakeWindow
	opened *atomic.Int32
}

func newWindowedMachine(t *testing.T, env config.Env, hasWebView2 bool) windowedMachine {
	t.Helper()
	w := &fakeWindow{isAvailable: hasWebView2}
	opened := &atomic.Int32{}
	m := &machine{
		env:     env,
		dataDir: t.TempDir(),
		port:    8080,
		log:     slog.New(slog.DiscardHandler),
		window:  w,
		openURL: func(string) error { opened.Add(1); return nil },
	}
	return windowedMachine{machine: m, window: w, opened: opened}
}

func TestServerLaunchModes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args     []string
		want     launch
		isServer bool
	}{
		{nil, launch{showWindow: true}, true},
		{[]string{"--hidden"}, launch{}, true},
		{[]string{"--updated"}, launch{showWindow: true, isUpdated: true}, true},
		{[]string{"--hidden", "--updated"}, launch{}, false},
		{[]string{"uninstall"}, launch{}, false},
	}
	for _, tc := range tests {
		got, isServer := serverLaunch(tc.args)
		if got != tc.want || isServer != tc.isServer {
			t.Errorf("%q: %+v %v, want %+v %v", tc.args, got, isServer, tc.want, tc.isServer)
		}
	}
}

func TestWindowOnLaunch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		env         config.Env
		hasWebView2 bool
		l           launch
		wantShows   int
		wantOpened  int32
	}{
		{"hidden autostart opens nothing", config.Env{}, true, launch{}, 0, 0},
		{"headless opens nothing", config.Env{Headless: true}, true, launch{showWindow: true}, 0, 0},
		{"window when webview2 is present", config.Env{}, true, launch{showWindow: true}, 1, 0},
		{"browser when webview2 is missing", config.Env{}, false, launch{showWindow: true}, 0, 1},
	}
	for _, tc := range tests {
		wm := newWindowedMachine(t, tc.env, tc.hasWebView2)
		wm.openOnLaunch(tc.l)
		if wm.window.showCount() != tc.wantShows || wm.opened.Load() != tc.wantOpened {
			t.Errorf("%s: %d shows and %d browser opens, want %d and %d", tc.name, wm.window.showCount(), wm.opened.Load(), tc.wantShows, tc.wantOpened)
		}
	}
}

func TestDeviceRequestRaisesTheWindow(t *testing.T) {
	t.Parallel()
	wm := newWindowedMachine(t, config.Env{}, true)
	stream := make(chan events.Event, 3)
	stream <- events.Event{Kind: events.KindDevice, Payload: api.DeviceChange{Action: api.DeviceApproved, Device: api.Device{Status: store.DeviceApproved}}}
	stream <- events.Event{Kind: events.KindDevice, Payload: api.DeviceChange{Action: api.DeviceRequested}}
	stream <- events.Event{Kind: events.KindTransfer, Payload: api.Transfer{}}
	close(stream)
	wm.react(t.Context(), stream)
	if shows := wm.window.showCount(); shows != 1 {
		t.Fatalf("window shown %d times, want once for the request", shows)
	}
}

func TestSecondInstanceAsksTheRunningOneToShow(t *testing.T) {
	t.Parallel()
	var shows atomic.Int32
	running := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			if err := json.NewEncoder(w).Encode(api.Health{App: api.AppName}); err != nil {
				t.Error(err)
			}
		case appShowEndpoint:
			shows.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer running.Close()
	addr, ok := running.Listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("address %T", running.Listener.Addr())
	}
	wm := newWindowedMachine(t, config.Env{}, true)
	wm.port = addr.Port
	if code := wm.joinRunning(t.Context(), errors.New("port in use")); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if diff := cmp.Diff(int32(1), shows.Load()); diff != "" {
		t.Fatalf("show requests (-want +got):\n%s", diff)
	}
}
