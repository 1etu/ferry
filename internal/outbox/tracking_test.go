package outbox

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

type probeWriter struct {
	*httptest.ResponseRecorder
	onWrite func(written int)
}

func (p *probeWriter) Write(b []byte) (int, error) {
	n, err := p.ResponseRecorder.Write(b)
	p.onWrite(p.Body.Len())
	return n, err
}

func TestProgressIsLiveDuringTheResponseAndClearedAfter(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	size := 3*progressChunkBytes + 17
	f := fx.offer(t, "big.bin", testContent(size))
	var transferID string
	var observed []int64
	w := &probeWriter{ResponseRecorder: httptest.NewRecorder()}
	w.onWrite = func(written int) {
		if transferID == "" {
			active, err := fx.store.ActiveTransfer(t.Context(), testDeviceID, f.ID)
			if err != nil {
				t.Fatalf("active transfer during response: %v", err)
			}
			transferID = active.ID
		}
		done, ok := fx.outbox.Progress(transferID)
		if !ok {
			t.Fatalf("got no live progress after %d bytes", written)
		}
		observed = append(observed, done)
	}

	fx.outbox.Serve(w, contentRequest(t.Context(), f.ID, testDeviceID, nil), f.ID)

	if w.Body.Len() != size {
		t.Fatalf("got %d bytes, want %d", w.Body.Len(), size)
	}
	if len(observed) < 2 || observed[len(observed)-1] < int64(size-progressChunkBytes) {
		t.Fatalf("got live progress %v, want it to advance with the body", observed)
	}
	if _, ok := fx.outbox.Progress(transferID); ok {
		t.Fatal("got live progress after the response, want it cleared")
	}
	if got := fx.onlyTransfer(t); got.Done != int64(size) || got.Status != store.TransferDone {
		t.Fatalf("got done %d status %s, want %d done", got.Done, got.Status, size)
	}
}

func TestProgressEventsAreThrottled(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	fx.outbox.progressInterval = time.Hour
	f := fx.offer(t, "big.bin", testContent(4*progressChunkBytes))

	fx.serve(t, f.ID, testDeviceID, nil)

	var statuses []store.TransferStatus
	for _, e := range fx.drain() {
		if payload, ok := e.Payload.(api.Transfer); ok {
			statuses = append(statuses, payload.Status)
		}
	}
	if len(statuses) != 2 || statuses[0] != store.TransferActive || statuses[1] != store.TransferDone {
		t.Fatalf("got transfer events %v, want created then done only", statuses)
	}
}

var errClientGone = errors.New("client gone")

type disconnectingWriter struct {
	*httptest.ResponseRecorder
	remaining int
}

func (d *disconnectingWriter) Write(b []byte) (int, error) {
	if len(b) > d.remaining {
		n, err := d.ResponseRecorder.Write(b[:d.remaining])
		d.remaining = 0
		return n, errors.Join(errClientGone, err)
	}
	d.remaining -= len(b)
	return d.ResponseRecorder.Write(b)
}

func TestClientDisconnectKeepsProgressAndReleasesTracking(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	f := fx.offer(t, "big.bin", testContent(2*progressChunkBytes))
	cut := progressChunkBytes + 12345
	w := &disconnectingWriter{ResponseRecorder: httptest.NewRecorder(), remaining: cut}

	fx.outbox.Serve(w, contentRequest(t.Context(), f.ID, testDeviceID, nil), f.ID)

	got := fx.onlyTransfer(t)
	if got.Done != int64(cut) || got.Status != store.TransferActive {
		t.Fatalf("got done %d status %s, want %d active", got.Done, got.Status, cut)
	}
	if _, ok := fx.outbox.Progress(got.ID); ok {
		t.Fatal("got live progress after the client left, want it cleared")
	}
}

func TestServeOverRealConnections(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	size := 32 << 20
	path := fx.writeFile(t, "large.bin", nil)
	if err := os.Truncate(path, int64(size)); err != nil {
		t.Fatalf("size large file: %v", err)
	}
	offered, err := fx.outbox.Offer(t.Context(), []string{path})
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	f := offered[0]
	finished := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fx.outbox.Serve(w, r.WithContext(withDevice(r.Context(), testDeviceID)), f.ID)
		finished <- struct{}{}
	}))
	t.Cleanup(srv.Close)
	waitServed := func() {
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Fatal("handler did not return")
		}
	}

	partial := get(t, srv.URL)
	if _, err := io.CopyN(io.Discard, partial.Body, 1<<20); err != nil {
		t.Fatalf("read first megabyte: %v", err)
	}
	if err := partial.Body.Close(); err != nil {
		t.Fatalf("close partial body: %v", err)
	}
	waitServed()
	left := fx.onlyTransfer(t)
	if left.Status != store.TransferActive || left.Done <= 0 || left.Done >= int64(size) {
		t.Fatalf("got done %d status %s after the client left, want partial progress", left.Done, left.Status)
	}
	if _, ok := fx.outbox.Progress(left.ID); ok {
		t.Fatal("got live progress after the client left, want it cleared")
	}

	full := get(t, srv.URL)
	n, err := io.Copy(io.Discard, full.Body)
	if err != nil || n != int64(size) {
		t.Fatalf("got %d bytes, err %v, want %d", n, err, size)
	}
	if err := full.Body.Close(); err != nil {
		t.Fatalf("close full body: %v", err)
	}
	waitServed()
	completed := fx.onlyTransfer(t)
	if completed.ID != left.ID || completed.Status != store.TransferDone || completed.Done != int64(size) {
		t.Fatalf("got %+v, want %s done at %d", completed, left.ID, size)
	}
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want 200", resp.StatusCode)
	}
	return resp
}

type sizingWriter struct {
	header       http.Header
	written      int64
	largestWrite int
}

func (s *sizingWriter) Header() http.Header {
	return s.header
}

func (s *sizingWriter) WriteHeader(int) {}

func (s *sizingWriter) Write(p []byte) (int, error) {
	s.written += int64(len(p))
	s.largestWrite = max(s.largestWrite, len(p))
	return len(p), nil
}

func TestPooledCopyWritesQuarterMegabytePiecesFromOneReusedBuffer(t *testing.T) {
	content := testContent(8 * progressChunkBytes)
	sink := &sizingWriter{header: http.Header{}}
	var covered int64
	tracked := &trackingWriter{ResponseWriter: sink, isPooled: true, cover: func(_, end int64) { covered = end }}
	tracked.WriteHeader(http.StatusOK)
	reader := bytes.NewReader(content)
	tests := []struct {
		name   string
		writer io.ReaderFrom
		reset  func()
	}{
		{"tracked", tracked, func() { tracked.offset = 0 }},
		{"untracked", &pooledWriter{ResponseWriter: sink}, func() {}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(10, func() {
				tc.reset()
				reader.Reset(content)
				sink.written = 0
				if _, err := tc.writer.ReadFrom(&io.LimitedReader{R: reader, N: int64(len(content))}); err != nil {
					t.Fatalf("copy: %v", err)
				}
			})
			if allocs > 3 {
				t.Fatalf("got %.0f allocations per 8 MiB copy, want the pooled buffer reused", allocs)
			}
			if sink.written != int64(len(content)) || sink.largestWrite != copyBufferBytes {
				t.Fatalf("wrote %d bytes in pieces up to %d, want %d in %d", sink.written, sink.largestWrite, len(content), copyBufferBytes)
			}
		})
	}
	if covered != int64(len(content)) {
		t.Fatalf("tracked progress covered %d, want %d", covered, len(content))
	}
}
