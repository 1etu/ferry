package app

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/1etu/ferry/internal/api"
)

type fakeOffers struct {
	status   int
	stall    chan struct{}
	posts    atomic.Int32
	received atomic.Pointer[api.OfferRequest]
}

func (o *fakeOffers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/health":
		if err := json.NewEncoder(w).Encode(api.Health{App: api.AppName}); err != nil {
			panic(err)
		}
	case "/api/files":
		o.posts.Add(1)
		if o.stall != nil {
			select {
			case <-o.stall:
			case <-r.Context().Done():
			}
			return
		}
		var req api.OfferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		o.received.Store(&req)
		w.WriteHeader(o.status)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func testSender(baseURL string, start func() error) sender {
	return sender{
		client:       &http.Client{Timeout: 5 * time.Second},
		baseURL:      baseURL,
		start:        start,
		pollInterval: 5 * time.Millisecond,
		startTimeout: 2 * time.Second,
	}
}

func TestSendOffersToTheRunningInstance(t *testing.T) {
	t.Parallel()
	offers := &fakeOffers{status: http.StatusCreated}
	server := httptest.NewServer(offers)
	t.Cleanup(server.Close)
	s := testSender(server.URL, func() error { t.Fatal("started a second instance"); return nil })
	if err := s.offer(t.Context(), []string{`C:\a.txt`, `C:\b.txt`}); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{`C:\a.txt`, `C:\b.txt`}, offers.received.Load().Paths); diff != "" {
		t.Fatalf("paths (-want +got):\n%s", diff)
	}
}

func TestSendFailsWhenTheOfferIsRejected(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(&fakeOffers{status: http.StatusNotFound})
	t.Cleanup(server.Close)
	if err := testSender(server.URL, func() error { return nil }).offer(t.Context(), []string{`C:\gone.txt`}); err == nil {
		t.Fatal("rejected offer reported success")
	}
}

func TestSendStartsTheAppWhenNothingListens(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	offers := &fakeOffers{status: http.StatusCreated}
	var starts atomic.Int32
	start := func() error {
		starts.Add(1)
		listener, err := net.Listen("tcp", net.JoinHostPort(loopbackHost, strconv.Itoa(port)))
		if err != nil {
			return err
		}
		server := &http.Server{Handler: offers, ReadHeaderTimeout: time.Second}
		go func() {
			if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
				t.Error(err)
			}
		}()
		t.Cleanup(func() {
			if err := server.Close(); err != nil {
				t.Error(err)
			}
		})
		return nil
	}
	if err := testSender(loopbackURL(port), start).offer(t.Context(), []string{`C:\a.txt`}); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 || offers.received.Load() == nil {
		t.Fatalf("starts %d, offer %v", starts.Load(), offers.received.Load())
	}
}

func TestSendGivesUpWhenTheStartedAppNeverAnswers(t *testing.T) {
	t.Parallel()
	s := testSender(loopbackURL(freePort(t)), func() error { return nil })
	s.startTimeout = 50 * time.Millisecond
	if err := s.offer(t.Context(), []string{`C:\a.txt`}); err == nil {
		t.Fatal("offer without a server reported success")
	}
}

func TestSendNeitherStartsTheAppNorRetriesWhenTheOfferTimesOut(t *testing.T) {
	t.Parallel()
	offers := &fakeOffers{status: http.StatusCreated, stall: make(chan struct{})}
	server := httptest.NewServer(offers)
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(offers.stall) })
	s := testSender(server.URL, func() error { t.Error("started a second instance"); return nil })
	s.client.Timeout = 50 * time.Millisecond

	if err := s.offer(t.Context(), []string{`C:\a.txt`}); err == nil {
		t.Fatal("timed out offer reported success")
	}
	if got := offers.posts.Load(); got != 1 {
		t.Fatalf("got %d offer requests, want 1", got)
	}
}
