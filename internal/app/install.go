package app

import (
	"context"
	"net/http"
	"runtime"

	"github.com/1etu/ferry/internal/config"
	"github.com/1etu/ferry/internal/install"
	"github.com/1etu/ferry/internal/platform"
)

const devVersion = "dev"

type installer struct {
	isInstalled func(exePath string) (bool, error)
	install     func(exePath, version string) (string, error)
	start       func(exePath string, args ...string) error
	isRunning   func(ctx context.Context) bool
	show        func(ctx context.Context) error
}

func needsSelfInstall(env config.Env, version, goos string) bool {
	return goos == "windows" && version != devVersion && !env.Headless && !env.Dev
}

func (m *machine) installFirstRun(ctx context.Context) (code int, isDone bool) {
	if !needsSelfInstall(m.env, Version, runtime.GOOS) {
		return exitOK, false
	}
	baseURL := loopbackURL(m.port)
	return m.selfInstall(ctx, installer{
		isInstalled: install.IsInstalled,
		install:     install.Install,
		start:       platform.StartDetached,
		isRunning: func(ctx context.Context) bool {
			probeCtx, cancel := context.WithTimeout(ctx, healthTimeout)
			defer cancel()
			return isRunning(probeCtx, http.DefaultClient, baseURL)
		},
		show: func(ctx context.Context) error { return postOwner(ctx, http.DefaultClient, baseURL+appShowEndpoint) },
	})
}

func (m *machine) selfInstall(ctx context.Context, in installer) (code int, isDone bool) {
	isInstalled, err := in.isInstalled(m.exePath)
	if err != nil {
		m.log.Error("install state unknown", "path", m.exePath, "err", err)
		return exitFailure, true
	}
	if isInstalled {
		return exitOK, false
	}
	if in.isRunning(ctx) {
		m.log.Info("installed copy already running, asking it to show its window")
		if err := in.show(ctx); err != nil {
			m.log.Warn("running instance not asked to show its window", "err", err)
		}
		return exitOK, true
	}
	installed, err := in.install(m.exePath, Version)
	if err != nil {
		m.log.Error("not installed", "path", m.exePath, "err", err)
		return exitFailure, true
	}
	m.log.Info("installed", "path", installed, "version", Version)
	if err := in.start(installed); err != nil {
		m.log.Error("installed copy not started", "path", installed, "err", err)
		return exitFailure, true
	}
	return exitOK, true
}
