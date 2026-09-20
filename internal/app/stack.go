package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/1etu/ferry/internal/auth"
	"github.com/1etu/ferry/internal/config"
	"github.com/1etu/ferry/internal/inbox"
	"github.com/1etu/ferry/internal/outbox"
	"github.com/1etu/ferry/internal/platform"
	"github.com/1etu/ferry/internal/server"
	"github.com/1etu/ferry/internal/store"
	"github.com/1etu/ferry/internal/webui"
)

const (
	dbFileName        = "ferry.db"
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 2 * time.Minute
	sweepAge          = 7 * 24 * time.Hour
	sweepInterval     = time.Hour
)

var (
	errStackClosed  = errors.New("stack closed for good")
	errStackStopped = errors.New("stack stopped")
)

type listenError struct {
	err error
}

func (e *listenError) Error() string {
	return "listen: " + e.err.Error()
}

func (e *listenError) Unwrap() error {
	return e.err
}

type stack struct {
	m     *machine
	hooks server.Deps

	mu       sync.Mutex
	live     *liveStack
	isClosed bool
	failure  error
}

type liveStack struct {
	cfg         config.Config
	sv          *services
	srv         *http.Server
	cancelBase  context.CancelCauseFunc
	served      chan error
	stopJanitor context.CancelFunc
	janitorDone chan struct{}
}

type services struct {
	store   *store.Store
	inbox   *inbox.Inbox
	outbox  *outbox.Outbox
	handler http.Handler
}

func (s *stack) start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosed {
		return errStackClosed
	}
	if s.live != nil {
		return nil
	}
	cfg := s.m.config()
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", net.JoinHostPort(s.m.listenHost, strconv.Itoa(s.m.port)))
	if err != nil {
		return &listenError{err: err}
	}
	sv, err := s.openServices(ctx, cfg)
	if err != nil {
		return errors.Join(err, listener.Close())
	}
	base, cancelBase := context.WithCancelCause(context.WithoutCancel(ctx))
	live := &liveStack{
		cfg: cfg,
		sv:  sv,
		srv: &http.Server{
			Handler:           sv.handler,
			ReadHeaderTimeout: readHeaderTimeout,
			IdleTimeout:       idleTimeout,
			BaseContext:       func(net.Listener) context.Context { return base },
		},
		cancelBase:  cancelBase,
		served:      make(chan error, 1),
		janitorDone: make(chan struct{}),
	}
	go func() {
		err := live.srv.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			s.m.quit()
		}
		live.served <- err
	}()
	var janitorCtx context.Context
	janitorCtx, live.stopJanitor = context.WithCancel(ctx)
	go func() {
		defer close(live.janitorDone)
		sweepPeriodically(janitorCtx, sv, s.m)
	}()
	s.live = live
	s.m.log.Info("listening", "addr", listener.Addr().String(), "version", Version, "path", cfg.ReceivedDir)
	return nil
}

func (s *stack) stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked(ctx)
}

func (s *stack) close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isClosed = true
	return errors.Join(s.stopLocked(ctx), s.failure)
}

func (s *stack) fail(err error) {
	s.mu.Lock()
	s.failure = err
	s.mu.Unlock()
	s.m.quit()
}

func (s *stack) receivedDir() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live == nil {
		return "", false
	}
	return s.live.cfg.ReceivedDir, true
}

func (s *stack) stopLocked(ctx context.Context) error {
	live := s.live
	if live == nil {
		return nil
	}
	s.live = nil
	log := s.m.log
	log.Info("stopping transfer stack")
	live.stopJanitor()
	<-live.janitorDone
	live.cancelBase(inbox.ErrShutdown)
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := live.srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("connections still open at shutdown deadline", "err", err)
	}
	served := <-live.served
	live.sv.close(context.WithoutCancel(ctx), s.m)
	if !errors.Is(served, http.ErrServerClosed) {
		return fmt.Errorf("server stopped unexpectedly: %w", served)
	}
	return nil
}

func (s *stack) openServices(ctx context.Context, cfg config.Config) (*services, error) {
	m := s.m
	st, err := store.Open(ctx, filepath.Join(m.dataDir, dbFileName))
	if err != nil {
		return nil, err
	}
	au := auth.New(auth.Config{Hosts: m.hosts, Port: m.port, Dev: m.env.Dev, Now: time.Now}, st, m.log)
	pairings := auth.NewPairings(st, m.hub, m.origins, time.Now, m.log)
	pairings.OnRevoke = m.sessions.Drop
	in, err := inbox.New(inbox.Config{
		ReceivedDir:        cfg.ReceivedDir,
		IncomingDir:        cfg.IncomingDir(),
		MaxUploadBytes:     cfg.MaxUploadBytes,
		ReserveBytes:       cfg.ReserveBytes,
		MaxActivePerDevice: cfg.MaxActiveUploadsPerDevice,
		DeviceID:           deviceOf,
		Session:            inbox.SessionFromContext,
		FreeSpace:          platform.FreeSpace,
	}, st, m.hub, m.log)
	if err != nil {
		return nil, errors.Join(err, st.Close())
	}
	if err := in.Recover(ctx); err != nil {
		m.log.Error("interrupted uploads not recovered", "err", err)
	}
	out := outbox.New(st, m.hub, deviceOf, m.log)
	deps := s.hooks
	deps.Config = cfg
	deps.Version = Version
	deps.Store = st
	deps.Auth = au
	deps.Pairings = pairings
	deps.Inbox = in
	deps.Outbox = out
	deps.WebUI = webui.FS()
	deps.Log = m.log
	return &services{store: st, inbox: in, outbox: out, handler: server.New(deps)}, nil
}

func (sv *services) close(ctx context.Context, m *machine) {
	if err := sv.inbox.Close(ctx); err != nil {
		m.log.Error("upload progress not saved at shutdown", "err", err)
	}
	if err := sv.store.Close(); err != nil {
		m.log.Error("database not closed cleanly", "err", err)
	}
}

func deviceOf(ctx context.Context) (string, bool) {
	p := auth.FromContext(ctx)
	return p.Device.ID, p.Role == auth.RoleDevice
}

func sweepPeriodically(ctx context.Context, sv *services, m *machine) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		if err := sv.inbox.Sweep(ctx, sweepAge); err != nil && ctx.Err() == nil {
			m.log.Error("stale uploads not swept", "err", err)
		}
		if err := sv.outbox.Sweep(ctx, sweepAge); err != nil && ctx.Err() == nil {
			m.log.Error("stale offered files not swept", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *stack) offer(ctx context.Context, paths []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live == nil {
		return errStackStopped
	}
	_, err := s.live.sv.outbox.Offer(ctx, paths)
	return err
}
