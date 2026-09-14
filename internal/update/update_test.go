package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/1etu/ferry/internal/events"
)

func TestCheckStagesNewerVersion(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("new binary"))
	f := newFixture(t, "1.0.0", r)

	st := f.u.Check(t.Context())

	if st.State != StateReady || st.Available != "1.0.1" || st.Error != "" {
		t.Fatalf("got %+v, want ready 1.0.1", st)
	}
	if !st.CheckedAt.Equal(fixedNow()) {
		t.Fatalf("checkedAt %v, want %v", st.CheckedAt, fixedNow())
	}
	if got := readFile(t, f.u.newExePath()); got != "new binary" {
		t.Fatalf("staged %q", got)
	}
	if c := f.cache(t); c.ETag != `"1.0.1"` || c.Version != "1.0.1" || c.Failed != "" {
		t.Fatalf("cache %+v", c)
	}
	want := []State{StateChecking, StateDownloading, StateReady}
	if got := f.states(); !slices.Equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
}

func TestCheckSendsETagOnlyWhenNothingIsPending(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("new binary"))
	f := newFixture(t, "1.0.0", r)
	f.u.Check(t.Context())
	if got := f.r.lastManifestRequest().ifNoneMatch; got != "" {
		t.Fatalf("first check sent If-None-Match %q", got)
	}

	st := f.u.Check(t.Context())

	if st.State != StateReady || st.Available != "1.0.1" {
		t.Fatalf("after 304 got %+v, want ready", st)
	}
	if got := f.r.lastManifestRequest().ifNoneMatch; got != `"1.0.1"` {
		t.Fatalf("second check sent If-None-Match %q", got)
	}
	if n := f.r.assetRequests(); n != 1 {
		t.Fatalf("asset downloaded %d times", n)
	}
}

func TestCheckReportsUpToDateAndCachesIt(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("same"))
	f := newFixture(t, "1.0.1", r)
	for range 2 {
		if st := f.u.Check(t.Context()); st.State != StateIdle || st.Available != "" {
			t.Fatalf("got %+v, want idle", st)
		}
	}
	if n := f.r.count(signaturePath); n != 1 {
		t.Fatalf("signature fetched %d times, want 1", n)
	}
	if n := f.r.assetRequests(); n != 0 {
		t.Fatalf("asset downloaded %d times", n)
	}
}

func TestCheckReplacesStagedVersionWhenManifestChanges(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("new"))
	f := newFixture(t, "1.0.0", r)
	f.u.Check(t.Context())
	r.update(func(r *release) { r.version, r.etag, r.asset = "1.0.2", `"2"`, []byte("newer") })

	st := f.u.Check(t.Context())

	if st.State != StateReady || st.Available != "1.0.2" {
		t.Fatalf("got %+v", st)
	}
	if got := readFile(t, f.u.newExePath()); got != "newer" {
		t.Fatalf("staged %q", got)
	}
}

func TestCheckRejectsBadSignature(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("new"))
	r.tamper = true
	f := newFixture(t, "1.0.0", r)

	st := f.u.Check(t.Context())

	if st.State != StateFailed || !strings.Contains(st.Error, ErrSignature.Error()) {
		t.Fatalf("got %+v", st)
	}
	if exists(t, f.u.cachePath) {
		t.Fatal("cache written for an unverified manifest")
	}
	if n := f.r.assetRequests(); n != 0 {
		t.Fatalf("asset downloaded %d times", n)
	}
}

func TestCheckRefusesDowngrade(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "0.9.0", []byte("older"))
	f := newFixture(t, "1.0.0", r)

	st := f.u.Check(t.Context())

	if st.State != StateIdle || st.Available != "" {
		t.Fatalf("got %+v, want idle", st)
	}
	if n := f.r.assetRequests(); n != 0 || exists(t, f.u.newExePath()) {
		t.Fatal("downgrade was downloaded")
	}
}

