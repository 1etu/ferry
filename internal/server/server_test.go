package server

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/1etu/ferry/internal/api"
)

type principalKind int

const (
	asOwner principalKind = iota
	asStranger
	asPendingDevice
	asApprovedDevice
)

type outcome struct {
	status int
	code   api.ErrorCode
}

func TestGuardsPerRoute(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	pending, _ := f.pendingDevice()
	approved, _ := f.approvedDevice()
	clients := map[principalKind]*client{
		asOwner:          f.owner(),
		asStranger:       f.stranger(),
		asPendingDevice:  pending,
		asApprovedDevice: approved,
	}
	unknown := newID()
	ok := func(status int) outcome { return outcome{status: status} }
	forbidden := outcome{http.StatusForbidden, api.CodeForbidden}
	unauthorized := outcome{http.StatusUnauthorized, api.CodeUnauthorized}
	pendingApproval := outcome{http.StatusForbidden, api.CodePendingApproval}
	notFound := outcome{http.StatusNotFound, api.CodeNotFound}
	tests := []struct {
		method, target string
		body           any
		want           map[principalKind]outcome
	}{
		{"GET", "/api/health", nil, everyone(ok(200))},
		{"GET", "/api/session", nil, everyone(ok(200))},
		{"GET", "/manifest.webmanifest", nil, everyone(ok(200))},
		{"GET", "/api/pairing", nil, ownerOnly(ok(200), forbidden)},
		{"POST", "/api/pairing", nil, ownerOnly(ok(200), forbidden)},
		{"GET", "/api/devices", nil, ownerOnly(ok(200), forbidden)},
		{"POST", "/api/devices/" + unknown + "/approve", nil, ownerOnly(notFound, forbidden)},
		{"POST", "/api/received/open", nil, ownerOnly(ok(204), forbidden)},
		{"POST", "/api/files", api.OfferRequest{}, ownerOnly(outcome{http.StatusBadRequest, api.CodeInvalidRequest}, forbidden)},
		{"POST", "/api/files/pick", nil, ownerOnly(ok(201), forbidden)},
		{"GET", "/api/transfers", nil, ownerOrApproved(ok(200), unauthorized, pendingApproval)},
		{"DELETE", "/api/transfers", nil, ownerOrApproved(ok(204), unauthorized, pendingApproval)},
		{"DELETE", "/api/transfers/" + unknown, nil, ownerOrApproved(notFound, unauthorized, pendingApproval)},
		{"GET", "/api/files", nil, ownerOrApproved(ok(200), unauthorized, pendingApproval)},
		{"DELETE", "/api/files/" + unknown, nil, ownerOrApproved(notFound, unauthorized, pendingApproval)},
		{"GET", "/api/files/" + unknown + "/content", nil, ownerOrApproved(notFound, unauthorized, pendingApproval)},
		{"DELETE", "/api/devices/" + unknown, nil, map[principalKind]outcome{
			asOwner: notFound, asStranger: unauthorized, asPendingDevice: forbidden, asApprovedDevice: forbidden,
		}},
		{"POST", "/api/uploads/", nil, map[principalKind]outcome{
			asOwner: forbidden, asStranger: unauthorized, asPendingDevice: pendingApproval, asApprovedDevice: ok(400),
		}},
		{"HEAD", "/api/uploads/" + unknown, nil, map[principalKind]outcome{
			asOwner: ok(403), asStranger: ok(401), asPendingDevice: ok(403), asApprovedDevice: ok(404),
		}},
	}
	for _, tc := range tests {
		for kind, want := range tc.want {
			req := clients[kind].request(tc.method, tc.target, tc.body)
			req.Header.Set("Tus-Resumable", "1.0.0")
			resp := clients[kind].do(req)
			if resp.status != want.status {
				t.Errorf("%s %s as %d: status %d, want %d, body %q", tc.method, tc.target, kind, resp.status, want.status, resp.body)
				continue
			}
			if want.code != "" {
				if got := errorCode(t, resp); got != want.code {
					t.Errorf("%s %s as %d: code %q, want %q", tc.method, tc.target, kind, got, want.code)
				}
			}
		}
	}
}

func everyone(o outcome) map[principalKind]outcome {
	return map[principalKind]outcome{asOwner: o, asStranger: o, asPendingDevice: o, asApprovedDevice: o}
}

