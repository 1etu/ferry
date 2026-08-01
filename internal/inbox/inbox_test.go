package inbox

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tus/tusd/v2/pkg/handler"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

func TestNewRequiresIncomingInsideReceived(t *testing.T) {
	t.Parallel()
	received := t.TempDir()
	cfg := Config{ReceivedDir: received, IncomingDir: filepath.Join(t.TempDir(), "elsewhere")}
	if _, err := New(cfg, nil, nil, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("incoming dir outside received dir accepted")
	}
	cfg.IncomingDir = received
	if _, err := New(cfg, nil, nil, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("incoming dir equal to received dir accepted")
	}
}

func TestNewCreatesIncomingDir(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	info, err := os.Stat(f.incoming)
	if err != nil || !info.IsDir() {
		t.Fatalf("incoming dir not created: %v", err)
	}
}

func TestCreateRejections(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		device  string
		headers map[string]string
		adjust  func(f *fixture)
		status  int
		code    api.ErrorCode
	}{
		{"no device in context", "", createHeaders("a.jpg", 10), nil, http.StatusInternalServerError, api.CodeInternal},
		{"deferred length", deviceOne, map[string]string{"Upload-Defer-Length": "1", "Upload-Metadata": "filename YQ=="}, nil, http.StatusBadRequest, api.CodeInvalidRequest},
		{"missing name", deviceOne, map[string]string{"Upload-Length": "10"}, nil, http.StatusBadRequest, api.CodeInvalidRequest},
		{"blank name", deviceOne, createHeaders("   ", 10), nil, http.StatusBadRequest, api.CodeInvalidRequest},
		{"no sealed session", deviceUnsealed, createHeaders("a.jpg", 10), nil, http.StatusForbidden, api.CodeSealExpired},
		{"too large", deviceOne, createHeaders("a.jpg", 1<<20+1), nil, http.StatusRequestEntityTooLarge, api.CodeTooLarge},
		{"no space", deviceOne, createHeaders("a.jpg", 1000), func(f *fixture) { f.free.Store(999) }, http.StatusInsufficientStorage, api.CodeNoSpace},
		{"reserve counts against free space", deviceOne, createHeaders("a.jpg", 1000), func(f *fixture) { f.free.Store(1500) }, http.StatusInsufficientStorage, api.CodeNoSpace},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, func(cfg *Config) { cfg.ReserveBytes = 600 })
			if tc.adjust != nil {
				tc.adjust(f)
			}
			f.requireRejectedCreation(t, tc.device, tc.headers, tc.status, tc.code)
		})
	}
}

func TestCreateRejectsMetadataNotSealedForTheSession(t *testing.T) {
	t.Parallel()
	sealedNonce := standard64(base64.RawURLEncoding.EncodeToString(make([]byte, 16)))
	tests := []struct {
		name     string
		metadata func(t *testing.T, f *fixture) string
	}{
		{"plain name", func(*testing.T, *fixture) string { return "name " + standard64("a.jpg") + ",nonce " + sealedNonce }},
		{"name sealed for another device", func(t *testing.T, f *fixture) string {
			t.Helper()
			return f.clients[deviceTwo].metadata(t, "a.jpg", make([]byte, 16))
		}},
		{"missing nonce", func(t *testing.T, f *fixture) string {
			t.Helper()
			name, _, _ := strings.Cut(f.clients[deviceOne].metadata(t, "a.jpg", make([]byte, 16)), ",nonce")
			return name
		}},
		{"short nonce", func(t *testing.T, f *fixture) string {
			t.Helper()
			return f.clients[deviceOne].metadata(t, "a.jpg", make([]byte, 8))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, nil)
			headers := map[string]string{"Upload-Length": "10", "Upload-Metadata": tc.metadata(t, f)}
			f.requireRejectedCreation(t, deviceOne, headers, http.StatusBadRequest, api.CodeInvalidRequest)
		})
	}
}

