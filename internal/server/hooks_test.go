package server

import (
	"context"
	"sync"

	"github.com/1etu/ferry/internal/api"
)

type hooks struct {
	mu              sync.Mutex
	settings        api.Settings
	applied         []api.Settings
	shows           int
	quits           int
	startAtLoginErr error
	firewallErr     error
	updatesApplied  chan struct{}
	firewallAllows  int
}

func newHooks(s api.Settings) *hooks {
	return &hooks{settings: s, updatesApplied: make(chan struct{}, 1)}
}

func (h *hooks) current() api.Settings {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.settings
}

func (h *hooks) network() api.Network {
	return api.Network{Firewall: api.FirewallBlocked, Profile: "public"}
}

func (h *hooks) showWindow() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.shows++
}

func (h *hooks) quit() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.quits++
}

func (h *hooks) applySettings(_ context.Context, s api.Settings) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.startAtLoginErr != nil && s.StartAtLogin != h.settings.StartAtLogin {
		return h.startAtLoginErr
	}
	h.settings = s
	h.applied = append(h.applied, s)
	return nil
}

func (h *hooks) applyUpdate(context.Context) error {
	h.updatesApplied <- struct{}{}
	return nil
}

func (h *hooks) allowFirewall(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.firewallAllows++
	return h.firewallErr
}

func (h *hooks) set(mutate func(*hooks)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	mutate(h)
}

func (h *hooks) calls() (shows, quits, settingsApplied, firewallAllowed int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.shows, h.quits, len(h.applied), h.firewallAllows
}
