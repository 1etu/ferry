package server_test

import (
	"context"
	"log/slog"
	"net"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
	"github.com/1etu/ferry/internal/config"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/inbox"
	"github.com/1etu/ferry/internal/outbox"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/server"
	"github.com/1etu/ferry/internal/store"
	"github.com/1etu/ferry/internal/update"
	"github.com/1etu/ferry/tests/kit"
)

const (
	testHostname   = "testpc"
	testLANIP      = "192.168.1.23"
	strangerIP     = "192.168.77.7"
	testVersion    = "1.2.3"
	indexHTML      = "<!doctype html><title>Ferry</title>"
	manifestJSON   = `{"name":"Ferry"}`
	assetJS        = "console.info(1)"
	waitTimeout    = 10 * time.Second
	tusContentType = "application/offset+octet-stream"
)

type options struct {
	pick       func(ctx context.Context) ([]string, error)
	pickFolder func(ctx context.Context) (string, error)
	updater    func(hub *events.Hub, log *slog.Logger) *update.Updater
	log        *slog.Logger
}

type fixture struct {
	t          *testing.T
	server     *httptest.Server
	store      *store.Store
	hub        *events.Hub
	inbox      *inbox.Inbox
	sessions   *seal.Sessions
	updater    *update.Updater
	hooks      *hooks
	received   string
	cancelBase context.CancelCauseFunc
	nextPeer   atomic.Int32

	mu     sync.Mutex
	opened []string
}

func newFixture(t *testing.T, opts options) *fixture {
	t.Helper()
	f := &fixture{t: t, received: t.TempDir(), sessions: seal.NewSessions(time.Now)}
	log := opts.log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "ferry.db"))
	kit.NoError(t, err)
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	f.store = st
	f.hub = events.NewHub(log)
	t.Cleanup(f.hub.Close)
	f.hooks = newHooks(api.Settings{Name: testHostname, ReceivedDir: f.received, CheckUpdates: true})
	f.updater = newUpdater(t, opts, f.hub, log)

	f.server = httptest.NewUnstartedServer(nil)
	tcpAddr, ok := f.server.Listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %T", f.server.Listener.Addr())
	}
	port := strconv.Itoa(tcpAddr.Port)
	au := auth.New(auth.Config{
		Hosts: func() []string { return []string{testHostname, testHostname + ".local", testLANIP} },
		Port:  tcpAddr.Port,
		Dev:   true,
		Now:   time.Now,
	}, st, log)
	origins := func() (local, ip string) {
		return "http://" + testHostname + ".local:" + port, "http://" + testLANIP + ":" + port
	}
	f.inbox, err = inbox.New(inbox.Config{
		ReceivedDir:        f.received,
		IncomingDir:        filepath.Join(f.received, ".incoming"),
		MaxUploadBytes:     1 << 20,
		MaxActivePerDevice: 2,
		ProgressInterval:   10 * time.Millisecond,
		DeviceID:           deviceOf,
		Session:            inbox.SessionFromContext,
		FreeSpace:          func(string) (uint64, error) { return 1 << 40, nil },
	}, st, f.hub, log)
	kit.NoError(t, err)
	t.Cleanup(func() {
		if err := f.inbox.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	pairings := auth.NewPairings(st, f.hub, origins, time.Now, log)
	pairings.OnRevoke = f.sessions.Drop
	handler := server.New(f.deps(opts, au, pairings, origins, log))
	base, cancelBase := context.WithCancelCause(context.WithoutCancel(t.Context()))
	f.cancelBase = cancelBase
	f.server.Config.Handler = handler
	f.server.Config.BaseContext = func(net.Listener) context.Context { return base }
	f.server.Start()
	t.Cleanup(func() {
		cancelBase(inbox.ErrShutdown)
		f.server.Close()
	})
	return f
}

func (f *fixture) deps(opts options, au *auth.Auth, pairings *auth.Pairings, origins func() (string, string), log *slog.Logger) server.Deps {
	pick := opts.pick
	if pick == nil {
		pick = func(context.Context) ([]string, error) { return nil, nil }
	}
	pickFolder := opts.pickFolder
	if pickFolder == nil {
		pickFolder = func(context.Context) (string, error) { return "", nil }
	}
	return server.Deps{
		Config:        config.Config{Name: testHostname, ReceivedDir: f.received},
		Version:       testVersion,
		Name:          func() string { return f.hooks.current().Name },
		Origins:       origins,
		Store:         f.store,
		Auth:          au,
		Pairings:      pairings,
		Sessions:      f.sessions,
		Inbox:         f.inbox,
		Outbox:        outbox.New(f.store, f.hub, deviceOf, log),
		Hub:           f.hub,
		Updater:       f.updater,
		Settings:      f.hooks.current,
		Network:       f.hooks.network,
		Pick:          pick,
		PickFolder:    pickFolder,
		OpenReceived:  f.recordOpened,
		ShowWindow:    f.hooks.showWindow,
		Quit:          f.hooks.quit,
		ApplySettings: f.hooks.applySettings,
		ApplyUpdate:   f.hooks.applyUpdate,
		AllowFirewall: f.hooks.allowFirewall,
		WebUI: fstest.MapFS{
			"index.html":           {Data: []byte(indexHTML)},
			"manifest.webmanifest": {Data: []byte(manifestJSON)},
			"favicon.svg":          {Data: []byte("<svg></svg>")},
			"assets/app-1a2b.js":   {Data: []byte(assetJS)},
		},
		Log: log,
	}
}

func newUpdater(t *testing.T, opts options, hub *events.Hub, log *slog.Logger) *update.Updater {
	t.Helper()
	if opts.updater != nil {
		return opts.updater(hub, log)
	}
	dir := t.TempDir()
	return update.New(update.Config{Current: "dev", DataDir: dir, ExePath: filepath.Join(dir, "Ferry.exe")}, hub, log)
}

func deviceOf(ctx context.Context) (string, bool) {
	p := auth.FromContext(ctx)
	return p.Device.ID, p.Role == auth.RoleDevice
}

func (f *fixture) recordOpened(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, path)
	return nil
}

func (f *fixture) openedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.opened...)
}
