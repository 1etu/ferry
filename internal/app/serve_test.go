package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/config"
)

const (
	uploadSize   = 3 << 20
	uploadedPart = 1 << 20
)

type instance struct {
	cancel context.CancelFunc
	exited chan int
}

type lifecycle struct {
	t        *testing.T
	env      config.Env
	baseURL  string
	received string
	logs     *syncBuffer
	owner    *http.Client
}

func newLifecycle(t *testing.T) *lifecycle {
	t.Helper()
	dir := t.TempDir()
	received := t.TempDir()
	writeConfig(t, dir, map[string]any{"receivedDir": received})
	port := freePort(t)
	l := &lifecycle{
		t:        t,
		env:      config.Env{DataDir: dir, Headless: true, Port: port},
		baseURL:  loopbackURL(port),
		received: received,
		logs:     &syncBuffer{},
		owner:    &http.Client{Timeout: 10 * time.Second},
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(l.logs.String())
		}
	})
	return l
}

func (l *lifecycle) serve(ctx context.Context) int {
	return serve(ctx, l.env, l.env.DataDir, loopbackHost, slog.New(slog.NewTextHandler(l.logs, nil)), launch{})
}

func (l *lifecycle) start() instance {
	l.t.Helper()
	ctx, cancel := context.WithCancel(context.WithoutCancel(l.t.Context()))
	exited := make(chan int, 1)
	go func() { exited <- l.serve(ctx) }()
	l.waitHealthy()
	return instance{cancel: cancel, exited: exited}
}

