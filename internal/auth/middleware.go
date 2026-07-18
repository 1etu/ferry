package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

const (
	touchInterval   = time.Minute
	hostsCacheTTL   = 10 * time.Second
	jsonContentType = "application/json; charset=utf-8"
)

var errNoDevice = errors.New("no device for cookie")

type Config struct {
	Hosts func() []string
	Port  int
	Dev   bool
	Now   func() time.Time
}

type Auth struct {
	cfg   Config
	port  string
	store *store.Store
	log   *slog.Logger

	mu              sync.Mutex
	hosts           []string
	hostsFetchedAt  time.Time
	cookieRenewedAt map[string]time.Time
}

func New(cfg Config, st *store.Store, log *slog.Logger) *Auth {
	return &Auth{
		cfg:             cfg,
		port:            strconv.Itoa(cfg.Port),
		store:           st,
		log:             log,
		cookieRenewedAt: make(map[string]time.Time),
	}
}

func (a *Auth) CheckHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, port := splitHostPort(r.Host)
		if !a.isAllowedHost(host, port) {
			a.log.Warn("request rejected, host not allowed", "host", r.Host, "remote", r.RemoteAddr)
			a.writeError(w, http.StatusMisdirectedRequest, api.CodeForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Auth) Identify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.cfg.Dev {
			r = withForwardedRemote(r)
		}
		p, err := a.identify(w, r)
		if err != nil {
			a.log.Error("request not identified", "remote", r.RemoteAddr, "err", err)
			a.writeError(w, http.StatusInternalServerError, api.CodeInternal)
			return
		}
		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), p)))
	})
}

func (a *Auth) CheckOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if isSafeMethod(r.Method) || origin == "" || a.isAllowedOrigin(origin) {
			next.ServeHTTP(w, r)
			return
		}
		a.log.Warn("request rejected, origin not allowed", "origin", origin, "remote", r.RemoteAddr)
		a.writeError(w, http.StatusForbidden, api.CodeForbidden)
	})
}

func (a *Auth) RequireOwner(next http.Handler) http.Handler {
	return a.guard(next, func(p Principal) (int, api.ErrorCode) {
		if p.Role == RoleOwner {
			return http.StatusOK, ""
		}
		return http.StatusForbidden, api.CodeForbidden
	})
}

func (a *Auth) RequireDevice(next http.Handler) http.Handler {
	return a.guard(next, func(p Principal) (int, api.ErrorCode) {
		switch p.Role {
		case RoleDevice:
			return http.StatusOK, ""
		case RoleNone:
			return http.StatusUnauthorized, api.CodeUnauthorized
		default:
			return http.StatusForbidden, api.CodeForbidden
		}
	})
}

func (a *Auth) RequireApprovedDevice(next http.Handler) http.Handler {
	return a.guard(next, func(p Principal) (int, api.ErrorCode) {
		switch {
		case p.isApprovedDevice():
			return http.StatusOK, ""
		case p.isPendingDevice():
			return http.StatusForbidden, api.CodePendingApproval
		case p.Role == RoleNone:
			return http.StatusUnauthorized, api.CodeUnauthorized
		default:
			return http.StatusForbidden, api.CodeForbidden
		}
	})
}

func (a *Auth) RequireOwnerOrApprovedDevice(next http.Handler) http.Handler {
	return a.guard(next, func(p Principal) (int, api.ErrorCode) {
		switch {
		case p.Role == RoleOwner, p.isApprovedDevice():
			return http.StatusOK, ""
		case p.isPendingDevice():
			return http.StatusForbidden, api.CodePendingApproval
		case p.Role == RoleNone:
			return http.StatusUnauthorized, api.CodeUnauthorized
		default:
			return http.StatusForbidden, api.CodeForbidden
		}
	})
}

func (a *Auth) guard(next http.Handler, decide func(Principal) (status int, code api.ErrorCode)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, code := decide(FromContext(r.Context()))
		if status != http.StatusOK {
			a.writeError(w, status, code)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Auth) identify(w http.ResponseWriter, r *http.Request) (Principal, error) {
	if cookie, err := r.Cookie(CookieName); err == nil {
		device, err := a.deviceForToken(r.Context(), cookie.Value)
		switch {
		case err == nil:
			return Principal{Role: RoleDevice, Device: a.markSeen(r.Context(), w, device, cookie.Value)}, nil
		case !errors.Is(err, errNoDevice):
			return Principal{}, err
		default:
			a.ClearCookie(w)
		}
	}
	if isLoopback(r.RemoteAddr) {
		return Principal{Role: RoleOwner}, nil
	}
	return Principal{Role: RoleNone}, nil
}

func (a *Auth) deviceForToken(ctx context.Context, token string) (store.Device, error) {
	device, err := a.store.DeviceByTokenHash(ctx, HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return store.Device{}, errNoDevice
	}
	if err != nil {
		return store.Device{}, fmt.Errorf("identify device: %w", err)
	}
	if device.Status == store.DeviceRevoked {
		return store.Device{}, errNoDevice
	}
	return device, nil
}

func (a *Auth) markSeen(ctx context.Context, w http.ResponseWriter, device store.Device, token string) store.Device {
	now := a.cfg.Now()
	if now.Sub(device.LastSeenAt) >= touchInterval {
		if err := a.store.TouchDevice(ctx, device.ID, now); err != nil {
			a.log.Warn("last seen not recorded, request continues", "device", device.ID, "err", err)
		} else {
			device.LastSeenAt = storedTime(now)
		}
	}
	if a.isCookieRenewalDue(device.ID, now) {
		a.SetCookie(w, token)
	}
	return device
}

func (a *Auth) isCookieRenewalDue(deviceID string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	renewedAt, ok := a.cookieRenewedAt[deviceID]
	if ok && now.Sub(renewedAt) < cookieRenewInterval {
		return false
	}
	a.cookieRenewedAt[deviceID] = now
	return true
}

func (a *Auth) writeError(w http.ResponseWriter, status int, code api.ErrorCode) {
	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(api.NewError(code, string(code))); err != nil {
		a.log.Debug("error response not written", "err", err)
	}
}
