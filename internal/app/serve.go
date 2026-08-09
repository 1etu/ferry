package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	stdlog "log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/config"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/platform"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/server"
	"github.com/1etu/ferry/internal/tray"
	"github.com/1etu/ferry/internal/update"
)

const (
	allInterfaces   = ""
	healthTimeout   = 2 * time.Second
	trayTooltip     = "Ferry"
	webviewDirName  = "webview"
	webviewDirPerm  = 0o750
	appShowEndpoint = "/api/app/show"
)

func runServer(ctx context.Context, stderr io.Writer, l launch) int {
	env := config.ReadEnv()
	dir, err := dataDir(env)
	if err != nil {
		reportFailure(stderr, err)
		return exitFailure
	}
	log, closeLog, err := openLog(dir, env.Dev, stderr)
	if err != nil {
		reportFailure(stderr, err)
		return exitFailure
	}
	stdlog.SetOutput(slog.NewLogLogger(log.Handler(), slog.LevelError).Writer())
	code := serve(ctx, env, dir, allInterfaces, log, l)
	if err := closeLog(); err != nil {
		reportFailure(stderr, fmt.Errorf("close log: %w", err))
	}
	return code
}

func serve(ctx context.Context, env config.Env, dir, listenHost string, log *slog.Logger, l launch) int {
	m, err := newMachine(env, dir, listenHost, log)
	if err != nil {
		log.Error("configuration not loaded", "err", err)
		return exitFailure
	}
	if code, isDone := m.installFirstRun(ctx); isDone {
		return code
	}
	ctx, quit := context.WithCancel(ctx)
	defer quit()
	var bg background
	m.wire(ctx, quit, &bg, m.updateConfig())
	if err := m.stack.start(ctx); err != nil {
		m.hub.Close()
		if listenErr, isListen := errors.AsType[*listenError](err); isListen {
			return m.joinRunning(ctx, listenErr.err)
		}
		log.Error("services not started", "err", err)
		return exitFailure
	}
	m.startBackground(ctx, &bg, l)
	if env.Headless {
		<-ctx.Done()
	} else {
		tray.Run(ctx, m.trayActions(ctx, &bg), trayTooltip)
		quit()
	}

	log.Info("shutting down")
	m.window.Close()
	code := exitOK
	if err := m.stack.close(ctx); err != nil {
		log.Error("stopped after a failure", "err", err)
		code = exitFailure
	}
	bg.Wait()
	m.hub.Close()
	log.Info("stopped")
	return code
}

func (m *machine) updateConfig() update.Config {
	return update.Config{Current: Version, DataDir: m.dataDir, ExePath: m.exePath, Dev: m.env.Dev}
}

func (m *machine) wire(ctx context.Context, quit context.CancelFunc, bg *background, updates update.Config) {
	m.quit = quit
	m.hub = events.NewHub(m.log)
	m.sessions = seal.NewSessions(time.Now)
	m.updater = update.New(updates, m.hub, m.log)
	m.stack = &stack{m: m, hooks: m.serverHooks(ctx, bg)}
}

func (m *machine) serverHooks(ctx context.Context, bg *background) server.Deps {
	return server.Deps{
		Name:          m.name,
		Origins:       m.origins,
		Sessions:      m.sessions,
		Hub:           m.hub,
		Updater:       m.updater,
		Settings:      m.settings,
		Network:       m.network.current,
		Pick:          func(ctx context.Context) ([]string, error) { return m.platform.pickFiles(ctx, pickTitle) },
		PickFolder:    func(ctx context.Context) (string, error) { return m.platform.pickFolder(ctx, folderTitle) },
		OpenReceived:  m.openReceived,
		ShowWindow:    m.showWindow,
		Quit:          m.quit,
		ApplySettings: func(_ context.Context, s api.Settings) error { return m.applySettings(ctx, bg, s) },
		ApplyUpdate:   func(context.Context) error { return m.applyUpdate(ctx) },
		AllowFirewall: func(reqCtx context.Context) error { return m.allowFirewall(reqCtx, ctx, bg) },
	}
}

