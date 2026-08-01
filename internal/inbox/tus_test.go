package inbox

import (
	"bytes"
	"crypto/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/store"
)

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSealedUploadResumesFromTheFrameBoundaryAfterACutPatch(t *testing.T) {
	t.Parallel()
	const (
		size = 2*seal.FrameSize + seal.FrameSize/2
		cut  = seal.FrameSize
	)
	f := newFixture(t, func(cfg *Config) { cfg.MaxUploadBytes = size })
	content := randomBytes(t, size)
	id, target := f.create(t, deviceOne, "movie.mov", size)
	frames := f.clients[deviceOne].frames(t, f.nonces[id], 0, content)
	insideSecondFrame := cut + frameOverhead + seal.FrameSize/3

	status := f.cut(t, http.MethodPatch, target, map[string]string{"Upload-Offset": "0"}, int64(len(frames)), frames[:insideSecondFrame])
	if status != http.StatusBadRequest {
		t.Fatalf("cut PATCH answered %d, want 400", status)
	}
	head := f.head(t, target)
	if head.status != http.StatusOK || head.header.Get("Upload-Offset") != strconv.Itoa(cut) {
		t.Fatalf("HEAD after cut: %d offset %q, want %d", head.status, head.header.Get("Upload-Offset"), cut)
	}
	if head.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("HEAD cache control %q", head.header.Get("Cache-Control"))
	}

	rest := f.patch(t, deviceOne, target, cut, content[cut:])
	if rest.status != http.StatusNoContent || rest.header.Get("Upload-Offset") != strconv.Itoa(size) {
		t.Fatalf("final PATCH: %d offset %q body %q", rest.status, rest.header.Get("Upload-Offset"), rest.body)
	}

	stored, err := os.ReadFile(filepath.Join(f.received, "movie.mov"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatal("received file differs from the uploaded bytes")
	}
	if f.hasIncoming(id) {
		t.Fatal("incoming files remain after completion")
	}
	tr := f.transfer(t, id)
	if tr.Status != store.TransferDone || tr.Done != size || tr.Name != "movie.mov" || tr.Path != filepath.Join(f.received, "movie.mov") {
		t.Fatalf("transfer %+v", tr)
	}
	if event := f.nextTransfer(t, store.TransferDone); event.ID != id || event.Done != size {
		t.Fatalf("event %+v", event)
	}
	if _, ok := f.in.Progress(id); ok {
		t.Fatal("completed upload still tracked")
	}
	f.expectNoTransferEvent(t, id, 100*time.Millisecond)
	after := f.head(t, target)
	if after.status != http.StatusOK || after.header.Get("Upload-Offset") != strconv.Itoa(size) {
		t.Fatalf("HEAD after completion: %d offset %q", after.status, after.header.Get("Upload-Offset"))
	}
}

func TestCompletionUsesCollisionSuffix(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	if err := os.WriteFile(filepath.Join(f.received, "note.txt"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, target := f.create(t, deviceOne, "note.txt", 6)
	if resp := f.patch(t, deviceOne, target, 0, []byte("second")); resp.status != http.StatusNoContent {
		t.Fatalf("patch: %d %q", resp.status, resp.body)
	}
	stored, err := os.ReadFile(filepath.Join(f.received, "note (2).txt"))
	if err != nil || string(stored) != "second" {
		t.Fatalf("note (2).txt = %q, %v", stored, err)
	}
	if tr := f.transfer(t, id); tr.Name != "note (2).txt" {
		t.Fatalf("transfer name %q", tr.Name)
	}
}

func TestZeroLengthUploadCompletesOnCreate(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	id, _ := f.create(t, deviceOne, "empty.txt", 0)
	info, err := os.Stat(filepath.Join(f.received, "empty.txt"))
	if err != nil || info.Size() != 0 {
		t.Fatalf("empty.txt: %v", err)
	}
	if tr := f.transfer(t, id); tr.Status != store.TransferDone {
		t.Fatalf("transfer %+v", tr)
	}
	if f.hasIncoming(id) {
		t.Fatal("incoming files remain")
	}
}

func TestConcurrentWritesPerDeviceAreCapped(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(cfg *Config) { cfg.MaxActivePerDevice = 1 })
	_, first := f.create(t, deviceOne, "first.bin", 100)
	_, second := f.create(t, deviceOne, "second.bin", 10)
	_, other := f.create(t, deviceTwo, "other.bin", 10)
	running := f.startPatch(t, first, 100)
	waitFor(t, "first PATCH to hold a write slot", func() bool { return f.in.writesOf(deviceOne) == 1 })

	create := f.send(t, f.request(t, http.MethodPost, "/api/uploads/", deviceOne, http.NoBody, createHeaders("third.bin", 10)))
	if create.status != http.StatusTooManyRequests || create.header.Get("Retry-After") != retryAfterSeconds {
		t.Fatalf("create while writing: %d retry-after %q", create.status, create.header.Get("Retry-After"))
	}
	if got := errorCodeOf(t, create); got != api.CodeRateLimited {
		t.Fatalf("create code %q", got)
	}
	if resp := f.patch(t, deviceOne, second, 0, []byte("0123456789")); resp.status != http.StatusTooManyRequests {
		t.Fatalf("second PATCH while writing: %d", resp.status)
	}
	if resp := f.patch(t, deviceTwo, other, 0, []byte("0123456789")); resp.status != http.StatusNoContent {
		t.Fatalf("other device's PATCH: %d %q", resp.status, resp.body)
	}

	running.finish(t)
	waitFor(t, "write slot release", func() bool { return f.in.writesOf(deviceOne) == 0 })
	if resp := f.patch(t, deviceOne, second, 0, []byte("0123456789")); resp.status != http.StatusNoContent {
		t.Fatalf("second PATCH after release: %d %q", resp.status, resp.body)
	}
}

func TestProgressIsPublishedAndSavedOnClose(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	id, target := f.create(t, deviceOne, "slow.bin", 1000)
	running := f.startPatch(t, target, 1000)
	running.write(t, randomBytes(t, 300))
	f.waitForProgress(t, id, 300)

	f.nextTransfer(t, store.TransferActive)
	progress := f.nextTransfer(t, store.TransferActive)
	if progress.ID != id || progress.Done != 300 {
		t.Fatalf("progress event %+v", progress)
	}
	running.finish(t)

	f.closeInbox(t)
	if tr := f.transfer(t, id); tr.Status != store.TransferActive || tr.Done != 300 {
		t.Fatalf("transfer after Close %+v", tr)
	}
}

func TestShutdownCauseEndsRunningPatchAndKeepsBytes(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	id, target := f.create(t, deviceOne, "shutdown.bin", 1000)
	running := f.startPatch(t, target, 1000)
	running.write(t, randomBytes(t, 200))
	f.waitForProgress(t, id, 200)

	f.cancelBase(ErrShutdown)
	if resp := running.response(t); resp.status != http.StatusServiceUnavailable {
		t.Fatalf("PATCH during shutdown: %d %q", resp.status, resp.body)
	}
	info, err := os.Stat(filepath.Join(f.incoming, id))
	if err != nil {
		t.Fatalf("partial file: %v", err)
	}
	if info.Size() != 200 {
		t.Fatalf("partial file holds %d bytes, want 200", info.Size())
	}
	if _, ok := f.in.Progress(id); !ok {
		t.Fatal("interrupted upload no longer tracked")
	}
	if tr := f.transfer(t, id); tr.Status != store.TransferActive {
		t.Fatalf("transfer %+v", tr)
	}
}
