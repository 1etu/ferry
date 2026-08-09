package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/config"
	"github.com/1etu/ferry/internal/platform"
)

const (
	sendTimeout       = 30 * time.Second
	startPollInterval = 250 * time.Millisecond
	startTimeout      = 10 * time.Second
)

type sender struct {
	client       *http.Client
	baseURL      string
	start        func() error
	pollInterval time.Duration
	startTimeout time.Duration
}

func runSend(ctx context.Context, paths []string) int {
	port, err := sendPort(config.ReadEnv())
	if err != nil {
		return exitFailure
	}
	absolute := make([]string, len(paths))
	for i, path := range paths {
		if absolute[i], err = filepath.Abs(path); err != nil {
			return exitFailure
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return exitFailure
	}
	//nolint:contextcheck
	if err := newSender(port, exe).offer(ctx, absolute); err != nil {
		return exitFailure
	}
	return exitOK
}

func newSender(port int, exe string) sender {
	return sender{
		client:       &http.Client{Timeout: sendTimeout},
		baseURL:      loopbackURL(port),
		start:        func() error { return platform.StartDetached(exe) },
		pollInterval: startPollInterval,
		startTimeout: startTimeout,
	}
}

func sendPort(env config.Env) (int, error) {
	if env.Port != 0 {
		return env.Port, nil
	}
	dir, err := dataDir(env)
	if err != nil {
		return 0, err
	}
	hostname, err := platform.Hostname()
	if err != nil {
		return 0, err
	}
	cfg, err := loadConfig(env, dir, hostname)
	if err != nil {
		return 0, err
	}
	return cfg.Port, nil
}

func (s sender) offer(ctx context.Context, paths []string) error {
	body, err := json.Marshal(api.OfferRequest{Paths: paths})
	if err != nil {
		return fmt.Errorf("encode offer: %w", err)
	}
	status, err := s.post(ctx, body)
	if platform.IsConnectionRefused(err) {
		if err := s.startServer(ctx); err != nil {
			return err
		}
		status, err = s.post(ctx, body)
	}
	if err != nil {
		return err
	}
	if status != http.StatusCreated {
		return fmt.Errorf("offer answered %d", status)
	}
	return nil
}

func (s sender) post(ctx context.Context, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/files", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build offer request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post offer: %w", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func (s sender) startServer(ctx context.Context) error {
	if err := s.start(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.startTimeout)
	defer cancel()
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for !isRunning(ctx, s.client, s.baseURL) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for started server: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	return nil
}

func (m *machine) offerPicked(ctx context.Context) {
	paths, err := m.platform.pickFiles(ctx, pickTitle)
	if err != nil {
		if ctx.Err() == nil {
			m.log.Error("file picker failed", "err", err)
		}
		return
	}
	if len(paths) == 0 {
		return
	}
	if err := m.stack.offer(ctx, paths); err != nil {
		m.log.Warn("picked files not offered", "err", err)
	}
}
