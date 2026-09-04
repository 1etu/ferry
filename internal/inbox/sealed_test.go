package inbox

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/store"
)

const sealedSize = seal.FrameSize + seal.FrameSize/2

func newSealedFixture(t *testing.T, adjust func(cfg *Config)) *fixture {
	t.Helper()
	return newFixture(t, func(cfg *Config) {
		cfg.MaxUploadBytes = 4 * seal.FrameSize
		if adjust != nil {
			adjust(cfg)
		}
	})
}

func (f *fixture) requireReceived(t *testing.T, name string, content []byte) {
	t.Helper()
	stored, err := os.ReadFile(filepath.Join(f.received, name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatalf("%s holds %d bytes that differ from the %d sent", name, len(stored), len(content))
	}
}

func (f *fixture) requireIncomingEmpty(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(f.incoming)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("incoming dir holds %d entries, want none", len(entries))
	}
}

func TestTamperedFrameEndsThePatchFinallyAndKeepsTheVerifiedPrefix(t *testing.T) {
	t.Parallel()
	f := newSealedFixture(t, nil)
	content := randomBytes(t, sealedSize)
	id, target := f.create(t, deviceOne, "tampered.bin", sealedSize)
	frames := f.clients[deviceOne].frames(t, f.nonces[id], 0, content)
	frames[len(frames)-1] ^= 0x01

	resp := f.patchRaw(t, deviceOne, target, 0, frames)
	if resp.status != http.StatusBadRequest || !strings.HasPrefix(resp.body, "ERR_UPLOAD_INTERRUPTED") {
		t.Fatalf("tampered PATCH: %d %q, want 400 ERR_UPLOAD_INTERRUPTED", resp.status, resp.body)
	}
	if head := f.head(t, target); head.header.Get("Upload-Offset") != strconv.Itoa(seal.FrameSize) {
		t.Fatalf("offset after the tampered frame %q, want %d", head.header.Get("Upload-Offset"), seal.FrameSize)
	}
	partial, err := os.ReadFile(filepath.Join(f.incoming, id))
	if err != nil || !bytes.Equal(partial, content[:seal.FrameSize]) {
		t.Fatalf("partial file is not the verified prefix: %d bytes, %v", len(partial), err)
	}
	if tr := f.transfer(t, id); tr.Status != store.TransferActive {
		t.Fatalf("transfer %+v", tr)
	}

	if resp := f.patch(t, deviceOne, target, seal.FrameSize, content[seal.FrameSize:]); resp.status != http.StatusNoContent {
		t.Fatalf("resume with intact frames: %d %q", resp.status, resp.body)
	}
	f.requireReceived(t, "tampered.bin", content)
}

func TestFrameUnderAnotherNonceIsRejected(t *testing.T) {
	t.Parallel()
	f := newSealedFixture(t, nil)
	_, target := f.create(t, deviceOne, "replayed.bin", 10)
	frames := f.clients[deviceOne].frames(t, newNonce(t), 0, []byte("0123456789"))
	if resp := f.patchRaw(t, deviceOne, target, 0, frames); resp.status != http.StatusBadRequest {
		t.Fatalf("frame from another upload: %d %q, want 400", resp.status, resp.body)
	}
}

func TestCreationWithUploadCompletesInOneRequest(t *testing.T) {
	t.Parallel()
	f := newSealedFixture(t, nil)
	content := randomBytes(t, sealedSize)
	resp, id, _ := f.createWithBody(t, deviceOne, "photo.heic", sealedSize, content)
	if resp.status != http.StatusCreated || resp.header.Get("Upload-Offset") != strconv.Itoa(sealedSize) {
		t.Fatalf("creation with upload: %d offset %q body %q", resp.status, resp.header.Get("Upload-Offset"), resp.body)
	}
	f.requireReceived(t, "photo.heic", content)
	if tr := f.transfer(t, id); tr.Status != store.TransferDone || tr.Done != sealedSize {
		t.Fatalf("transfer %+v", tr)
	}
	f.requireIncomingEmpty(t)
}

func TestCreationWithTheFirstChunkContinuesWithPatch(t *testing.T) {
	t.Parallel()
	f := newSealedFixture(t, nil)
	content := randomBytes(t, sealedSize)
	resp, id, target := f.createWithBody(t, deviceOne, "video.mov", sealedSize, content[:seal.FrameSize])
	if resp.status != http.StatusCreated || resp.header.Get("Upload-Offset") != strconv.Itoa(seal.FrameSize) {
		t.Fatalf("creation with the first chunk: %d offset %q", resp.status, resp.header.Get("Upload-Offset"))
	}
	if tr := f.transfer(t, id); tr.Status != store.TransferActive {
		t.Fatalf("partial creation was abandoned: %+v", tr)
	}
	if resp := f.patch(t, deviceOne, target, seal.FrameSize, content[seal.FrameSize:]); resp.status != http.StatusNoContent {
		t.Fatalf("PATCH after creation: %d %q", resp.status, resp.body)
	}
	f.requireReceived(t, "video.mov", content)
}

