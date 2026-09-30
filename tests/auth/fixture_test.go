package auth_test

import (
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/store"
	"github.com/1etu/ferry/tests/kit"
)

const (
	testLocalOrigin = "http://egetu-pc.local:8080"
	testIPOrigin    = "http://192.168.1.23:8080"
)

func testOrigins() (local, ip string) {
	return testLocalOrigin, testIPOrigin
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 2, 14, 3, 7, 123_000_000, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "ferry.db"))
	kit.NoError(t, err, "open store")
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return st
}

type pairingsFixture struct {
	t        *testing.T
	clock    *fakeClock
	store    *store.Store
	hub      *events.Hub
	pairings *auth.Pairings
	events   <-chan events.Event
}

func newPairingsFixture(t *testing.T) *pairingsFixture {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	hub := events.NewHub(log)
	t.Cleanup(hub.Close)
	ownerEvents, unsubscribe := hub.Subscribe(events.Scope{Owner: true})
	t.Cleanup(unsubscribe)
	clock := newFakeClock()
	st := openTestStore(t)
	return &pairingsFixture{
		t:        t,
		clock:    clock,
		store:    st,
		hub:      hub,
		pairings: auth.NewPairings(st, hub, testOrigins, clock.Now, log),
		events:   ownerEvents,
	}
}

func (f *pairingsFixture) redeem(r auth.Redeem) (store.Device, string, error) {
	if r.Name == "" {
		r.Name = "iPhone"
	}
	if r.RemoteIP == "" {
		r.RemoteIP = "192.168.1.50"
	}
	return f.pairings.Redeem(f.t.Context(), r)
}

func (f *pairingsFixture) mustRedeem(r auth.Redeem) store.Device {
	f.t.Helper()
	device, _, err := f.redeem(r)
	if err != nil {
		f.t.Fatalf("redeem: %v", err)
	}
	return device
}

func (f *pairingsFixture) mustApprove(deviceID string) store.Device {
	f.t.Helper()
	device, err := f.pairings.Approve(f.t.Context(), deviceID)
	if err != nil {
		f.t.Fatalf("approve: %v", err)
	}
	return device
}

func (f *pairingsFixture) drainEvents() []events.Event {
	var drained []events.Event
	for {
		select {
		case e := <-f.events:
			drained = append(drained, e)
		default:
			return drained
		}
	}
}

func (f *pairingsFixture) storedDevice(id string) store.Device {
	f.t.Helper()
	device, err := f.store.Device(f.t.Context(), id)
	if err != nil {
		f.t.Fatalf("stored device: %v", err)
	}
	return device
}

func requireDeviceEvent(t *testing.T, e events.Event, action, deviceID string) {
	t.Helper()
	payload, ok := e.Payload.(api.DeviceChange)
	if !ok || e.Kind != events.KindDevice || payload.Action != action || payload.Device.ID != deviceID {
		t.Fatalf("got %s event %+v, want device %s for %s", e.Kind, payload, action, deviceID)
	}
}
