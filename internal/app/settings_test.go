package app

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/config"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/platform"
)

type fakeRegistry struct {
	mu        sync.Mutex
	isOn      bool
	err       error
	writes    []bool
	writtenTo []string
}

func (r *fakeRegistry) runAtLogin() (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isOn, r.err
}

func (r *fakeRegistry) setRunAtLogin(exe string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.isOn = enabled
	r.writes = append(r.writes, enabled)
	r.writtenTo = append(r.writtenTo, exe)
	return nil
}

func settingsMachine(t *testing.T, registry *fakeRegistry) *machine {
	t.Helper()
	dir := t.TempDir()
	writeConfig(t, dir, map[string]any{"receivedDir": t.TempDir(), "port": 9000, "name": "pc"})
	cfg, err := loadConfig(config.Env{Port: 9100}, dir, "testpc")
	if err != nil {
		t.Fatal(err)
	}
	return &machine{
		dataDir:  dir,
		hostname: "testpc",
		port:     cfg.Port,
		exePath:  `C:\Programs\Ferry\Ferry.exe`,
		log:      slog.New(slog.DiscardHandler),
		cfg:      cfg,
		platform: platformCalls{runAtLogin: registry.runAtLogin, setRunAtLogin: registry.setRunAtLogin},
	}
}

func TestApplySettingsSavesTheFileAndWritesTheRegistryOnlyOnChange(t *testing.T) {
	t.Parallel()
	registry := &fakeRegistry{}
	m := settingsMachine(t, registry)
	var bg background
	next := m.settings()
	next.Name, next.CheckUpdates = "Studio", false
	if err := m.applySettings(t.Context(), &bg, next); err != nil {
		t.Fatal(err)
	}
	if len(registry.writes) != 0 {
		t.Fatalf("registry written %v for an unchanged start at login", registry.writes)
	}
	next.StartAtLogin = true
	if err := m.applySettings(t.Context(), &bg, next); err != nil {
		t.Fatal(err)
	}
	bg.Wait()
	if got := m.settings(); got != next {
		t.Fatalf("settings %+v, want %+v", got, next)
	}
	if len(registry.writes) != 1 || registry.writtenTo[0] != m.exePath {
		t.Fatalf("registry writes %v to %v", registry.writes, registry.writtenTo)
	}
	saved, err := config.Load(filepath.Join(m.dataDir, configFileName), config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Name != "Studio" || saved.CheckUpdates || saved.Port != 9000 {
		t.Fatalf("saved config %+v keeps neither the change nor the file's own port", saved)
	}
}

func TestStartAtLoginUnsupportedLeavesSettingsAlone(t *testing.T) {
	t.Parallel()
	registry := &fakeRegistry{err: platform.ErrUnsupported}
	m := settingsMachine(t, registry)
	var bg background
	next := m.settings()
	if next.StartAtLogin {
		t.Fatal("start at login reported on without a registry")
	}
	next.Name, next.StartAtLogin = "Den", true
	if err := m.applySettings(t.Context(), &bg, next); !errors.Is(err, platform.ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
	if m.settings().Name != "pc" {
		t.Fatalf("name changed to %q by a rejected patch", m.settings().Name)
	}
}

func TestFirewallCheckPublishesTheNetwork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		state platform.Firewall
		err   error
		want  api.Network
	}{
		{"allowed on a private network", platform.Firewall{Profile: platform.ProfilePrivate, Allowed: true}, nil, api.Network{Firewall: api.FirewallAllowed, Profile: "private"}},
		{"blocked on a public network", platform.Firewall{Profile: platform.ProfilePublic}, nil, api.Network{Firewall: api.FirewallBlocked, Profile: "public"}},
		{"unsupported platform", platform.Firewall{}, platform.ErrUnsupported, api.Network{Firewall: api.FirewallUnknown, Profile: "unknown"}},
		{"powershell failed", platform.Firewall{}, errors.New("exit status 1"), api.Network{Firewall: api.FirewallUnknown, Profile: "unknown"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &machine{log: slog.New(slog.DiscardHandler), hub: events.NewHub(slog.New(slog.DiscardHandler))}
			t.Cleanup(m.hub.Close)
			m.platform.firewallState = func(context.Context, string) (platform.Firewall, error) { return tc.state, tc.err }
			stream, unsubscribe := m.hub.Subscribe(events.Scope{Owner: true})
			t.Cleanup(unsubscribe)
			if got := m.network.current(); got != unknownNetwork() {
				t.Fatalf("before the first check %+v", got)
			}
			m.checkFirewall(t.Context())
			if got := m.network.current(); got != tc.want {
				t.Fatalf("network %+v, want %+v", got, tc.want)
			}
			if e := <-stream; e.Kind != events.KindNetwork || !e.OwnerOnly || e.Payload != tc.want {
				t.Fatalf("event %+v", e)
			}
		})
	}
}

func TestAllowFirewallRechecksAfterTheRuleIsAdded(t *testing.T) {
	t.Parallel()
	m := &machine{log: slog.New(slog.DiscardHandler), hub: events.NewHub(slog.New(slog.DiscardHandler))}
	t.Cleanup(m.hub.Close)
	isAdded := false
	m.platform.firewallState = func(context.Context, string) (platform.Firewall, error) {
		return platform.Firewall{Profile: platform.ProfilePublic, Allowed: isAdded}, nil
	}
	m.platform.addFirewallRule = func(context.Context, string) error { return platform.ErrCanceled }
	var bg background
	if err := m.allowFirewall(t.Context(), t.Context(), &bg); !errors.Is(err, platform.ErrCanceled) {
		t.Fatalf("declined prompt: %v", err)
	}
	m.platform.addFirewallRule = func(context.Context, string) error { isAdded = true; return nil }
	if err := m.allowFirewall(t.Context(), t.Context(), &bg); err != nil {
		t.Fatal(err)
	}
	bg.Wait()
	if got := m.network.current(); got.Firewall != api.FirewallAllowed {
		t.Fatalf("network after allow %+v", got)
	}
}