func TestCreationWithTheFirstChunkIsAnnouncedOnceAnswered(t *testing.T) {
	t.Parallel()
	f := newSealedFixture(t, nil)
	nonce := newNonce(t)
	headers := f.uploadHeaders(t, deviceOne, "video.mov", 3*seal.FrameSize, nonce)
	headers["Content-Type"] = tusContentType
	running := f.startUpload(t, http.MethodPost, "/api/uploads/", headers, 2*seal.FrameSize, nonce)
	running.write(t, randomBytes(t, seal.FrameSize))
	var id string
	waitFor(t, "the transfer row", func() bool {
		transfers, err := f.store.Transfers(t.Context(), deviceOne, 10)
		if err != nil || len(transfers) != 1 {
			return false
		}
		id = transfers[0].ID
		return true
	})
	f.waitForProgress(t, id, seal.FrameSize)
	f.expectNoTransferEvent(t, id, 100*time.Millisecond)

	running.write(t, randomBytes(t, seal.FrameSize))
	if resp := running.response(t); resp.status != http.StatusCreated {
		t.Fatalf("creation with the first chunk: %d %q", resp.status, resp.body)
	}
	if event := f.nextTransfer(t, store.TransferActive); event.ID != id || event.Done != 2*seal.FrameSize {
		t.Fatalf("announced %+v, want %s at %d", event, id, 2*seal.FrameSize)
	}
}

func TestCutCreationWithUploadRemovesTheTransferAndLeavesNothingIncoming(t *testing.T) {
	t.Parallel()
	f := newSealedFixture(t, nil)
	content := randomBytes(t, sealedSize)
	nonce := newNonce(t)
	headers := f.uploadHeaders(t, deviceOne, "cut.bin", sealedSize, nonce)
	frames := f.clients[deviceOne].frames(t, nonce, 0, content)

	status := f.cut(t, http.MethodPost, "/api/uploads/", headers, int64(len(frames)), frames[:seal.FrameSize/2])
	if status != http.StatusBadRequest {
		t.Fatalf("cut creation answered %d, want 400", status)
	}
	transfers, err := f.store.Transfers(t.Context(), deviceOne, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(transfers) != 0 {
		t.Fatalf("transfers %+v, want none", transfers)
	}
	f.expectNoTransferEvent(t, "", 100*time.Millisecond)
	f.requireIncomingEmpty(t)
}

func TestCreationBodyHoldsAWriteSlot(t *testing.T) {
	t.Parallel()
	f := newSealedFixture(t, func(cfg *Config) { cfg.MaxActivePerDevice = 1 })
	_, second := f.create(t, deviceOne, "second.bin", 10)
	nonce := newNonce(t)
	headers := f.uploadHeaders(t, deviceOne, "first.bin", 100, nonce)
	headers["Content-Type"] = tusContentType
	running := f.startUpload(t, http.MethodPost, "/api/uploads/", headers, 100, nonce)
	waitFor(t, "creation body to hold a write slot", func() bool { return f.in.writesOf(deviceOne) == 1 })

	create := f.send(t, f.request(t, http.MethodPost, "/api/uploads/", deviceOne, http.NoBody, createHeaders("third.bin", 10)))
	if create.status != http.StatusTooManyRequests || errorCodeOf(t, create) != api.CodeRateLimited {
		t.Fatalf("creation while a creation body is written: %d %q", create.status, create.body)
	}
	if resp := f.patch(t, deviceOne, second, 0, []byte("0123456789")); resp.status != http.StatusTooManyRequests {
		t.Fatalf("PATCH while a creation body is written: %d", resp.status)
	}
	running.write(t, randomBytes(t, 100))
	if resp := running.response(t); resp.status != http.StatusCreated {
		t.Fatalf("creation with body: %d %q", resp.status, resp.body)
	}
	waitFor(t, "write slot release", func() bool { return f.in.writesOf(deviceOne) == 0 })
}

func TestSealedRequestsWithoutASessionAreSealExpired(t *testing.T) {
	t.Parallel()
	f := newSealedFixture(t, nil)
	_, target := f.create(t, deviceOne, "mine.bin", 10)
	tests := []struct {
		method, target string
		headers        map[string]string
	}{
		{http.MethodPatch, target, map[string]string{"Upload-Offset": "0", "Content-Type": tusContentType}},
		{http.MethodPost, "/api/uploads/", map[string]string{"Upload-Length": "10", "Content-Type": tusContentType}},
	}
	for _, tc := range tests {
		req := f.request(t, tc.method, tc.target, deviceOne, strings.NewReader("ciphertext"), tc.headers)
		req.Header.Del(sealHeader)
		resp := f.send(t, req)
		if resp.status != http.StatusForbidden || errorCodeOf(t, resp) != api.CodeSealExpired {
			t.Fatalf("%s without a session: %d %q, want 403 seal_expired", tc.method, resp.status, resp.body)
		}
	}
}

func TestMethodOverrideCannotTurnAPostIntoAPlainPatch(t *testing.T) {
	t.Parallel()
	f := newSealedFixture(t, nil)
	id, target := f.create(t, deviceOne, "override.bin", 10)
	headers := map[string]string{methodOverrideHeader: http.MethodPatch, "Upload-Offset": "0", "Content-Type": tusContentType}
	resp := f.send(t, f.request(t, http.MethodPost, target, deviceOne, strings.NewReader("0123456789"), headers))
	if resp.status == http.StatusNoContent {
		t.Fatal("a POST with a method override appended a plain body")
	}
	if done, ok := f.in.Progress(id); !ok || done != 0 {
		t.Fatalf("progress %d, %v after the override attempt, want 0 and still live", done, ok)
	}
}
