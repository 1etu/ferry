package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/update"
)

const (
	stagedVersion = "9.9.9"
	releasePrefix = "/releases"
)

func releaseServer(t *testing.T) (*httptest.Server, ed25519.PublicKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	asset := []byte("MZ new build")
	sum := sha256.Sum256(asset)
	manifest, err := update.MarshalManifest(update.Manifest{
		Version: stagedVersion,
		Assets: map[string]update.Asset{
			runtime.GOOS + "-" + runtime.GOARCH: {Name: "Ferry.exe", Size: int64(len(asset)), SHA256: hex.EncodeToString(sum[:])},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(private, manifest))
	bodies := map[string][]byte{
		releasePrefix + "/latest/download/manifest.json":             manifest,
		releasePrefix + "/latest/download/manifest.sig":              []byte(signature),
		releasePrefix + "/download/v" + stagedVersion + "/Ferry.exe": asset,
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

func readyUpdater(t *testing.T) func(*events.Hub, *slog.Logger) *update.Updater {
	t.Helper()
	srv, key := releaseServer(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "Ferry.exe")
	if err := os.WriteFile(exe, []byte("MZ old build"), 0o600); err != nil {
		t.Fatal(err)
	}
	return func(hub *events.Hub, log *slog.Logger) *update.Updater {
		return update.New(update.Config{
			Current:     "1.0.0",
			ManifestURL: srv.URL + releasePrefix + "/latest/download/manifest.json",
			PublicKey:   key,
			DataDir:     dir,
			ExePath:     exe,
			Client:      srv.Client(),
			Dev:         true,
		}, hub, log)
	}
}

func TestUpdateRoutesWithoutAStagedUpdate(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	status := decode[api.UpdateStatus](t, f.owner().send(http.MethodGet, "/api/update", nil))
	if status.Current != "dev" || status.State != string(update.StateDisabled) || status.CheckedAt != "" {
		t.Fatalf("status %+v", status)
	}
	resp := f.owner().send(http.MethodPost, "/api/update/check", nil)
	expectStatus(t, resp, http.StatusAccepted)
	if decode[api.UpdateStatus](t, resp).State != string(update.StateDisabled) {
		t.Fatalf("check answered %s", resp.body)
	}
	expectError(t, f.owner().send(http.MethodPost, "/api/update/apply", nil), http.StatusConflict, api.CodeConflict)
}

func TestCheckStagesAnUpdateThatApplyHandsToTheApp(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{updater: readyUpdater(t)})
	stream, unsubscribe := f.hub.Subscribe(events.Scope{Owner: true})
	t.Cleanup(unsubscribe)
	expectStatus(t, f.owner().send(http.MethodPost, "/api/update/check", nil), http.StatusAccepted)
	waitFor(t, "a staged update", func() bool { return f.updater.Status().State == update.StateReady })
	status := decode[api.UpdateStatus](t, f.owner().send(http.MethodGet, "/api/update", nil))
	checkedAt, err := time.Parse(time.RFC3339, status.CheckedAt)
	if status.Available != stagedVersion || err != nil || checkedAt.IsZero() {
		t.Fatalf("status %+v, checkedAt %v", status, err)
	}
	expectOwnerOnlyUpdateEvent(t, stream)

	expectStatus(t, f.owner().send(http.MethodPost, "/api/update/apply", nil), http.StatusAccepted)
	select {
	case <-f.hooks.updatesApplied:
	case <-time.After(waitTimeout):
		t.Fatal("apply hook not called")
	}
}

func expectOwnerOnlyUpdateEvent(t *testing.T, stream <-chan events.Event) {
	t.Helper()
	timeout := time.After(waitTimeout)
	for {
		select {
		case e := <-stream:
			if e.Kind == events.KindUpdate && e.OwnerOnly {
				return
			}
		case <-timeout:
			t.Fatal("no owner-only update event")
		}
	}
}
