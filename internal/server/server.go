package server

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
	"github.com/1etu/ferry/internal/config"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/inbox"
	"github.com/1etu/ferry/internal/outbox"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/store"
	"github.com/1etu/ferry/internal/update"
)

type Deps struct {
	Config        config.Config
	Version       string
	Name          func() string
	Origins       func() (local, ip string)
	Store         *store.Store
	Auth          *auth.Auth
	Pairings      *auth.Pairings
	Sessions      *seal.Sessions
	Inbox         *inbox.Inbox
	Outbox        *outbox.Outbox
	Hub           *events.Hub
	Updater       *update.Updater
	Settings      func() api.Settings
	Network       func() api.Network
	Pick          func(ctx context.Context) ([]string, error)
	PickFolder    func(ctx context.Context) (string, error)
	OpenReceived  func(path string) error
	ShowWindow    func()
	Quit          func()
	ApplySettings func(ctx context.Context, s api.Settings) error
	ApplyUpdate   func(ctx context.Context) error
	AllowFirewall func(ctx context.Context) error
	WebUI         fs.FS
	Log           *slog.Logger
}

type server struct {
	Deps
	eventStream http.Handler
	uploads     http.Handler
	handshakeMu sync.Mutex
}

func New(d Deps) http.Handler {
	s := newServer(d)
	return s.chain(s.mux())
}

func newServer(d Deps) *server {
	s := &server{Deps: d}
	s.eventStream = d.Hub.Handler(s.eventScope)
	s.uploads = http.StripPrefix("/api/uploads", d.Inbox.Handler())
	return s
}

func (s *server) mux() *http.ServeMux {
	mux := http.NewServeMux()
	for _, rt := range routes() {
		serve := rt.serve
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serve(s, w, r) })
		mux.Handle(rt.pattern, s.guard(rt.access, s.requireSeal(rt.sealing, handler)))
	}
	mux.HandleFunc("/api/", s.apiNotFound)
	mux.HandleFunc("/", s.static)
	return mux
}

func (s *server) guard(a access, next http.Handler) http.Handler {
	switch a {
	case owner:
		return s.Auth.RequireOwner(next)
	case ownerOrApprovedDevice:
		return s.Auth.RequireOwnerOrApprovedDevice(next)
	case approvedDevice:
		return s.Auth.RequireApprovedDevice(next)
	default:
		return next
	}
}

func (s *server) eventScope(r *http.Request) (events.Scope, bool) {
	p := auth.FromContext(r.Context())
	switch p.Role {
	case auth.RoleOwner:
		return events.Scope{Owner: true}, true
	case auth.RoleDevice:
		scope := events.Scope{DeviceID: p.Device.ID, IsApproved: p.Device.Status == store.DeviceApproved}
		if scope.IsApproved {
			scope.Seal = s.Sessions.Sealer(p.Device.ID)
		}
		return scope, true
	default:
		return events.Scope{}, false
	}
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	s.eventStream.ServeHTTP(w, r)
}

func (s *server) upload(w http.ResponseWriter, r *http.Request) {
	s.uploads.ServeHTTP(w, r)
}
