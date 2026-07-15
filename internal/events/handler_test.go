package events

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type streamRecorder struct {
	header        http.Header
	isStalled     bool
	writeDeadline time.Time
	deadlines     []time.Duration

	mu      sync.Mutex
	status  int
	body    strings.Builder
	flushed int
}

func newStreamRecorder() *streamRecorder {
	return &streamRecorder{header: http.Header{}}
}

func (s *streamRecorder) Header() http.Header {
	return s.header
}

func (s *streamRecorder) WriteHeader(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

func (s *streamRecorder) Write(p []byte) (int, error) {
	if s.isStalled {
		time.Sleep(time.Until(s.writeDeadline))
		return 0, os.ErrDeadlineExceeded
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.Write(p)
}

func (s *streamRecorder) SetWriteDeadline(deadline time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeDeadline = deadline
	if deadline.IsZero() {
		s.deadlines = append(s.deadlines, 0)
	} else {
		s.deadlines = append(s.deadlines, time.Until(deadline))
	}
	return nil
}

func (s *streamRecorder) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushed = s.body.Len()
}

func (s *streamRecorder) flushedBody() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.String()[:s.flushed]
}

type runningStream struct {
	recorder *streamRecorder
	cancel   context.CancelFunc
	done     chan struct{}
}

func startStream(t *testing.T, h *Hub, scope Scope) *runningStream {
	t.Helper()
	return startRecordedStream(t, h, scope, newStreamRecorder())
}

func startRecordedStream(t *testing.T, h *Hub, scope Scope, recorder *streamRecorder) *runningStream {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	s := &runningStream{recorder: recorder, cancel: cancel, done: make(chan struct{})}
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/events", http.NoBody)
	handler := h.Handler(func(*http.Request) (Scope, bool) { return scope, true })
	go func() {
		defer close(s.done)
		handler.ServeHTTP(s.recorder, req)
	}()
	synctest.Wait()
	return s
}

func assertFlushed(t *testing.T, s *runningStream, want string) {
	t.Helper()
	synctest.Wait()
	if got := s.recorder.flushedBody(); got != want {
		t.Fatalf("flushed %q, want %q", got, want)
	}
}

func TestHandlerStreamsFramedEvents(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub()
		s := startStream(t, h, Scope{DeviceID: "a", IsApproved: true})
		assertFlushed(t, s, retryFrame)

		h.Publish(Event{Kind: KindTransfer, DeviceID: "a", Payload: map[string]string{"id": "t1"}})
		h.Publish(Event{Kind: KindTransfer, DeviceID: "b", Payload: map[string]string{"id": "t2"}})
		h.Publish(Event{Kind: KindReset})
		assertFlushed(t, s, retryFrame+
			"event: transfer\ndata: {\"id\":\"t1\"}\n\n"+
			"event: reset\ndata: {}\n\n")

		s.cancel()
		<-s.done
		if s.recorder.status != http.StatusOK {
			t.Fatalf("got status %d, want 200", s.recorder.status)
		}
		if got := s.recorder.header.Get("Content-Type"); got != "text/event-stream" {
			t.Fatalf("got content type %q", got)
		}
		if got := s.recorder.header.Get("Cache-Control"); got != "no-store" {
			t.Fatalf("got cache control %q", got)
		}
	})
}

func TestHandlerSendsKeepAliveEveryInterval(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub()
		if h.keepAlive != 20*time.Second {
			t.Fatalf("got default keep-alive %v, want 20s", h.keepAlive)
		}
		h.keepAlive = 5 * time.Second
		s := startStream(t, h, Scope{Owner: true})

		time.Sleep(4 * time.Second)
		assertFlushed(t, s, retryFrame)

		time.Sleep(2 * time.Second)
		assertFlushed(t, s, retryFrame+keepAliveFrame)

		time.Sleep(5 * time.Second)
		assertFlushed(t, s, retryFrame+keepAliveFrame+keepAliveFrame)

		s.cancel()
		<-s.done
	})
}

