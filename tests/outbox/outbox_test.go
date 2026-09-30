package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/outbox"
	"github.com/1etu/ferry/internal/store"
	"github.com/1etu/ferry/tests/kit"
)

const testDeviceID = "01J9ZK0X5S8V7Q2M3N4P5R6T7W"

type deviceKey struct{}

func withDevice(ctx context.Context, deviceID string) context.Context {
	return context.WithValue(ctx, deviceKey{}, deviceID)
}

func deviceFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(deviceKey{}).(string)
	return id, ok
}

type fixture struct {
	outbox *outbox.Outbox
	store  *store.Store
	events <-chan events.Event
	dir    string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(t.Context(), filepath.Join(dir, "ferry.db"))
	kit.NoError(t, err, "open store")
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	err = st.InsertDevice(t.Context(), store.Device{
		ID:        testDeviceID,
		Name:      "iPhone",
		TokenHash: []byte("token-hash"),
		Status:    store.DeviceApproved,
		CreatedAt: time.Now(),
	})
	kit.NoError(t, err, "insert device")
	log := slog.New(slog.DiscardHandler)
	hub := events.NewHub(log)
	t.Cleanup(hub.Close)
	subscription, cancel := hub.Subscribe(events.Scope{Owner: true})
	t.Cleanup(cancel)
	return fixture{
		outbox: outbox.New(st, hub, deviceFromContext, log),
		store:  st,
		events: subscription,
		dir:    dir,
	}
}

func (fx fixture) writeFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(fx.dir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func (fx fixture) offer(t *testing.T, name string, content []byte) store.File {
	t.Helper()
	offered, err := fx.outbox.Offer(t.Context(), []string{fx.writeFile(t, name, content)})
	if err != nil {
		t.Fatalf("offer %s: %v", name, err)
	}
	fx.drain()
	return offered[0]
}

func (fx fixture) drain() []events.Event {
	var received []events.Event
	for {
		select {
		case e, ok := <-fx.events:
			if !ok {
				return received
			}
			received = append(received, e)
		default:
			return received
		}
	}
}

func payloadJSON(t *testing.T, e events.Event) string {
	t.Helper()
	encoded, err := json.Marshal(e.Payload)
	kit.NoError(t, err, "encode payload")
	return string(encoded)
}

func TestOfferRejectsAnythingButRegularFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		paths func(t *testing.T, fx fixture) []string
	}{
		{
			name:  "directory",
			paths: func(_ *testing.T, fx fixture) []string { return []string{fx.dir} },
		},
		{
			name:  "missing path",
			paths: func(_ *testing.T, fx fixture) []string { return []string{filepath.Join(fx.dir, "gone.txt")} },
		},
		{
			name: "one bad path rejects the whole request",
			paths: func(t *testing.T, fx fixture) []string {
				t.Helper()
				return []string{fx.writeFile(t, "ok.txt", []byte("ok")), filepath.Join(fx.dir, "gone.txt")}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newFixture(t)
			_, err := fx.outbox.Offer(t.Context(), tt.paths(t, fx))
			if !errors.Is(err, outbox.ErrNotRegularFile) {
				t.Fatalf("got %v, want ErrNotRegularFile", err)
			}
			files, err := fx.store.Files(t.Context())
			kit.NoError(t, err, "list files")
			if len(files) != 0 {
				t.Fatalf("got %d stored files, want 0", len(files))
			}
			if got := fx.drain(); len(got) != 0 {
				t.Fatalf("got %d events, want 0", len(got))
			}
		})
	}
}

func TestOfferInsertsInRequestOrderAndPublishes(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	first := fx.writeFile(t, "first.txt", []byte("12345"))
	second := fx.writeFile(t, "second.bin", []byte("1"))

	offered, err := fx.outbox.Offer(t.Context(), []string{first, second})
	kit.NoError(t, err, "offer")
	if len(offered) != 2 || offered[0].Name != "first.txt" || offered[1].Name != "second.bin" {
		t.Fatalf("got %+v, want first.txt then second.bin", offered)
	}
	if offered[0].Path != first || offered[0].Size != 5 {
		t.Fatalf("got path %q size %d, want %q size 5", offered[0].Path, offered[0].Size, first)
	}
	stored, err := fx.store.File(t.Context(), offered[0].ID)
	kit.NoError(t, err, "load stored file")
	if stored != offered[0] {
		t.Fatalf("got stored %+v, want %+v", stored, offered[0])
	}

	published := fx.drain()
	if len(published) != 2 {
		t.Fatalf("got %d events, want 2", len(published))
	}
	e := published[0]
	if e.Kind != events.KindFile || e.DeviceID != "" || e.OwnerOnly {
		t.Fatalf("got event %+v, want a broadcast file event", e)
	}
	want := `{"action":"added","file":{"id":"` + offered[0].ID + `","name":"first.txt","size":5,"modifiedAt":"` +
		offered[0].ModTime.Format("2006-01-02T15:04:05.000Z") + `","createdAt":"` +
		offered[0].CreatedAt.Format("2006-01-02T15:04:05.000Z") + `"}}`
	if got := payloadJSON(t, e); got != want {
		t.Fatalf("got payload %s, want %s", got, want)
	}
}

func TestRemoveDeletesAndPublishes(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	f := fx.offer(t, "a.txt", []byte("a"))

	kit.NoError(t, fx.outbox.Remove(t.Context(), f.ID), "remove")
	if _, err := fx.store.File(t.Context(), f.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound after remove", err)
	}
	published := fx.drain()
	if len(published) != 1 || published[0].Kind != events.KindFile {
		t.Fatalf("got %+v, want one file event", published)
	}
	if got, ok := published[0].Payload.(api.FileChange); !ok || got.Action != api.FileRemoved || got.File.ID != f.ID {
		t.Fatalf("got payload %+v, want removed %s", published[0].Payload, f.ID)
	}
}

func TestRemoveUnknownFileIsNotFound(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	if err := fx.outbox.Remove(t.Context(), "01J9ZK0X5S8V7Q2M3N4P5R6T7X"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestSweepRemovesOnlyFilesOlderThanTheCutoff(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	fresh := fx.offer(t, "fresh.txt", []byte("fresh"))
	stale := store.File{
		ID:        "01J9ZK0X5S8V7Q2M3N4P5R6T7Y",
		Path:      fx.writeFile(t, "stale.txt", []byte("stale")),
		Name:      "stale.txt",
		Size:      5,
		ModTime:   time.Now(),
		CreatedAt: time.Now().Add(-8 * 24 * time.Hour),
	}
	kit.NoError(t, fx.store.InsertFile(t.Context(), stale), "insert stale file")

	kit.NoError(t, fx.outbox.Sweep(t.Context(), 7*24*time.Hour), "sweep")
	files, err := fx.store.Files(t.Context())
	kit.NoError(t, err, "list files")
	if len(files) != 1 || files[0].ID != fresh.ID {
		t.Fatalf("got %+v, want only %s", files, fresh.ID)
	}
	published := fx.drain()
	if len(published) != 1 {
		t.Fatalf("got %d events, want 1", len(published))
	}
	if got, ok := published[0].Payload.(api.FileChange); !ok || got.Action != api.FileRemoved || got.File.ID != stale.ID {
		t.Fatalf("got payload %+v, want removed %s", published[0].Payload, stale.ID)
	}
}