func (l *lifecycle) waitHealthy() {
	l.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !isRunning(l.t.Context(), l.owner, l.baseURL) {
		if time.Now().After(deadline) {
			l.t.Fatal("instance did not become healthy")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (l *lifecycle) stop(i instance) {
	l.t.Helper()
	started := time.Now()
	i.cancel()
	l.expectExit(i, exitOK)
	if elapsed := time.Since(started); elapsed > shutdownTimeout {
		l.t.Fatalf("shutdown took %s", elapsed)
	}
}

func (l *lifecycle) expectExit(i instance, want int) {
	l.t.Helper()
	select {
	case code := <-i.exited:
		if code != want {
			l.t.Fatalf("exit code %d, want %d", code, want)
		}
	case <-time.After(shutdownTimeout + 2*time.Second):
		l.t.Fatal("instance did not stop")
	}
}

func (l *lifecycle) call(c *http.Client, method, target string, body io.Reader, header map[string]string) *http.Response {
	l.t.Helper()
	req, err := http.NewRequestWithContext(l.t.Context(), method, l.baseURL+target, body)
	if err != nil {
		l.t.Fatal(err)
	}
	for key, value := range header {
		req.Header.Set(key, value)
	}
	resp, err := c.Do(req)
	if err != nil {
		l.t.Fatalf("%s %s: %v", method, target, err)
	}
	return resp
}

func (l *lifecycle) callJSON(c *http.Client, method, target string, body any, wantStatus int, out any) {
	l.t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		l.t.Fatal(err)
	}
	resp := l.call(c, method, target, bytes.NewReader(encoded), nil)
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		l.t.Fatalf("%s %s: %d", method, target, resp.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			l.t.Fatal(err)
		}
	}
}

func (l *lifecycle) uploadDone(id string) int64 {
	l.t.Helper()
	var transfers []api.Transfer
	l.callJSON(l.owner, http.MethodGet, "/api/transfers", nil, http.StatusOK, &transfers)
	for i := range transfers {
		if transfers[i].ID == id {
			return transfers[i].Done
		}
	}
	l.t.Fatalf("transfer %s not listed", id)
	return 0
}

func (l *lifecycle) waitForProgress(id string, want int64) {
	l.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for l.uploadDone(id) < want {
		if time.Now().After(deadline) {
			l.t.Fatal("upload progress never reached the server")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func randomContent(t *testing.T, size int) []byte {
	t.Helper()
	content := make([]byte, size)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	return content
}

func expectStatusFrom(t *testing.T, patched <-chan int, want int) {
	t.Helper()
	select {
	case status := <-patched:
		if status != want {
			t.Fatalf("in-flight PATCH answered %d, want %d", status, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("in-flight PATCH never answered")
	}
}

func expectFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s holds %d bytes that differ from the %d sent", path, len(got), len(want))
	}
}

func TestShutdownMidUploadKeepsItResumableAcrossRestart(t *testing.T) {
	t.Parallel()
	l := newLifecycle(t)
	first := l.start()
	if code := l.serve(t.Context()); code != exitOK {
		t.Fatalf("second instance exited %d, want %d", code, exitOK)
	}
	d := l.pairDevice()
	content := randomContent(t, uploadSize)
	target, nonce := d.create("clip.mov", uploadSize)
	patched := d.startPatch(target, nonce, uploadSize, content[:uploadedPart])
	l.waitForProgress(uploadID(target), uploadedPart)

	l.stop(first)
	expectStatusFrom(t, patched, http.StatusServiceUnavailable)

	second := l.start()
	defer l.stop(second)
	if done := l.uploadDone(uploadID(target)); done != uploadedPart {
		t.Fatalf("recovered progress %d, want %d", done, uploadedPart)
	}
	expired := d.tus(http.MethodHead, target, http.NoBody, nil)
	expired.Body.Close()
	if expired.StatusCode != http.StatusForbidden {
		t.Fatalf("HEAD with the session of the stopped instance: %d, want 403", expired.StatusCode)
	}
	d.handshake()
	offset := d.offset(target)
	if offset != strconv.Itoa(uploadedPart) {
		t.Fatalf("HEAD offset %q, want %d", offset, uploadedPart)
	}
	if status := d.patch(target, nonce, uploadedPart, content[uploadedPart:]); status != http.StatusNoContent {
		t.Fatalf("resumed PATCH: %d", status)
	}
	expectFile(t, filepath.Join(l.received, "clip.mov"), content)
}

func TestReceivedDirChangeRestartsTheStackWithAnUploadInFlight(t *testing.T) {
	t.Parallel()
	l := newLifecycle(t)
	running := l.start()
	defer l.stop(running)
	d := l.pairDevice()
	content := randomContent(t, uploadSize)
	target, nonce := d.create("movie.mov", uploadSize)
	patched := d.startPatch(target, nonce, uploadSize, content[:uploadedPart])
	l.waitForProgress(uploadID(target), uploadedPart)

	moved := filepath.Join(t.TempDir(), "Moved")
	var settings api.Settings
	l.callJSON(l.owner, http.MethodPatch, "/api/settings", api.SettingsPatch{ReceivedDir: &moved}, http.StatusOK, &settings)
	if settings.ReceivedDir != moved {
		t.Fatalf("settings %+v", settings)
	}
	expectStatusFrom(t, patched, http.StatusServiceUnavailable)
	l.waitHealthy()
	if offset := d.offset(target); offset != strconv.Itoa(uploadedPart) {
		t.Fatalf("HEAD offset %q after the move, want %d", offset, uploadedPart)
	}
	if status := d.patch(target, nonce, uploadedPart, content[uploadedPart:]); status != http.StatusNoContent {
		t.Fatalf("resumed PATCH: %d", status)
	}
	expectFile(t, filepath.Join(moved, "movie.mov"), content)
	if _, err := os.Stat(filepath.Join(l.received, ".incoming")); !os.IsNotExist(err) {
		t.Fatalf("old incoming folder still there: %v", err)
	}
	cfg, err := loadConfig(config.Env{}, l.env.DataDir, "unused")
	if err != nil || cfg.ReceivedDir != moved {
		t.Fatalf("saved received dir %q, %v", cfg.ReceivedDir, err)
	}
}
