package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/config"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/platform"
)

type platformCalls struct {
	runAtLogin      func() (bool, error)
	setRunAtLogin   func(exePath string, enabled bool) error
	firewallState   func(ctx context.Context, exePath string) (platform.Firewall, error)
	addFirewallRule func(ctx context.Context, exePath string) error
	pickFiles       func(ctx context.Context, title string) ([]string, error)
	pickFolder      func(ctx context.Context, title string) (string, error)
}

func systemCalls() platformCalls {
	return platformCalls{
		runAtLogin:      platform.RunAtLogin,
		setRunAtLogin:   platform.SetRunAtLogin,
		firewallState:   platform.FirewallState,
		addFirewallRule: platform.AddFirewallRule,
		pickFiles:       platform.PickFiles,
		pickFolder:      platform.PickFolder,
	}
}

func (m *machine) settings() api.Settings {
	cfg := m.config()
	return api.Settings{
		Name:         cfg.Name,
		ReceivedDir:  cfg.ReceivedDir,
		StartAtLogin: m.startsAtLogin(),
		CheckUpdates: cfg.CheckUpdates,
	}
}

func (m *machine) startsAtLogin() bool {
	isOn, err := m.platform.runAtLogin()
	if err != nil && !errors.Is(err, platform.ErrUnsupported) {
		m.log.Warn("start at login unknown, reported off", "err", err)
	}
	return isOn && err == nil
}

func (m *machine) applySettings(ctx context.Context, bg *background, s api.Settings) error {
	if s.StartAtLogin != m.startsAtLogin() {
		if err := m.platform.setRunAtLogin(m.exePath, s.StartAtLogin); err != nil {
			return fmt.Errorf("set start at login: %w", err)
		}
	}
	current := m.config()
	next := current
	next.Name, next.ReceivedDir, next.CheckUpdates = s.Name, s.ReceivedDir, s.CheckUpdates
	if next == current {
		return nil
	}
	if err := m.saveConfig(next); err != nil {
		return err
	}
	m.setConfig(next)
	m.log.Info("settings changed", "path", next.ReceivedDir, "check_updates", next.CheckUpdates)
	if next.ReceivedDir != current.ReceivedDir {
		bg.Go(func() { m.moveReceivedDir(ctx) })
	}
	return nil
}

func (m *machine) saveConfig(next config.Config) error {
	path := filepath.Join(m.dataDir, configFileName)
	defaults, err := defaultConfig(m.hostname)
	if err != nil {
		return err
	}
	stored, err := config.Load(path, defaults)
	if err != nil {
		return err
	}
	stored.Name, stored.ReceivedDir, stored.CheckUpdates = next.Name, next.ReceivedDir, next.CheckUpdates
	return stored.Save(path)
}

func (m *machine) moveReceivedDir(ctx context.Context) {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	from, isLive := m.stack.receivedDir()
	to := m.config().ReceivedDir
	if !isLive || from == to {
		return
	}
	if err := m.stack.stop(ctx); err != nil {
		m.log.Error("transfer stack failed before the received folder moved", "err", err)
	}
	fromIncoming, toIncoming := config.Config{ReceivedDir: from}.IncomingDir(), config.Config{ReceivedDir: to}.IncomingDir()
	if err := moveIncoming(fromIncoming, toIncoming); err != nil {
		m.log.Warn("partial uploads not moved, they expire and restart", "path", toIncoming, "err", err)
		if err := os.RemoveAll(fromIncoming); err != nil {
			m.log.Warn("partial uploads left in the old received folder", "path", fromIncoming, "err", err)
		}
	}
	err := m.stack.start(ctx)
	if err == nil || errors.Is(err, errStackClosed) {
		return
	}
	m.log.Error("transfer stack not started in the new received folder, keeping the old one", "path", to, "err", err)
	restored := m.config()
	restored.ReceivedDir = from
	if err := m.saveConfig(restored); err != nil {
		m.log.Error("old received folder not saved back", "err", err)
	}
	m.setConfig(restored)
	if err := m.stack.start(ctx); err != nil && !errors.Is(err, errStackClosed) {
		m.stack.fail(fmt.Errorf("restart with received folder %s: %w", from, err))
	}
}

func moveIncoming(from, to string) error {
	if _, err := os.Stat(from); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err := os.Remove(to); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clear %s: %w", to, err)
	}
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("move %s to %s: %w", from, to, err)
	}
	return nil
}

func (m *machine) openReceived(path string) error {
	if path == m.config().ReceivedDir {
		return platform.OpenFolder(path)
	}
	return platform.RevealFile(path)
}

func (m *machine) openReceivedDir() {
	if err := platform.OpenFolder(m.config().ReceivedDir); err != nil {
		m.log.Warn("received folder not opened", "err", err)
	}
}

const (
	firewallCheckDelay = 3 * time.Second
	firewallPromptWait = 60 * time.Second
)

type networkState struct {
	mu        sync.Mutex
	last      api.Network
	isChecked bool
}

func unknownNetwork() api.Network {
	return api.Network{Firewall: api.FirewallUnknown, Profile: string(platform.ProfileUnknown)}
}

func (n *networkState) current() api.Network {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.isChecked {
		return unknownNetwork()
	}
	return n.last
}

func (n *networkState) set(v api.Network) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.last, n.isChecked = v, true
}

func (m *machine) checkFirewallAfter(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	m.checkFirewall(ctx)
}

func (m *machine) checkFirewall(ctx context.Context) {
	fw, err := m.platform.firewallState(ctx, m.exePath)
	result := unknownNetwork()
	switch {
	case errors.Is(err, platform.ErrUnsupported):
		m.log.Debug("firewall check not supported on this platform")
	case err != nil && ctx.Err() != nil:
		return
	case err != nil:
		m.log.Warn("firewall state unknown", "err", err)
	default:
		result = networkFrom(fw)
	}
	m.network.set(result)
	m.log.Info("firewall checked", "firewall", result.Firewall, "profile", result.Profile)
	m.hub.Publish(events.Event{Kind: events.KindNetwork, OwnerOnly: true, Payload: result})
}

func networkFrom(fw platform.Firewall) api.Network {
	state := api.FirewallBlocked
	if fw.Allowed {
		state = api.FirewallAllowed
	}
	return api.Network{Firewall: state, Profile: string(fw.Profile)}
}

func (m *machine) allowFirewall(reqCtx, ctx context.Context, bg *background) error {
	promptCtx, cancel := context.WithTimeout(reqCtx, firewallPromptWait)
	defer cancel()
	if err := m.platform.addFirewallRule(promptCtx, m.exePath); err != nil {
		return fmt.Errorf("add firewall rule: %w", err)
	}
	m.log.Info("firewall rule added", "path", m.exePath)
	bg.Go(func() { m.checkFirewall(ctx) })
	return nil
}
