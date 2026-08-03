package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

func testContent(size int) []byte {
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i % 251)
	}
	return content
}

func contentRequest(ctx context.Context, fileID, deviceID string, header http.Header) *http.Request {
	if deviceID != "" {
		ctx = withDevice(ctx, deviceID)
	}
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/files/"+fileID+"/content", http.NoBody)
	for key, values := range header {
		r.Header[key] = values
	}
	return r
}

func (fx fixture) serve(t *testing.T, fileID, deviceID string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	fx.outbox.Serve(w, contentRequest(t.Context(), fileID, deviceID, header), fileID)
	return w
}

func (fx fixture) transfers(t *testing.T) []store.Transfer {
	t.Helper()
	all, err := fx.store.Transfers(t.Context(), "", 100)
	if err != nil {
		t.Fatalf("list transfers: %v", err)
	}
	return all
}

func (fx fixture) onlyTransfer(t *testing.T) store.Transfer {
	t.Helper()
	all := fx.transfers(t)
	if len(all) != 1 {
		t.Fatalf("got %d transfers, want 1", len(all))
	}
	return all[0]
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) api.ErrorCode {
	t.Helper()
	var body api.Error
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", w.Body.String(), err)
	}
	return body.Error.Code
}

func TestServeWholeFile(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	content := testContent(1000)
	f := fx.offer(t, "report.pdf", content)

	w := fx.serve(t, f.ID, testDeviceID, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), content) {
		t.Fatalf("got %d body bytes, want the file's 1000", w.Body.Len())
	}
	wantHeaders := map[string]string{
		"Content-Disposition":    `attachment; filename="report.pdf"; filename*=UTF-8''report.pdf`,
		"Content-Type":           "application/pdf",
		"Content-Length":         "1000",
		"Accept-Ranges":          "bytes",
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "private, no-store",
	}
	for key, want := range wantHeaders {
		if got := w.Header().Get(key); got != want {
			t.Errorf("got %s %q, want %q", key, got, want)
		}
	}
	for _, key := range []string{"Last-Modified", "Etag"} {
		if w.Header().Get(key) == "" {
			t.Errorf("got no %s header", key)
		}
	}
}

func TestServeOwnerCreatesNoTransfer(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	content := testContent(300)
	f := fx.offer(t, "a.bin", content)

	w := fx.serve(t, f.ID, "", nil)

	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), content) {
		t.Fatalf("got status %d with %d bytes, want 200 with the file", w.Code, w.Body.Len())
	}
	if got := fx.transfers(t); len(got) != 0 {
		t.Fatalf("got %d transfers, want 0", len(got))
	}
}

func TestServeHeadCreatesNoTransfer(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	f := fx.offer(t, "a.bin", testContent(300))
	r := contentRequest(t.Context(), f.ID, testDeviceID, nil)
	r.Method = http.MethodHead
	w := httptest.NewRecorder()

	fx.outbox.Serve(w, r, f.ID)

	if w.Code != http.StatusOK || w.Header().Get("Content-Length") != "300" {
		t.Fatalf("got status %d length %q, want 200 and 300", w.Code, w.Header().Get("Content-Length"))
	}
	if got := fx.transfers(t); len(got) != 0 {
		t.Fatalf("got %d transfers, want 0", len(got))
	}
}

func TestServeUnknownFileIsNotFound(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	w := fx.serve(t, "01J9ZK0X5S8V7Q2M3N4P5R6T7X", testDeviceID, nil)
	if w.Code != http.StatusNotFound || errorCode(t, w) != api.CodeNotFound {
		t.Fatalf("got %d %s, want 404 not_found", w.Code, w.Body.String())
	}
}

func TestServeMissingFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		deviceID      string
		wantTransfers int
	}{
		{name: "device request fails its transfer", deviceID: testDeviceID, wantTransfers: 1},
		{name: "owner request creates no transfer", deviceID: "", wantTransfers: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newFixture(t)
			f := fx.offer(t, "moved.txt", []byte("gone soon"))
			if err := os.Remove(f.Path); err != nil {
				t.Fatalf("remove offered file: %v", err)
			}

			w := fx.serve(t, f.ID, tt.deviceID, nil)

			if w.Code != http.StatusGone || errorCode(t, w) != api.CodeFileMissing {
				t.Fatalf("got %d %s, want 410 file_missing", w.Code, w.Body.String())
			}
			got := fx.transfers(t)
			if len(got) != tt.wantTransfers {
				t.Fatalf("got %d transfers, want %d", len(got), tt.wantTransfers)
			}
			if tt.wantTransfers == 1 && (got[0].Status != store.TransferFailed || got[0].Error != string(api.CodeFileMissing)) {
				t.Fatalf("got %s/%q, want failed/file_missing", got[0].Status, got[0].Error)
			}
		})
	}
}

func TestServeRefreshesChangedFile(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	f := fx.offer(t, "log.txt", []byte("short"))
	grown := []byte("much longer now")
	if err := os.WriteFile(f.Path, grown, 0o600); err != nil {
		t.Fatalf("rewrite offered file: %v", err)
	}

	w := fx.serve(t, f.ID, testDeviceID, nil)

	if !bytes.Equal(w.Body.Bytes(), grown) {
		t.Fatalf("got body %q, want %q", w.Body.String(), grown)
	}
	stored, err := fx.store.File(t.Context(), f.ID)
	if err != nil {
		t.Fatalf("load file: %v", err)
	}
	if stored.Size != int64(len(grown)) {
		t.Fatalf("got stored size %d, want %d", stored.Size, len(grown))
	}
	if got := fx.onlyTransfer(t); got.Size != int64(len(grown)) || got.Status != store.TransferDone {
		t.Fatalf("got %+v, want done with the new size", got)
	}
}
