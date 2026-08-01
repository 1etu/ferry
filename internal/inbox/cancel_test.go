package inbox

import (
	"errors"
	"net/http"
	"testing"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

func TestCancelRemovesIdleUpload(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	id, target := f.create(t, deviceOne, "idle.bin", 100)
	if resp := f.patch(t, deviceOne, target, 0, []byte("0123456789")); resp.status != http.StatusNoContent {
		t.Fatalf("patch: %d %q", resp.status, resp.body)
	}
	f.waitForProgress(t, id, 10)

	if err := f.in.Cancel(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if f.hasIncoming(id) {
		t.Fatal("partial files still present")
	}
	if tr := f.transfer(t, id); tr.Status != store.TransferCanceled || tr.Done != 10 || tr.Error != "" {
		t.Fatalf("transfer %+v", tr)
	}
	if event := f.nextTransfer(t, store.TransferCanceled); event.ID != id {
		t.Fatalf("event %+v", event)
	}
	resp := f.head(t, target)
	if resp.status != http.StatusNotFound {
		t.Fatalf("HEAD after cancel: %d", resp.status)
	}
	if err := f.in.Cancel(t.Context(), id); !errors.Is(err, ErrNotActive) {
		t.Fatalf("second cancel: %v, want ErrNotActive", err)
	}
}

func TestCancelInterruptsRunningPatch(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	id, target := f.create(t, deviceOne, "running.bin", 1000)
	running := f.startPatch(t, target, 1000)
	running.write(t, []byte("0123456789"))
	f.waitForProgress(t, id, 10)

	if err := f.in.Cancel(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if resp := running.response(t); resp.status != http.StatusBadRequest {
		t.Fatalf("interrupted PATCH: %d %q", resp.status, resp.body)
	}
	if f.hasIncoming(id) {
		t.Fatal("partial files still present")
	}
	if tr := f.transfer(t, id); tr.Status != store.TransferCanceled {
		t.Fatalf("transfer %+v", tr)
	}
	if _, ok := f.in.Progress(id); ok {
		t.Fatal("canceled upload still tracked")
	}
}

func TestCancelDeviceCancelsOnlyThatDevice(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	first, _ := f.create(t, deviceOne, "one.bin", 10)
	second, _ := f.create(t, deviceOne, "two.bin", 10)
	other, _ := f.create(t, deviceTwo, "other.bin", 10)

	if err := f.in.CancelDevice(t.Context(), deviceOne); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{first, second} {
		if tr := f.transfer(t, id); tr.Status != store.TransferCanceled {
			t.Fatalf("transfer %s %+v", id, tr)
		}
		if f.hasIncoming(id) {
			t.Fatalf("files of %s still present", id)
		}
	}
	if tr := f.transfer(t, other); tr.Status != store.TransferActive {
		t.Fatalf("other device's transfer %+v", tr)
	}
	if err := f.in.CancelDevice(t.Context(), deviceOne); err != nil {
		t.Fatalf("second CancelDevice: %v", err)
	}
}

func TestCancelUnknownUploadIsNotActive(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	if err := f.in.Cancel(t.Context(), "01J9ZK0X5S8V7Q2M3N4P5R6T7W"); !errors.Is(err, ErrNotActive) {
		t.Fatalf("got %v, want ErrNotActive", err)
	}
}

func TestDeleteByDeviceTerminatesAndMarksCanceled(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	id, target := f.create(t, deviceOne, "delete.bin", 100)
	if resp := f.patch(t, deviceOne, target, 0, []byte("0123456789")); resp.status != http.StatusNoContent {
		t.Fatalf("patch: %d %q", resp.status, resp.body)
	}
	f.waitForProgress(t, id, 10)

	resp := f.send(t, f.request(t, http.MethodDelete, target, deviceOne, http.NoBody, nil))
	if resp.status != http.StatusNoContent {
		t.Fatalf("DELETE: %d %q", resp.status, resp.body)
	}
	if f.hasIncoming(id) {
		t.Fatal("partial files still present")
	}
	if tr := f.transfer(t, id); tr.Status != store.TransferCanceled || tr.Done != 10 {
		t.Fatalf("transfer %+v", tr)
	}
	if event := f.nextTransfer(t, store.TransferCanceled); event.ID != id || event.Done != 10 {
		t.Fatalf("event %+v", event)
	}
}

func TestOtherDeviceCannotTouchUpload(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	_, target := f.create(t, deviceOne, "mine.bin", 10)
	tests := []struct {
		method  string
		headers map[string]string
		hasBody bool
	}{
		{http.MethodHead, nil, false},
		{http.MethodPatch, map[string]string{"Upload-Offset": "0", "Content-Type": tusContentType}, true},
		{http.MethodDelete, nil, true},
	}
	for _, tc := range tests {
		resp := f.send(t, f.request(t, tc.method, target, deviceTwo, http.NoBody, tc.headers))
		if resp.status != http.StatusNotFound {
			t.Fatalf("%s by other device: %d", tc.method, resp.status)
		}
		if tc.hasBody {
			if got := errorCodeOf(t, resp); got != api.CodeNotFound {
				t.Fatalf("%s by other device: code %q", tc.method, got)
			}
		}
	}
	if resp := f.head(t, target); resp.status != http.StatusOK {
		t.Fatalf("HEAD by owner: %d", resp.status)
	}
}
