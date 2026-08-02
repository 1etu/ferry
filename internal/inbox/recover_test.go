package inbox

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

func TestRecoverRestoresOffsetsAndFailsLostUploads(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	f.seedActive(t, "kept", 100)
	f.seedActive(t, "lost", 100)
	f.writeIncoming(t, "kept", []byte("12345"))
	if err := f.in.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if done, ok := f.in.Progress("kept"); !ok || done != 5 {
		t.Fatalf("kept progress = %d, %v; want 5, true", done, ok)
	}
	if kept := f.transfer(t, "kept"); kept.Status != store.TransferActive || kept.Done != 5 {
		t.Fatalf("kept transfer %+v", kept)
	}
	if _, ok := f.in.Progress("lost"); ok {
		t.Fatal("lost upload is tracked")
	}
	if lost := f.transfer(t, "lost"); lost.Status != store.TransferFailed || lost.Error != string(api.CodeExpired) {
		t.Fatalf("lost transfer %+v", lost)
	}
}

func TestRecoverFinishesCompleteUploadsAndKeepsDeliveredFiles(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	content := []byte("12345")

	delivered := f.seedActive(t, "delivered", 5)
	delivered.Path = filepath.Join(f.received, "delivered.bin")
	f.update(t, delivered)
	if err := os.WriteFile(delivered.Path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	f.writeInfo(t, "delivered")

	claimed := f.seedActive(t, "claimed", 5)
	claimed.Path = filepath.Join(f.received, "claimed.bin")
	f.update(t, claimed)
	if err := os.WriteFile(claimed.Path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f.writeIncoming(t, "claimed", content)

	f.seedActive(t, "whole", 5)
	f.writeIncoming(t, "whole", content)

	if err := f.in.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"delivered", "claimed", "whole"} {
		tr := f.transfer(t, id)
		if tr.Status != store.TransferDone || tr.Name != id+".bin" || tr.Done != 5 || tr.Path != filepath.Join(f.received, id+".bin") {
			t.Fatalf("%s transfer %+v", id, tr)
		}
		if stored, err := os.ReadFile(tr.Path); err != nil || !bytes.Equal(stored, content) {
			t.Fatalf("%s file %q, %v", id, stored, err)
		}
		if f.hasIncoming(id) {
			t.Fatalf("%s left files in the incoming dir", id)
		}
		if _, ok := f.in.Progress(id); ok {
			t.Fatalf("%s still tracked", id)
		}
	}
	if _, err := os.Stat(filepath.Join(f.received, "claimed (2).bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale claim produced a suffixed copy: %v", err)
	}
}

func TestSweepExpiresOldUploadsAndRemovesOrphans(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	oldID, oldTarget := f.create(t, deviceOne, "old.bin", 100)
	youngID, youngTarget := f.create(t, deviceOne, "young.bin", 100)
	for _, target := range []string{oldTarget, youngTarget} {
		if resp := f.patch(t, deviceOne, target, 0, []byte("0123456789")); resp.status != http.StatusNoContent {
			t.Fatalf("patch: %d %q", resp.status, resp.body)
		}
	}
	f.waitForProgress(t, oldID, 10)
	f.age(t, oldID)
	f.writeInfo(t, "orphan")
	if err := f.in.Sweep(t.Context(), 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if f.hasIncoming(oldID) {
		t.Fatal("old upload files still present")
	}
	if !f.hasIncoming(youngID) {
		t.Fatal("young upload files removed")
	}
	if _, err := os.Stat(filepath.Join(f.incoming, "orphan.info")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan info file: %v", err)
	}
	if old := f.transfer(t, oldID); old.Status != store.TransferFailed || old.Error != string(api.CodeExpired) || old.Done != 10 {
		t.Fatalf("old transfer %+v", old)
	}
	if event := f.nextTransfer(t, store.TransferFailed); event.ID != oldID || event.Error != api.CodeExpired {
		t.Fatalf("event %+v", event)
	}
	if _, ok := f.in.Progress(oldID); ok {
		t.Fatal("expired upload still tracked")
	}
	if resp := f.head(t, oldTarget); resp.status != http.StatusNotFound {
		t.Fatalf("HEAD of expired upload: %d", resp.status)
	}
}

func TestSweepFinishesCompleteUploadInsteadOfExpiring(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	tr := f.seedActive(t, "whole", 5)
	f.writeIncoming(t, "whole", []byte("12345"))
	f.age(t, "whole")
	f.in.track(tr)
	if err := f.in.Sweep(t.Context(), 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(f.received, "whole.bin"))
	if err != nil || string(stored) != "12345" {
		t.Fatalf("whole.bin = %q, %v", stored, err)
	}
	if got := f.transfer(t, "whole"); got.Status != store.TransferDone || got.Done != 5 {
		t.Fatalf("transfer %+v", got)
	}
	if f.hasIncoming("whole") {
		t.Fatal("incoming files remain")
	}
	if event := f.nextTransfer(t, store.TransferDone); event.ID != "whole" {
		t.Fatalf("event %+v", event)
	}
}
