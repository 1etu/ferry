package server_test

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/inbox"
	"github.com/1etu/ferry/internal/store"
	"github.com/1etu/ferry/tests/kit"
)

func TestUploadThroughTheServerLandsInReceivedDir(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, _ := f.approvedDevice()
	content := []byte("hello from the phone")
	_, target := f.createUpload(device, "note.txt", int64(len(content)))
	expectStatus(t, device.do(patchRequest(device, target, 0, content)), http.StatusNoContent)
	got, err := os.ReadFile(filepath.Join(f.received, "note.txt"))
	kit.NoError(t, err)
	if !bytes.Equal(got, content) {
		t.Fatalf("received %q", got)
	}
}

func TestShutdownCauseAnswersInFlightPatchAndKeepsThePartialFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, _ := f.approvedDevice()
	id, target := f.createUpload(device, "movie.mov", 1000)
	running := f.startPatch(device, target, 1000)
	running.write(t, bytes.Repeat([]byte{7}, 300))
	waitFor(t, "progress", func() bool { done, ok := f.inbox.Progress(id); return ok && done >= 300 })

	f.cancelBase(inbox.ErrShutdown)
	if resp := running.response(t); resp.status != http.StatusServiceUnavailable {
		t.Fatalf("in-flight PATCH answered %d %q", resp.status, resp.body)
	}
	info, err := os.Stat(filepath.Join(f.received, ".incoming", id))
	kit.NoError(t, err)
	if info.Size() != 300 {
		t.Fatalf("partial file holds %d bytes", info.Size())
	}
}

func TestTransfersLimitIsValidated(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	for query, status := range map[string]int{"0": 400, "501": 400, "abc": 400, "-1": 400, "1": 200, "500": 200} {
		resp := f.owner().send(http.MethodGet, "/api/transfers?limit="+query, nil)
		if resp.status != status {
			t.Errorf("limit %s: status %d, want %d", query, resp.status, status)
		}
	}
}

func TestTransfersAreScopedToTheDevice(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	first, firstDevice := f.approvedDevice()
	_, secondDevice := f.approvedDevice()
	own := f.insertTransfer(firstDevice.ID, store.DirectionIn, store.TransferDone)
	foreign := f.insertTransfer(secondDevice.ID, store.DirectionIn, store.TransferDone)

	if got := decode[[]api.Transfer](t, first.send(http.MethodGet, "/api/transfers", nil)); len(got) != 1 || got[0].ID != own.ID {
		t.Fatalf("device sees %+v", got)
	}
	if got := decode[[]api.Transfer](t, f.owner().send(http.MethodGet, "/api/transfers", nil)); len(got) != 2 {
		t.Fatalf("owner sees %+v", got)
	}
	expectError(t, first.send(http.MethodDelete, "/api/transfers/"+foreign.ID, nil), http.StatusNotFound, api.CodeNotFound)

	expectStatus(t, first.send(http.MethodDelete, "/api/transfers", nil), http.StatusNoContent)
	if got := decode[[]api.Transfer](t, f.owner().send(http.MethodGet, "/api/transfers", nil)); len(got) != 1 || got[0].ID != foreign.ID {
		t.Fatalf("after the device cleared its history the owner sees %+v", got)
	}
	expectStatus(t, f.owner().send(http.MethodDelete, "/api/transfers", nil), http.StatusNoContent)
	if got := decode[[]api.Transfer](t, f.owner().send(http.MethodGet, "/api/transfers", nil)); len(got) != 0 {
		t.Fatalf("after the owner cleared everything %+v", got)
	}
}

func TestDeleteTransfer(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, d := f.approvedDevice()
	stream, unsubscribe := f.hub.Subscribe(events.Scope{Owner: true})
	t.Cleanup(unsubscribe)

	finished := f.insertTransfer(d.ID, store.DirectionIn, store.TransferDone)
	expectStatus(t, device.send(http.MethodDelete, "/api/transfers/"+finished.ID, nil), http.StatusNoContent)
	if _, err := f.store.Transfer(t.Context(), finished.ID); err == nil {
		t.Fatal("finished transfer still stored")
	}

	notLive := f.insertTransfer(d.ID, store.DirectionIn, store.TransferActive)
	expectError(t, f.owner().send(http.MethodDelete, "/api/transfers/"+notLive.ID, nil), http.StatusConflict, api.CodeConflict)

	liveID, _ := f.createUpload(device, "clip.mov", 1000)
	expectStatus(t, device.send(http.MethodDelete, "/api/transfers/"+liveID, nil), http.StatusNoContent)
	if tr, err := f.store.Transfer(t.Context(), liveID); err != nil || tr.Status != store.TransferCanceled {
		t.Fatalf("live upload after cancel: %+v %v", tr, err)
	}

	download := f.insertTransfer(d.ID, store.DirectionOut, store.TransferActive)
	expectStatus(t, f.owner().send(http.MethodDelete, "/api/transfers/"+download.ID, nil), http.StatusNoContent)
	if tr, err := f.store.Transfer(t.Context(), download.ID); err != nil || tr.Status != store.TransferCanceled {
		t.Fatalf("download after cancel: %+v %v", tr, err)
	}
	expectTransferEvent(t, stream, download.ID, store.TransferCanceled)
}

func expectTransferEvent(t *testing.T, stream <-chan events.Event, id string, status store.TransferStatus) {
	t.Helper()
	timeout := time.After(waitTimeout)
	for {
		select {
		case e := <-stream:
			if tr, ok := e.Payload.(api.Transfer); ok && tr.ID == id && tr.Status == status {
				return
			}
		case <-timeout:
			t.Fatalf("no %s event for transfer %s", status, id)
		}
	}
}

func TestTransfersOverlayLiveUploadProgress(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, _ := f.approvedDevice()
	id, target := f.createUpload(device, "big.bin", 1000)
	running := f.startPatch(device, target, 1000)
	running.write(t, bytes.Repeat([]byte{1}, 400))
	waitFor(t, "progress", func() bool { done, ok := f.inbox.Progress(id); return ok && done >= 400 })
	stored, err := f.store.Transfer(t.Context(), id)
	kit.NoError(t, err)
	listed := decode[[]api.Transfer](t, device.send(http.MethodGet, "/api/transfers", nil))
	if len(listed) != 1 || listed[0].Done < 400 || stored.Done >= 400 {
		t.Fatalf("listed %+v, stored done %d", listed, stored.Done)
	}
}

func TestOpenReceived(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	_, d := f.approvedDevice()
	done := f.insertTransfer(d.ID, store.DirectionIn, store.TransferDone)
	active := f.insertTransfer(d.ID, store.DirectionIn, store.TransferActive)
	sent := f.insertTransfer(d.ID, store.DirectionOut, store.TransferDone)

	expectStatus(t, f.owner().send(http.MethodPost, "/api/received/open", nil), http.StatusNoContent)
	expectStatus(t, f.owner().send(http.MethodPost, "/api/received/open", api.OpenReceivedRequest{TransferID: done.ID}), http.StatusNoContent)
	for _, id := range []string{active.ID, sent.ID, newID()} {
		resp := f.owner().send(http.MethodPost, "/api/received/open", api.OpenReceivedRequest{TransferID: id})
		expectError(t, resp, http.StatusNotFound, api.CodeNotFound)
	}
	expectError(t, f.owner().send(http.MethodPost, "/api/received/open", `{"transfer":1}`), http.StatusBadRequest, api.CodeInvalidRequest)

	opened := f.openedPaths()
	if len(opened) != 2 || opened[0] != f.received || opened[1] != done.Path {
		t.Fatalf("opened %q", opened)
	}
}
