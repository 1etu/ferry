package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/1etu/ferry/internal/update"
)

const releasePrefix = "/releases"

func releaseServer(t *testing.T, version string, asset []byte) (*httptest.Server, ed25519.PublicKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(asset)
	manifest, err := update.MarshalManifest(update.Manifest{
		Version: version,
		Assets: map[string]update.Asset{
			runtime.GOOS + "-" + runtime.GOARCH: {Name: "Ferry.exe", Size: int64(len(asset)), SHA256: hex.EncodeToString(sum[:])},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bodies := map[string][]byte{
		releasePrefix + "/latest/download/manifest.json":       manifest,
		releasePrefix + "/latest/download/manifest.sig":        []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, manifest))),
		releasePrefix + "/download/v" + version + "/Ferry.exe": asset,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := w.Write(body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, public
}

func launchExitingChild(string, ...string) (*os.Process, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self, "-test.run=^$")
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

func wiredMachine(t *testing.T, l *lifecycle, updates update.Config) (*machine, context.Context) {
	t.Helper()
	m, err := newMachine(l.env, l.env.DataDir, loopbackHost, slog.New(slog.NewTextHandler(l.logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, quit := context.WithCancel(context.WithoutCancel(t.Context()))
	var bg background
	m.wire(ctx, quit, &bg, updates)
	if err := m.stack.start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		quit()
		if err := m.stack.close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
		bg.Wait()
		m.hub.Close()
	})
	return m, ctx
}

func TestApplyUpdateRollsBackWhenTheNewVersionNeverAnswers(t *testing.T) {
	t.Parallel()
	l := newLifecycle(t)
	srv, key := releaseServer(t, "1.0.1", []byte("MZ new build"))
	exe := filepath.Join(t.TempDir(), "Ferry.exe")
	if err := os.WriteFile(exe, []byte("MZ old build"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, ctx := wiredMachine(t, l, update.Config{
		Current:     "1.0.0",
		ManifestURL: srv.URL + releasePrefix + "/latest/download/manifest.json",
		PublicKey:   key,
		DataDir:     l.env.DataDir,
		ExePath:     exe,
		Client:      srv.Client(),
		Launch:      launchExitingChild,
		Dev:         true,
	})
	if err := m.applyUpdate(ctx); !errors.Is(err, update.ErrNotReady) {
		t.Fatalf("apply without a staged update: %v", err)
	}
	if status := m.updater.Check(ctx); status.State != update.StateReady {
		t.Fatalf("check ended %+v", status)
	}
	if err := m.applyUpdate(ctx); err == nil {
		t.Fatal("apply succeeded although the new version never answered")
	}
	l.waitHealthy()
	if ctx.Err() != nil {
		t.Fatal("the app quit after a failed update")
	}
	if status := m.updater.Status(); status.State != update.StateFailed {
		t.Fatalf("status %+v after rollback", status)
	}
	expectFile(t, exe, []byte("MZ old build"))
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "Ferry.new.exe")); !os.IsNotExist(err) {
		t.Fatalf("staged binary kept after rollback: %v", err)
	}
}

func TestApplyUpdateRefusesWhileQuitting(t *testing.T) {
	t.Parallel()
	l := newLifecycle(t)
	m, _ := wiredMachine(t, l, update.Config{Current: "dev", DataDir: l.env.DataDir, ExePath: filepath.Join(t.TempDir(), "Ferry.exe")})
	quitting, cancel := context.WithCancel(t.Context())
	cancel()
	if err := m.applyUpdate(quitting); !errors.Is(err, errQuitting) {
		t.Fatalf("got %v, want errQuitting", err)
	}
	if _, isLive := m.stack.receivedDir(); !isLive {
		t.Fatal("stack stopped by a refused apply")
	}
}