func ownerOnly(owner, others outcome) map[principalKind]outcome {
	return map[principalKind]outcome{asOwner: owner, asStranger: others, asPendingDevice: others, asApprovedDevice: others}
}

func ownerOrApproved(allowed, stranger, pending outcome) map[principalKind]outcome {
	return map[principalKind]outcome{asOwner: allowed, asStranger: stranger, asPendingDevice: pending, asApprovedDevice: allowed}
}

func TestEventsRequireAPrincipal(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	expectError(t, f.stranger().send(http.MethodGet, "/api/events", nil), http.StatusUnauthorized, api.CodeUnauthorized)
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	evilHost := f.owner().request(http.MethodGet, "/api/health", nil)
	evilHost.Host = "evil.example"
	evilOrigin := f.owner().request(http.MethodPost, "/api/pairing", nil)
	evilOrigin.Header.Set("Origin", "http://evil.example")
	tests := []struct {
		name   string
		req    *http.Request
		status int
	}{
		{"json", f.owner().request(http.MethodGet, "/api/health", nil), http.StatusOK},
		{"guard error", f.stranger().request(http.MethodGet, "/api/pairing", nil), http.StatusForbidden},
		{"host rejected", evilHost, http.StatusMisdirectedRequest},
		{"origin rejected", evilOrigin, http.StatusForbidden},
		{"spa", f.owner().request(http.MethodGet, "/", nil), http.StatusOK},
		{"asset", f.owner().request(http.MethodGet, "/assets/app-1a2b.js", nil), http.StatusOK},
		{"unknown api", f.owner().request(http.MethodGet, "/api/nothing", nil), http.StatusNotFound},
	}
	for _, tc := range tests {
		resp := f.owner().do(tc.req)
		if resp.status != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, resp.status, tc.status)
		}
		for header, want := range map[string]string{
			"Content-Security-Policy": contentSecurityPolicy,
			"X-Content-Type-Options":  "nosniff",
			"Referrer-Policy":         "no-referrer",
			"X-Frame-Options":         "DENY",
		} {
			if got := resp.header.Get(header); got != want {
				t.Errorf("%s: %s %q, want %q", tc.name, header, got, want)
			}
		}
	}
}

func TestMiddlewareChainForwardsFlushToTheConnection(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{log: slog.New(slog.NewTextHandler(&lockedBuffer{}, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	handler := f.handler.chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("data: x\n\n")); err != nil {
			t.Error(err)
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush through the chain: %v", err)
		}
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/events", http.NoBody)
	req.Host = "127.0.0.1:1"
	req.RemoteAddr = "127.0.0.1:50000"
	handler.ServeHTTP(rec, req)
	if !rec.Flushed {
		t.Fatal("recorder was not flushed")
	}
}

func TestRecoverAnswersInternalErrorWithHeaders(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	handler := f.handler.chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	req.Host = "127.0.0.1:1"
	req.RemoteAddr = "127.0.0.1:50000"
	handler.ServeHTTP(rec, req)
	resp := response{status: rec.Code, header: rec.Header(), body: rec.Body.Bytes()}
	expectError(t, resp, http.StatusInternalServerError, api.CodeInternal)
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers missing on the panic response")
	}
}

func TestRecoverRepanicsAbortHandler(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	handler := f.handler.recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		recovered := recover()
		if err, isError := recovered.(error); !isError || !errors.Is(err, http.ErrAbortHandler) {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", recovered)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
}

func TestRequestLogNamesRouteDeviceAndForwardedRemote(t *testing.T) {
	t.Parallel()
	logs := &lockedBuffer{}
	f := newFixture(t, options{log: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	device, d := f.approvedDevice()
	expectStatus(t, device.send(http.MethodGet, "/api/files", nil), http.StatusOK)
	want := []string{`route="GET /api/files"`, "status=200", "device=" + d.ID, "remote=" + device.forwardedFor + ":"}
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, `route="GET /api/files"`) {
			for _, part := range want {
				if !strings.Contains(line, part) {
					t.Fatalf("request log %q lacks %q", line, part)
				}
			}
			return
		}
	}
	t.Fatalf("no request log line in %q", logs.String())
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
