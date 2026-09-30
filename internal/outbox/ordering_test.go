package outbox

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/store"
)

func transferStatuses(published []events.Event) []store.TransferStatus {
	var statuses []store.TransferStatus
	for _, e := range published {
		if payload, ok := e.Payload.(api.Transfer); ok {
			statuses = append(statuses, payload.Status)
		}
	}
	return statuses
}

func requireEndsSettled(t *testing.T, statuses []store.TransferStatus, settled store.TransferStatus) {
	t.Helper()
	if len(statuses) == 0 || statuses[len(statuses)-1] != settled {
		t.Fatalf("got transfer events %v, want the last one %s", statuses, settled)
	}
	for i, status := range statuses[:len(statuses)-1] {
		if status != store.TransferActive {
			t.Fatalf("got %s at event %d of %v, want only active before the settled event", status, i, statuses)
		}
	}
}

func TestTailRangeThenHeadCompletesTheTransfer(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	f := fx.offer(t, "movie.mov", testContent(1000))

	fx.serve(t, f.ID, http.Header{"Range": {"bytes=600-"}})
	fx.serve(t, f.ID, http.Header{"Range": {"bytes=0-599"}})

	if got := fx.onlyTransfer(t); got.Done != 600 || got.Status != store.TransferActive {
		t.Fatalf("got done %d status %s after separate requests, want 600 active", got.Done, got.Status)
	}

	var transferID string
	w := &probeWriter{ResponseRecorder: httptest.NewRecorder()}
	w.onWrite = func(int) {
		if transferID != "" {
			return
		}
		transferID = fx.onlyTransfer(t).ID
		fx.serve(t, f.ID, http.Header{"Range": {"bytes=0-599"}})
	}
	fx.outbox.Serve(w, contentRequest(t, f.ID, http.Header{"Range": {"bytes=600-"}}), f.ID)

	if got := fx.onlyTransfer(t); got.Done != 1000 || got.Status != store.TransferDone {
		t.Fatalf("got done %d status %s with head and tail served together, want 1000 done", got.Done, got.Status)
	}
}

func TestNoProgressIsPublishedAfterAParallelRequestCompletes(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	fx.outbox.progressInterval = 0
	f := fx.offer(t, "big.bin", testContent(3*progressChunkBytes))

	isParallelServed := false
	w := &probeWriter{ResponseRecorder: httptest.NewRecorder()}
	w.onWrite = func(int) {
		if isParallelServed {
			return
		}
		isParallelServed = true
		fx.serve(t, f.ID, nil)
	}
	fx.outbox.Serve(w, contentRequest(t, f.ID, nil), f.ID)

	requireEndsSettled(t, transferStatuses(fx.drain()), store.TransferDone)
	if got := fx.onlyTransfer(t); got.Status != store.TransferDone || got.Done != int64(3*progressChunkBytes) {
		t.Fatalf("got %+v, want done", got)
	}
}

func TestCancelDuringADownloadEndsItsEvents(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	fx.outbox.progressInterval = 0
	f := fx.offer(t, "big.bin", testContent(3*progressChunkBytes))

	var canceledID string
	w := &probeWriter{ResponseRecorder: httptest.NewRecorder()}
	w.onWrite = func(written int) {
		if canceledID != "" || written < progressChunkBytes+1 {
			return
		}
		canceledID = fx.onlyTransfer(t).ID
		if err := fx.outbox.Cancel(t.Context(), canceledID); err != nil {
			t.Fatalf("cancel: %v", err)
		}
	}
	fx.outbox.Serve(w, contentRequest(t, f.ID, nil), f.ID)

	requireEndsSettled(t, transferStatuses(fx.drain()), store.TransferCanceled)
	got := fx.onlyTransfer(t)
	if got.ID != canceledID || got.Status != store.TransferCanceled || got.Done != progressChunkBytes {
		t.Fatalf("got %+v, want %s canceled at the first chunk", got, canceledID)
	}
}

func TestCancelOfAFinishedDownloadKeepsIt(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	f := fx.offer(t, "small.bin", testContent(100))
	fx.serve(t, f.ID, nil)
	done := fx.onlyTransfer(t)
	fx.drain()

	if err := fx.outbox.Cancel(t.Context(), done.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := fx.onlyTransfer(t); got.Status != store.TransferDone {
		t.Fatalf("got status %s, want done", got.Status)
	}
	if published := fx.drain(); len(published) != 0 {
		t.Fatalf("got events %v, want none", published)
	}
}
