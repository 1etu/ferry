package inbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/store"
)

const (
	deviceHeader   = "X-Test-Device"
	sealHeader     = "X-Ferry-Seal"
	deviceOne      = "d1"
	deviceTwo      = "d2"
	deviceUnsealed = "d3"
	waitTimeout    = 10 * time.Second
)

type deviceKey struct{}

func withDevice(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, deviceKey{}, id)
}

func deviceFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(deviceKey{}).(string)
	return id, ok
}

type fixture struct {
	in         *Inbox
	store      *store.Store
	hub        *events.Hub
	sessions   *seal.Sessions
	clients    map[string]sealedClient
	nonces     map[string][]byte
	server     *httptest.Server
	received   string
	incoming   string
	free       atomic.Uint64
	cancelBase context.CancelCauseFunc
	events     <-chan events.Event
	isClosed   bool
}

type response struct {
	status int
	header http.Header
	body   string
}

func newFixture(t *testing.T, adjust func(cfg *Config)) *fixture {
	t.Helper()
	f := &fixture{received: t.TempDir(), sessions: seal.NewSessions(time.Now), nonces: map[string][]byte{}}
	f.free.Store(1 << 40)
	f.incoming = filepath.Join(f.received, ".incoming")
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "ferry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	f.store = st
	f.clients = map[string]sealedClient{}
	for _, id := range []string{deviceOne, deviceTwo} {
		d := store.Device{ID: id, Name: "iPhone " + id, TokenHash: []byte("hash-" + id), Status: store.DeviceApproved, CreatedAt: time.Now()}
		if err := st.InsertDevice(t.Context(), d); err != nil {
			t.Fatal(err)
		}
		f.clients[id] = handshake(t, f.sessions, id)
	}
	f.hub = events.NewHub(slog.New(slog.DiscardHandler))
	stream, cancelStream := f.hub.Subscribe(events.Scope{Owner: true})
	t.Cleanup(cancelStream)
	f.events = stream
	cfg := Config{
		ReceivedDir:        f.received,
		IncomingDir:        f.incoming,
		MaxUploadBytes:     1 << 20,
		MaxActivePerDevice: 2,
		ProgressInterval:   10 * time.Millisecond,
		DeviceID:           deviceFromContext,
		Session:            SessionFromContext,
		FreeSpace:          func(string) (uint64, error) { return f.free.Load(), nil },
	}
	if adjust != nil {
		adjust(&cfg)
	}
	f.in, err = New(cfg, st, f.hub, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.closeInbox(t) })
	mux := http.NewServeMux()
	mux.Handle("/api/uploads/", http.StripPrefix("/api/uploads", f.in.Handler()))
	identify := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.Header.Get(deviceHeader); id != "" {
			ctx := withDevice(r.Context(), id)
			if session, ok := f.sessions.Lookup(id, r.Header.Get(sealHeader)); ok {
				ctx = ContextWithSession(ctx, session)
			}
			r = r.WithContext(ctx)
		}
		mux.ServeHTTP(w, r)
	})
	base, cancelBase := context.WithCancelCause(context.WithoutCancel(t.Context()))
	f.cancelBase = cancelBase
	f.server = httptest.NewUnstartedServer(identify)
	f.server.Config.BaseContext = func(net.Listener) context.Context { return base }
	f.server.Start()
	t.Cleanup(f.server.Close)
	return f
}

func (f *fixture) closeInbox(t *testing.T) {
	t.Helper()
	if f.isClosed {
		return
	}
	f.isClosed = true
	if err := f.in.Close(context.WithoutCancel(t.Context())); err != nil {
		t.Error(err)
	}
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (f *fixture) waitForProgress(t *testing.T, id string, done int64) {
	t.Helper()
	waitFor(t, "progress "+strconv.FormatInt(done, 10), func() bool {
		got, ok := f.in.Progress(id)
		return ok && got >= done
	})
}

func (f *fixture) nextTransfer(t *testing.T, status store.TransferStatus) api.Transfer {
	t.Helper()
	for {
		select {
		case e, ok := <-f.events:
			if !ok {
				t.Fatal("event stream closed")
			}
			transfer, isTransfer := e.Payload.(api.Transfer)
			if isTransfer && transfer.Status == status {
				return transfer
			}
		case <-time.After(waitTimeout):
			t.Fatalf("no transfer event with status %s", status)
		}
	}
}

func (f *fixture) expectNoTransferEvent(t *testing.T, id string, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case e := <-f.events:
			if transfer, ok := e.Payload.(api.Transfer); ok && (id == "" || transfer.ID == id) {
				t.Fatalf("unexpected transfer event %+v", transfer)
			}
		case <-deadline:
			return
		}
	}
}

func (f *fixture) seedActive(t *testing.T, id string, size int64) store.Transfer {
	t.Helper()
	now := time.Now()
	tr := store.Transfer{ID: id, DeviceID: deviceOne, Direction: store.DirectionIn, Name: id + ".bin", Size: size, Status: store.TransferActive, CreatedAt: now, UpdatedAt: now}
	if err := f.store.InsertTransfer(t.Context(), tr); err != nil {
		t.Fatal(err)
	}
	return tr
}

func (f *fixture) update(t *testing.T, tr store.Transfer) {
	t.Helper()
	if err := f.store.UpdateTransfer(t.Context(), tr); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) writeIncoming(t *testing.T, id string, content []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.incoming, id), content, 0o600); err != nil {
		t.Fatal(err)
	}
	f.writeInfo(t, id)
}

func (f *fixture) writeInfo(t *testing.T, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.incoming, id+infoSuffix), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) age(t *testing.T, id string) {
	t.Helper()
	stale := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(f.incoming, id), stale, stale); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) transfer(t *testing.T, id string) store.Transfer {
	t.Helper()
	tr, err := f.store.Transfer(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func (f *fixture) hasIncoming(id string) bool {
	_, dataErr := os.Stat(filepath.Join(f.incoming, id))
	_, infoErr := os.Stat(filepath.Join(f.incoming, id+infoSuffix))
	return dataErr == nil || infoErr == nil
}

func errorCodeOf(t *testing.T, resp response) api.ErrorCode {
	t.Helper()
	if got := resp.header.Get("Content-Type"); got != jsonContentType {
		t.Fatalf("content type %q, want %q", got, jsonContentType)
	}
	var body api.Error
	if err := json.Unmarshal([]byte(resp.body), &body); err != nil {
		t.Fatalf("decode %q: %v", resp.body, err)
	}
	return body.Error.Code
}

func handshake(t *testing.T, sessions *seal.Sessions, deviceID string) sealedClient {
	t.Helper()
	client, err := seal.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	opened, err := sessions.Handshake(deviceID, nil, client.Public, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := client.Finish(nil, opened.ServerKey, opened.Confirm)
	if err != nil {
		t.Fatal(err)
	}
	return sealedClient{sessionID: opened.Session.ID, key: key}
}
