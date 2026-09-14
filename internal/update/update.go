package update

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/1etu/ferry/internal/events"
)

const (
	DefaultManifestURL = "https://github.com/1etu/ferry/releases/latest/download/manifest.json"
	manifestSuffix     = "/latest/download/manifest.json"
	cacheFileName      = "update.json"
	devVersion         = "dev"
	envManifestURL     = "FERRY_UPDATE_MANIFEST_URL"
	envPublicKey       = "FERRY_UPDATE_PUBLIC_KEY"
	firstCheckDelay    = time.Minute
	checkInterval      = 24 * time.Hour
)

var (
	ErrDisabled = errors.New("updates are disabled")
	ErrBusy     = errors.New("update check in progress")
	ErrReplay   = errors.New("manifest older than the last verified one")
)

type Config struct {
	Current     string
	ManifestURL string
	PublicKey   ed25519.PublicKey
	DataDir     string
	ExePath     string
	Client      *http.Client
	Now         func() time.Time
	Launch      func(exePath string, args ...string) (*os.Process, error)
	Dev         bool
}

type Updater struct {
	current       Version
	manifestURL   *url.URL
	releaseURL    string
	key           ed25519.PublicKey
	cachePath     string
	exePath       string
	client        *http.Client
	now           func() time.Time
	launch        func(exePath string, args ...string) (*os.Process, error)
	hub           *events.Hub
	log           *slog.Logger
	canCheck      bool
	firstDelay    time.Duration
	interval      time.Duration
	healthTimeout time.Duration

	mu      sync.Mutex
	status  Status
	isBusy  bool
	pending *Manifest
	staged  string
}

func New(cfg Config, hub *events.Hub, log *slog.Logger) *Updater {
	cfg = withDevOverrides(cfg, log)
	u := &Updater{
		key:           cfg.PublicKey,
		cachePath:     filepath.Join(cfg.DataDir, cacheFileName),
		exePath:       cfg.ExePath,
		now:           cfg.Now,
		launch:        cfg.Launch,
		hub:           hub,
		log:           log,
		firstDelay:    firstCheckDelay,
		interval:      checkInterval,
		healthTimeout: healthTimeout,
		status:        Status{Current: cfg.Current, State: StateDisabled},
	}
	if u.key == nil {
		u.key = publicKey
	}
	if u.now == nil {
		u.now = time.Now
	}
	if u.launch == nil {
		u.launch = launchDetached
	}
	u.client = redirectGuardedClient(cfg.Client, u.checkRedirect)
	switch err := u.configure(cfg); {
	case err == nil:
		u.canCheck = true
		u.status.State = StateIdle
	case cfg.Current == devVersion:
		log.Info("updates disabled for dev build")
	default:
		log.Warn("updates disabled", "err", err)
	}
	return u
}

func withDevOverrides(cfg Config, log *slog.Logger) Config {
	if !cfg.Dev {
		return cfg
	}
	if raw := os.Getenv(envManifestURL); raw != "" {
		cfg.ManifestURL = raw
	}
	raw := os.Getenv(envPublicKey)
	if raw == "" {
		return cfg
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		log.Warn("ignoring dev public key", "env", envPublicKey, "err", err)
		return cfg
	}
	if len(key) != ed25519.PublicKeySize {
		log.Warn("ignoring dev public key, wrong length", "env", envPublicKey, "bytes", len(key))
		return cfg
	}
	cfg.PublicKey = key
	return cfg
}

func (u *Updater) configure(cfg Config) error {
	if cfg.Current == devVersion {
		return errors.New("dev build")
	}
	current, err := ParseVersion(cfg.Current)
	if err != nil {
		return fmt.Errorf("current version %q: %w", cfg.Current, err)
	}
	if isPlaceholderKey(u.key) {
		return errors.New("no public key embedded")
	}
	rawURL := cmp.Or(cfg.ManifestURL, DefaultManifestURL)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("manifest url: %w", err)
	}
	if parsed.Scheme != "https" && !cfg.Dev {
		return fmt.Errorf("manifest url %s is not https", rawURL)
	}
	if !strings.HasSuffix(parsed.Path, manifestSuffix) {
		return fmt.Errorf("manifest url %s does not end with %s", rawURL, manifestSuffix)
	}
	u.current = current
	u.manifestURL = parsed
	u.releaseURL = strings.TrimSuffix(rawURL, manifestSuffix)
	return nil
}

func (u *Updater) Run(ctx context.Context, enabled func() bool) {
	timer := time.NewTimer(u.firstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if enabled() {
			u.Check(ctx)
		}
		timer.Reset(u.interval)
	}
}

func (u *Updater) Check(ctx context.Context) Status {
	if err := u.acquire(); err != nil {
		return u.Status()
	}
	defer u.release()
	u.beginCheck()
	return u.runCheck(ctx)
}

func (u *Updater) StartCheck(ctx context.Context) Status {
	if err := u.acquire(); err != nil {
		return u.Status()
	}
	started := u.beginCheck()
	go func() {
		defer u.release()
		u.runCheck(ctx)
	}()
	return started
}

func (u *Updater) beginCheck() Status {
	return u.set(func(s *Status) { s.State, s.Error = StateChecking, "" })
}

func (u *Updater) runCheck(ctx context.Context) Status {
	state, available, err := u.check(ctx)
	if err != nil {
		u.log.Warn("update check failed", "err", err)
	}
	return u.finish(state, available, err)
}

func (u *Updater) check(ctx context.Context) (State, string, error) {
	c, err := loadCache(u.cachePath)
	if err != nil {
		u.log.Warn("update cache ignored", "path", u.cachePath, "err", err)
	}
	fetched, err := u.fetchManifest(ctx, u.etagIfNothingPending(c))
	if err != nil {
		return StateFailed, "", err
	}
	if fetched.isUnchanged {
		return u.unchangedOutcome()
	}
	if last, err := ParseVersion(c.Version); c.Version != "" && err == nil && fetched.version.Less(last) {
		return StateFailed, "", fmt.Errorf("%w: %s after %s", ErrReplay, fetched.manifest.Version, c.Version)
	}
	c.ETag, c.Version = fetched.etag, fetched.manifest.Version
	if err := saveCache(u.cachePath, c); err != nil {
		u.log.Warn("update cache not saved", "path", u.cachePath, "err", err)
	}
	switch {
	case !u.current.Less(fetched.version):
		return StateIdle, "", nil
	case fetched.manifest.Version == c.Failed:
		u.log.Info("update skipped after a failed install", "version", c.Failed)
		return StateIdle, "", nil
	}
	return u.stageManifest(ctx, fetched.manifest)
}

func (u *Updater) etagIfNothingPending(c cache) string {
	last, err := ParseVersion(c.Version)
	if u.stagedVersion() != "" || err != nil || !u.current.Less(last) || c.Version == c.Failed {
		return c.ETag
	}
	return ""
}

func (u *Updater) unchangedOutcome() (State, string, error) {
	if staged := u.stagedVersion(); staged != "" {
		return StateReady, staged, nil
	}
	return StateIdle, "", nil
}
