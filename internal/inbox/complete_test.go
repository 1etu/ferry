package inbox

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

func TestFinalPatchRetriesCompletionUntilTheFileIsFree(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("needs a handle without FILE_SHARE_DELETE to make the rename fail")
	}
	f := newFixture(t, nil)
	content := []byte("0123456789")
	id, target := f.create(t, deviceOne, "held.bin", int64(len(content)))
	held, err := os.Open(filepath.Join(f.incoming, id))
	if err != nil {
		t.Fatal(err)
	}
	isHeld := true
	t.Cleanup(func() {
		if isHeld {
			held.Close()
		}
	})

	final := f.patch(t, deviceOne, target, 0, content)
	if final.status != http.StatusLocked || errorCodeOf(t, final) != api.CodeInternal {
		t.Fatalf("final PATCH while the file is held: %d %q", final.status, final.body)
	}
	if _, ok := f.in.Progress(id); !ok {
		t.Fatal("upload no longer tracked after the failed move")
	}
	if tr := f.transfer(t, id); tr.Status != store.TransferActive {
		t.Fatalf("transfer %+v", tr)
	}
	if _, err := os.Stat(filepath.Join(f.received, "held.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("claim file left behind: %v", err)
	}
	if head := f.head(t, target); head.status != http.StatusLocked {
		t.Fatalf("HEAD while the file is held: %d", head.status)
	}

	isHeld = false
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	head := f.head(t, target)
	if head.status != http.StatusOK || head.header.Get("Upload-Offset") != "10" || head.header.Get("Upload-Length") != "10" {
		t.Fatalf("HEAD after release: %d offset %q length %q", head.status, head.header.Get("Upload-Offset"), head.header.Get("Upload-Length"))
	}
	stored, err := os.ReadFile(filepath.Join(f.received, "held.bin"))
	if err != nil || !bytes.Equal(stored, content) {
		t.Fatalf("held.bin = %q, %v", stored, err)
	}
	if tr := f.transfer(t, id); tr.Status != store.TransferDone || tr.Name != "held.bin" || tr.Done != 10 {
		t.Fatalf("transfer %+v", tr)
	}
	if f.hasIncoming(id) {
		t.Fatal("incoming files remain")
	}
	if event := f.nextTransfer(t, store.TransferDone); event.ID != id {
		t.Fatalf("event %+v", event)
	}
	if all, err := f.store.Transfers(t.Context(), deviceOne, 10); err != nil || len(all) != 1 {
		t.Fatalf("transfers = %d, %v; want exactly one", len(all), err)
	}
}

func TestHeadOfFinishedUploadReportsCompletion(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	id, target := f.create(t, deviceOne, "done.bin", 10)
	if resp := f.patch(t, deviceOne, target, 0, []byte("0123456789")); resp.status != http.StatusNoContent {
		t.Fatalf("patch: %d %q", resp.status, resp.body)
	}
	head := f.head(t, target)
	if head.status != http.StatusOK || head.header.Get("Upload-Offset") != "10" || head.header.Get("Upload-Length") != "10" {
		t.Fatalf("HEAD of finished upload: %d offset %q length %q", head.status, head.header.Get("Upload-Offset"), head.header.Get("Upload-Length"))
	}
	if head.header.Get("Tus-Resumable") != tusVersion || head.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("HEAD headers %v", head.header)
	}
	if resp := f.patch(t, deviceOne, target, 10, nil); resp.status != http.StatusNoContent || resp.header.Get("Upload-Offset") != "10" {
		t.Fatalf("PATCH at the end of a finished upload: %d offset %q", resp.status, resp.header.Get("Upload-Offset"))
	}
	other := f.send(t, f.request(t, http.MethodHead, target, deviceTwo, http.NoBody, nil))
	if other.status != http.StatusNotFound {
		t.Fatalf("HEAD by another device: %d", other.status)
	}
	if err := f.store.DeleteFinishedTransfers(t.Context(), deviceOne); err != nil {
		t.Fatal(err)
	}
	if resp := f.head(t, target); resp.status != http.StatusNotFound {
		t.Fatalf("HEAD after the history is cleared: %d", resp.status)
	}
	if id == "" {
		t.Fatal("no upload id")
	}
}

func TestFreeSpaceCountsOutstandingUploads(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(cfg *Config) { cfg.ReserveBytes = 600 })
	f.free.Store(2000)
	_, first := f.create(t, deviceOne, "first.bin", 1000)
	second := f.send(t, f.request(t, http.MethodPost, "/api/uploads/", deviceOne, http.NoBody, createHeaders("second.bin", 1000)))
	if second.status != http.StatusInsufficientStorage || errorCodeOf(t, second) != api.CodeNoSpace {
		t.Fatalf("second creation with the first outstanding: %d %q", second.status, second.body)
	}
	if resp := f.patch(t, deviceOne, first, 0, randomBytes(t, 1000)); resp.status != http.StatusNoContent {
		t.Fatalf("patch: %d %q", resp.status, resp.body)
	}
	f.create(t, deviceOne, "second.bin", 1000)
}

func TestPatchIsRefusedWhenFreeSpaceDrops(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(cfg *Config) { cfg.ReserveBytes = 600 })
	id, target := f.create(t, deviceOne, "paused.bin", 1000)
	f.free.Store(1500)
	resp := f.patch(t, deviceOne, target, 0, []byte("0123456789"))
	if resp.status != http.StatusInsufficientStorage || errorCodeOf(t, resp) != api.CodeNoSpace {
		t.Fatalf("PATCH without space: %d %q", resp.status, resp.body)
	}
	if tr := f.transfer(t, id); tr.Status != store.TransferActive {
		t.Fatalf("transfer %+v", tr)
	}
	if !f.hasIncoming(id) {
		t.Fatal("partial upload removed")
	}
	f.free.Store(1 << 40)
	if resp := f.patch(t, deviceOne, target, 0, randomBytes(t, 1000)); resp.status != http.StatusNoContent {
		t.Fatalf("PATCH after space is freed: %d %q", resp.status, resp.body)
	}
	if tr := f.transfer(t, id); tr.Status != store.TransferDone {
		t.Fatalf("transfer %+v", tr)
	}
}
