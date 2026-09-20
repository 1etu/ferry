package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/1etu/ferry/internal/config"
	"github.com/1etu/ferry/internal/install"
)

const (
	quitEndpoint     = "/api/app/quit"
	quitWaitTimeout  = 10 * time.Second
	quitPollInterval = 250 * time.Millisecond
	quitProbeTimeout = time.Second
)

func runUninstall(ctx context.Context, stderr io.Writer) int {
	env := config.ReadEnv()
	dir, err := dataDir(env)
	if err != nil {
		reportFailure(stderr, err)
		return exitFailure
	}
	port := portOrDefault(env)
	client := &http.Client{Timeout: quitProbeTimeout}
	quit := func(ctx context.Context) error {
		return quitRunning(ctx, client, loopbackURL(port), quitWaitTimeout, quitPollInterval)
	}
	if err := install.Uninstall(ctx, dir, quit); err != nil {
		reportFailure(stderr, err)
		return exitFailure
	}
	return exitOK
}

func quitRunning(ctx context.Context, client *http.Client, baseURL string, wait, interval time.Duration) error {
	if !isRunning(ctx, client, baseURL) {
		return nil
	}
	if err := postOwner(ctx, client, baseURL+quitEndpoint); err != nil {
		return fmt.Errorf("ask the running instance to quit: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for isRunning(ctx, client, baseURL) {
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("running instance still answering after %s: %w", wait, err)
	}
	return nil
}

func portOrDefault(env config.Env) int {
	if port, err := sendPort(env); err == nil {
		return port
	}
	return config.Defaults("", "").Port
}