func (m *machine) startBackground(ctx context.Context, bg *background, l launch) {
	bg.Go(func() { m.checkFirewallAfter(ctx, firewallCheckDelay) })
	if l.isUpdated {
		bg.Go(func() { m.removeOldExe(ctx) })
	}
	if m.env.Headless {
		return
	}
	bg.Go(m.installSendTo)
	bg.Go(func() { m.updater.Run(ctx, func() bool { return m.config().CheckUpdates }) })
	bg.Go(func() { m.watchEvents(ctx) })
	m.openOnLaunch(l)
}

func (m *machine) openOnLaunch(l launch) {
	if l.showWindow {
		m.showWindow()
	}
}

func (m *machine) joinRunning(ctx context.Context, listenErr error) int {
	probeCtx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	baseURL := loopbackURL(m.port)
	if !isRunning(probeCtx, http.DefaultClient, baseURL) {
		m.log.Error("listen failed", "port", m.port, "err", listenErr)
		return exitFailure
	}
	m.log.Info("already running, handing over to the running instance", "port", m.port)
	if err := postOwner(probeCtx, http.DefaultClient, baseURL+appShowEndpoint); err != nil {
		m.log.Warn("running instance not asked to show its window", "err", err)
	}
	return exitOK
}

func postOwner(ctx context.Context, client *http.Client, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, http.NoBody)
	if err != nil {
		return fmt.Errorf("build request to %s: %w", target, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("post %s answered %d", target, resp.StatusCode)
	}
	return nil
}

func (m *machine) showWindow() {
	if m.env.Headless {
		return
	}
	if !m.window.Available() {
		m.log.Warn("webview2 unavailable, owner view opened in the browser")
		m.openOwnerView()
		return
	}
	if err := os.MkdirAll(filepath.Join(m.dataDir, webviewDirName), webviewDirPerm); err != nil {
		m.log.Warn("webview profile folder not writable, owner view opened in the browser", "err", err)
		m.openOwnerView()
		return
	}
	m.window.Show()
}

func (m *machine) openOwnerView() {
	if err := m.openURL(m.ownerURL()); err != nil {
		m.log.Warn("owner view not opened", "err", err)
	}
}

func (m *machine) watchEvents(ctx context.Context) {
	for {
		stream, unsubscribe := m.hub.Subscribe(events.Scope{Owner: true})
		m.react(ctx, stream)
		unsubscribe()
		if ctx.Err() != nil {
			return
		}
	}
}

func (m *machine) react(ctx context.Context, stream <-chan events.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-stream:
			if !ok {
				return
			}
			switch payload := e.Payload.(type) {
			case api.DeviceChange:
				if payload.Action == api.DeviceRequested {
					m.showWindow()
				}
			case update.Status:
				tray.SetUpdateReady(payload.State == update.StateReady)
			}
		}
	}
}

func (m *machine) installSendTo() {
	err := platform.InstallSendTo(m.exePath)
	switch {
	case errors.Is(err, platform.ErrUnsupported):
		m.log.Debug("send to shortcut not supported on this platform")
	case err != nil:
		m.log.Warn("send to shortcut not installed", "err", err)
	}
}

func (m *machine) trayActions(ctx context.Context, bg *background) tray.Actions {
	return tray.Actions{
		Open:         m.showWindow,
		SendFiles:    func() { bg.Go(func() { m.offerPicked(ctx) }) },
		OpenReceived: m.openReceivedDir,
		Update: func() {
			bg.Go(func() {
				if err := m.applyUpdate(ctx); err != nil {
					m.log.Error("update not applied from the tray", "err", err)
				}
			})
		},
		Quit: m.quit,
	}
}

type background struct {
	mu       sync.Mutex
	isClosed bool
	wg       sync.WaitGroup
}

func (b *background) Go(task func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.isClosed {
		b.wg.Go(task)
	}
}

func (b *background) Wait() {
	b.mu.Lock()
	b.isClosed = true
	b.mu.Unlock()
	b.wg.Wait()
}
