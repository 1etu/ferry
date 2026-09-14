package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/events"
)

const (
	manifestPath  = "/releases/latest/download/manifest.json"
	signaturePath = "/releases/latest/download/manifest.sig"
	assetName     = "Ferry.exe"
)

type requestLog struct {
	path        string
	ifNoneMatch string
}

type release struct {
	t         *testing.T
	public    ed25519.PublicKey
	private   ed25519.PrivateKey
	mu        sync.Mutex
	version   string
	asset     []byte
	etag      string
	size      int64
	sha256    string
	tamper    bool
	assetCode int
	requests  []requestLog
}

func newRelease(t *testing.T, version string, asset []byte) *release {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &release{t: t, public: public, private: private, version: version, asset: asset, etag: `"` + version + `"`}
}

func (r *release) update(mutate func(*release)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mutate(r)
}

func (r *release) signedManifest() ([]byte, string) {
	sum := sha256.Sum256(r.asset)
	a := Asset{Name: assetName, Size: int64(len(r.asset)), SHA256: hex.EncodeToString(sum[:])}
	if r.size != 0 {
		a.Size = r.size
	}
	if r.sha256 != "" {
		a.SHA256 = r.sha256
	}
	body, err := MarshalManifest(Manifest{Version: r.version, Assets: map[string]Asset{platformKey(): a}})
	if err != nil {
		r.t.Fatal(err)
	}
	sig := ed25519.Sign(r.private, body)
	if r.tamper {
		sig[0] ^= 1
	}
	return body, base64.StdEncoding.EncodeToString(sig)
}

func (r *release) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, requestLog{req.URL.Path, req.Header.Get("If-None-Match")})
	body, sig := r.signedManifest()
	switch req.URL.Path {
	case manifestPath:
		if req.Header.Get("If-None-Match") == r.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", r.etag)
		r.write(w, body)
	case signaturePath:
		r.write(w, []byte(sig))
	case "/releases/download/v" + r.version + "/" + assetName:
		if r.assetCode != 0 {
			w.WriteHeader(r.assetCode)
			return
		}
		r.write(w, r.asset)
	default:
		http.NotFound(w, req)
	}
}

func (r *release) write(w http.ResponseWriter, body []byte) {
	if _, err := w.Write(body); err != nil {
		r.t.Error(err)
	}
}

func (r *release) count(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, req := range r.requests {
		if req.path == path {
			n++
		}
	}
	return n
}

func (r *release) assetRequests() int {
	r.mu.Lock()
	version := r.version
	r.mu.Unlock()
	return r.count("/releases/download/v" + version + "/" + assetName)
}

func (r *release) lastManifestRequest() requestLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	var last requestLog
	for _, req := range r.requests {
		if req.path == manifestPath {
			last = req
		}
	}
	return last
}

type fixture struct {
	t      *testing.T
	u      *Updater
	r      *release
	srv    *httptest.Server
	exe    string
	stream <-chan events.Event
}

func fixedNow() time.Time {
	return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newFixture(t *testing.T, current string, r *release) *fixture {
	t.Helper()
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	exe := filepath.Join(dir, assetName)
	writeFile(t, exe, "old")
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub(discardLogger())
	stream, cancel := hub.Subscribe(events.Scope{Owner: true})
	t.Cleanup(cancel)
	u := New(Config{
		Current:     current,
		ManifestURL: srv.URL + manifestPath,
		PublicKey:   r.public,
		DataDir:     dataDir,
		ExePath:     exe,
		Client:      srv.Client(),
		Now:         fixedNow,
		Dev:         true,
	}, hub, discardLogger())
	return &fixture{t: t, u: u, r: r, srv: srv, exe: exe, stream: stream}
}

func (f *fixture) states() []State {
	f.t.Helper()
	var states []State
	for {
		select {
		case e := <-f.stream:
			st, ok := e.Payload.(Status)
			if !ok {
				f.t.Fatalf("payload %T is not a Status", e.Payload)
			}
			states = append(states, st.State)
		default:
			return states
		}
	}
}

func (f *fixture) cache(t *testing.T) cache {
	t.Helper()
	c, err := loadCache(f.u.cachePath)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return false
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 5s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func healthServer(t *testing.T, version string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"app": "ferry", "name": "pc", "version": version}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
