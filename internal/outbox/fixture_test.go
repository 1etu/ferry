package outbox

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/store"
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
	outbox *Outbox
	store  *store.Store
	events <-chan events.Event
	dir    string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(t.Context(), filepath.Join(dir, "ferry.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
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
	if err != nil {
		t.Fatalf("insert device: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	hub := events.NewHub(log)
	t.Cleanup(hub.Close)
	subscription, cancel := hub.Subscribe(events.Scope{Owner: true})
	t.Cleanup(cancel)
	return fixture{
		outbox: New(st, hub, deviceFromContext, log),
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