func TestCheckSkipsVersionThatFailedToInstall(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("new"))
	f := newFixture(t, "1.0.0", r)
	if err := saveCache(f.u.cachePath, cache{Failed: "1.0.1"}); err != nil {
		t.Fatal(err)
	}

	st := f.u.Check(t.Context())

	if st.State != StateIdle || st.Available != "" {
		t.Fatalf("got %+v, want idle", st)
	}
	if n := f.r.assetRequests(); n != 0 {
		t.Fatalf("asset downloaded %d times", n)
	}
	if c := f.cache(t); c.Failed != "1.0.1" || c.Version != "1.0.1" {
		t.Fatalf("cache %+v", c)
	}
}

func TestCheckRefusesReplayOfOlderManifest(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("new"))
	f := newFixture(t, "1.0.0", r)
	if err := saveCache(f.u.cachePath, cache{Version: "1.2.0"}); err != nil {
		t.Fatal(err)
	}

	st := f.u.Check(t.Context())

	if st.State != StateFailed || !strings.Contains(st.Error, ErrReplay.Error()) {
		t.Fatalf("got %+v", st)
	}
	if c := f.cache(t); c.Version != "1.2.0" {
		t.Fatalf("cache overwritten: %+v", c)
	}
	if n := f.r.assetRequests(); n != 0 {
		t.Fatalf("asset downloaded %d times", n)
	}
}

func TestCheckReportsMissingManifest(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("new"))
	f := newFixture(t, "1.0.0", r)
	f.u.manifestURL.Path = "/missing/latest/download/manifest.json"

	st := f.u.Check(t.Context())

	if st.State != StateFailed || !strings.Contains(st.Error, "status 404") {
		t.Fatalf("got %+v", st)
	}
}

func TestNewDisablesWithoutTrust(t *testing.T) {
	t.Parallel()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		cfg  Config
	}{
		{"dev build", Config{Current: "dev", PublicKey: public}},
		{"placeholder key", Config{Current: "1.0.0", PublicKey: make(ed25519.PublicKey, ed25519.PublicKeySize)}},
		{"plain http outside dev", Config{Current: "1.0.0", PublicKey: public, ManifestURL: "http://example.invalid" + manifestPath}},
		{"unexpected manifest path", Config{Current: "1.0.0", PublicKey: public, ManifestURL: "https://example.invalid/manifest.json"}},
		{"unparsable current version", Config{Current: "nightly", PublicKey: public}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.cfg.DataDir = t.TempDir()
			tc.cfg.ExePath = filepath.Join(tc.cfg.DataDir, assetName)
			u := New(tc.cfg, events.NewHub(discardLogger()), discardLogger())
			if st := u.Check(t.Context()); st.State != StateDisabled || st.Current != tc.cfg.Current {
				t.Fatalf("got %+v, want disabled", st)
			}
			if err := u.Stage(t.Context()); !errors.Is(err, ErrDisabled) {
				t.Fatalf("Stage: %v", err)
			}
			if err := u.Apply(t.Context(), "http://127.0.0.1:1/api/health"); !errors.Is(err, ErrDisabled) {
				t.Fatalf("Apply: %v", err)
			}
		})
	}
}

func TestDevOverridesApplyOnlyInDevMode(t *testing.T) {
	r := newRelease(t, "1.0.1", []byte("new"))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	t.Setenv(envManifestURL, srv.URL+manifestPath)
	t.Setenv(envPublicKey, base64.StdEncoding.EncodeToString(r.public))
	dir := t.TempDir()
	cfg := Config{Current: "1.0.0", DataDir: dir, ExePath: filepath.Join(dir, assetName), Client: &http.Client{Transport: offlineTransport{}}}
	hub := events.NewHub(discardLogger())

	if st := New(cfg, hub, discardLogger()).Check(t.Context()); st.State == StateReady {
		t.Fatalf("release build honored dev overrides: %+v", st)
	}
	cfg.Client = srv.Client()
	if n := r.count(manifestPath); n != 0 {
		t.Fatalf("release build made %d requests", n)
	}
	cfg.Dev = true
	if st := New(cfg, hub, discardLogger()).Check(t.Context()); st.State != StateReady {
		t.Fatalf("dev build ignored overrides: %+v", st)
	}
}

type offlineTransport struct{}

func (offlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
}