func (f *fixture) requireRejectedCreation(t *testing.T, device string, headers map[string]string, status int, code api.ErrorCode) {
	t.Helper()
	resp := f.send(t, f.request(t, http.MethodPost, "/api/uploads/", device, http.NoBody, headers))
	if resp.status != status {
		t.Fatalf("status %d body %q, want %d", resp.status, resp.body, status)
	}
	if got := errorCodeOf(t, resp); got != code {
		t.Fatalf("code %q, want %q", got, code)
	}
	transfers, err := f.store.Transfers(t.Context(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(transfers) != 0 {
		t.Fatalf("rejected creation inserted %d transfers", len(transfers))
	}
}

func TestCreateInsertsActiveTransferAndPublishes(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	id, _ := f.create(t, deviceOne, "../photos/IMG:0001.HEIC", 42)
	tr := f.transfer(t, id)
	if tr.Status != store.TransferActive || tr.Name != "IMG0001.HEIC" || tr.Size != 42 || tr.DeviceID != deviceOne || tr.Direction != store.DirectionIn {
		t.Fatalf("transfer %+v", tr)
	}
	event := f.nextTransfer(t, store.TransferActive)
	if event.ID != id || event.Name != "IMG0001.HEIC" {
		t.Fatalf("event %+v", event)
	}
	encoded, err := os.ReadFile(filepath.Join(f.incoming, id+infoSuffix))
	if err != nil {
		t.Fatal(err)
	}
	var info handler.FileInfo
	if err := json.Unmarshal(encoded, &info); err != nil {
		t.Fatal(err)
	}
	if info.MetaData[metadataName] != "IMG0001.HEIC" || uploadNonce(info.MetaData) == nil {
		t.Fatalf("stored metadata %v, want the opened, sanitized name and the nonce", info.MetaData)
	}
}

func TestClaimAppendsCollisionSuffixBeforeTheLastExtension(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	for _, existing := range []string{"photo.jpg", "photo (2).jpg", "archive.tar.gz", "README"} {
		if err := os.WriteFile(filepath.Join(f.received, existing), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct{ name, want string }{
		{"photo.jpg", "photo (3).jpg"},
		{"archive.tar.gz", "archive.tar (2).gz"},
		{"README", "README (2)"},
		{"fresh.txt", "fresh.txt"},
	}
	for i, tc := range tests {
		tr := f.seedActive(t, fmt.Sprintf("claim%d", i), 1)
		tr.Name = tc.name
		got, err := f.in.claim(t.Context(), tr)
		if err != nil {
			t.Fatalf("claim %s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("claim %s = %s, want %s", tc.name, got, tc.want)
		}
		if _, err := os.Stat(filepath.Join(f.received, got)); err != nil {
			t.Fatalf("claimed name %s does not exist: %v", got, err)
		}
		if stored := f.transfer(t, tr.ID); stored.Path != filepath.Join(f.received, got) {
			t.Fatalf("claim of %s recorded as %q", tc.name, stored.Path)
		}
	}
}

func TestCollisionOfLongNameStaysWithinFilenameLimit(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	name := strings.Repeat("a", 250) + ".jpeg"
	if err := os.WriteFile(filepath.Join(f.received, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, target := f.create(t, deviceOne, name, 1)
	if resp := f.patch(t, deviceOne, target, 0, []byte("y")); resp.status != http.StatusNoContent {
		t.Fatalf("patch: %d %q", resp.status, resp.body)
	}
	want := strings.Repeat("a", 246) + " (2).jpeg"
	stored, err := os.ReadFile(filepath.Join(f.received, want))
	if err != nil || string(stored) != "y" {
		t.Fatalf("%s = %q, %v", want, stored, err)
	}
	if tr := f.transfer(t, id); tr.Name != want || len(tr.Name) != maxFilenameBytes {
		t.Fatalf("transfer name %q (%d bytes)", tr.Name, len(tr.Name))
	}
}