func TestHandlerReturnsOnContextCancel(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub()
		s := startStream(t, h, Scope{DeviceID: "a"})
		if got := h.subscriberCount(); got != 1 {
			t.Fatalf("got %d subscribers while streaming, want 1", got)
		}
		s.cancel()
		<-s.done
		if got := h.subscriberCount(); got != 0 {
			t.Fatalf("got %d subscribers after cancel, want 0", got)
		}
	})
}

func TestHandlerReturnsWhenHubCloses(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub()
		s := startStream(t, h, Scope{Owner: true})
		h.Close()
		<-s.done
		s.cancel()
	})
}

func TestHandlerReturnsWhenDeviceIsDisconnected(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub()
		s := startStream(t, h, Scope{DeviceID: "a"})
		h.Disconnect("a")
		<-s.done
		s.cancel()
	})
}

func TestHandlerReturnsWhenStreamIsDropped(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub()
		oldest := startStream(t, h, Scope{DeviceID: "a"})
		rest := make([]*runningStream, 0, maxStreamsPerDevice)
		for range maxStreamsPerDevice {
			rest = append(rest, startStream(t, h, Scope{DeviceID: "a"}))
		}
		<-oldest.done
		oldest.cancel()
		for _, s := range rest {
			s.cancel()
			<-s.done
		}
	})
}

func TestHandlerSetsAWriteDeadlineBeforeEveryFrameAndClearsItOnReturn(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub()
		h.keepAlive = 5 * time.Second
		s := startStream(t, h, Scope{Owner: true})
		h.Publish(Event{Kind: KindReset})
		time.Sleep(6 * time.Second)
		assertFlushed(t, s, retryFrame+"event: reset\ndata: {}\n\n"+keepAliveFrame)

		s.cancel()
		<-s.done
		want := []time.Duration{writeTimeout, writeTimeout, writeTimeout, 0}
		if got := s.recorder.deadlines; !slices.Equal(got, want) {
			t.Fatalf("got write deadlines %v, want %v", got, want)
		}
	})
}

func TestHandlerReturnsWhenAStalledClientMissesTheWriteDeadline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub()
		stalled := newStreamRecorder()
		stalled.isStalled = true
		start := time.Now()
		s := startRecordedStream(t, h, Scope{Owner: true}, stalled)

		<-s.done
		if waited := time.Since(start); waited != writeTimeout {
			t.Fatalf("handler returned after %v, want %v", waited, writeTimeout)
		}
		if got := h.subscriberCount(); got != 0 {
			t.Fatalf("got %d subscribers after the stall, want 0", got)
		}
		s.cancel()
	})
}

func TestHandlerRejectsRequestWithoutScope(t *testing.T) {
	t.Parallel()
	h := newTestHub()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/events", http.NoBody)
	h.Handler(func(*http.Request) (Scope, bool) { return Scope{}, false }).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", rec.Code)
	}
	if got := rec.Body.String(); got != unauthorizedResponse {
		t.Fatalf("got body %q", got)
	}
	if got := h.subscriberCount(); got != 0 {
		t.Fatalf("got %d subscribers, want 0", got)
	}
}

func TestSealingStreamSealsNamesAndSkipsEventsItCannotSeal(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub()
		seal := func(plain string) (string, bool) { return "sealed-" + plain, plain != "lost.jpg" }
		device := startStream(t, h, Scope{DeviceID: "a", IsApproved: true, Seal: seal})
		owner := startStream(t, h, Scope{Owner: true})
		for _, name := range []string{"a.jpg", "lost.jpg"} {
			h.Publish(Event{Kind: KindTransfer, DeviceID: "a", Payload: namedPayload{Name: name}})
		}
		h.Publish(Event{Kind: KindReset})
		reset := "event: reset\ndata: {}\n\n"
		assertFlushed(t, device, retryFrame+"event: transfer\ndata: {\"name\":\"sealed-a.jpg\"}\n\n"+reset)
		assertFlushed(t, owner, retryFrame+"event: transfer\ndata: {\"name\":\"a.jpg\"}\n\n"+
			"event: transfer\ndata: {\"name\":\"lost.jpg\"}\n\n"+reset)
		for _, s := range []*runningStream{device, owner} {
			s.cancel()
			<-s.done
		}
	})
}
