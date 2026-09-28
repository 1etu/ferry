package inbox

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/1etu/ferry/internal/seal"
)

type countingBody struct {
	reader io.Reader
	read   atomic.Int64
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read.Add(int64(n))
	return n, err
}

type endlessBody struct{}

func (endlessBody) Read(p []byte) (int, error) { return len(p), nil }

type failingBody struct{ err error }

func (b failingBody) Read([]byte) (int, error) { return 0, b.err }

func newRing(body io.Reader) *readAhead {
	return newReadAhead(body, &bufferPool{bytes: readAheadBytes})
}

func isReleased(r *readAhead) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pooled == nil && r.ring == nil
}

func isDrained(r *readAhead) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isDrained
}

func drainGoroutines() int {
	stacks := make([]byte, 1<<20)
	n := runtime.Stack(stacks, true)
	return strings.Count(string(stacks[:n]), "(*readAhead).drain(")
}

func TestReadAheadDeliversTheBodyInOrderThenItsError(t *testing.T) {
	t.Parallel()
	content := randomBytes(t, 10*seal.FrameSize)
	errBody := errors.New("connection reset")
	pr, pw := io.Pipe()
	go func() {
		rest := content
		for len(rest) > 0 {
			n := min(7919, len(rest))
			if _, err := pw.Write(rest[:n]); err != nil {
				return
			}
			rest = rest[n:]
		}
		pw.CloseWithError(errBody)
	}()
	ring := newRing(pr)
	var out bytes.Buffer
	if _, err := io.Copy(&out, ring); !errors.Is(err, errBody) {
		t.Fatalf("err %v, want the body's error", err)
	}
	if !bytes.Equal(out.Bytes(), content) {
		t.Fatal("bytes differ after the read-ahead")
	}
	if !isReleased(ring) {
		t.Fatal("ring not returned to the pool after the body's error")
	}
	if _, err := ring.Read(make([]byte, 1)); !errors.Is(err, errBody) {
		t.Fatalf("the error is not sticky: %v", err)
	}
}

func TestReadAheadEndsWithEOFAndReturnsItsRing(t *testing.T) {
	t.Parallel()
	content := randomBytes(t, 2*seal.FrameSize+123)
	ring := newRing(bytes.NewReader(content))
	got, err := io.ReadAll(ring)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("read %d bytes, err %v", len(got), err)
	}
	if !isReleased(ring) {
		t.Fatal("ring not returned to the pool at EOF")
	}
	if n, err := ring.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("read after EOF: %d, %v", n, err)
	}
}

func TestReadAheadReadsAtMostTheRingAheadAndStopsWhenToldTo(t *testing.T) {
	t.Parallel()
	body := &countingBody{reader: endlessBody{}}
	ring := newRing(body)
	consumed := 0
	settle := func() {
		t.Helper()
		waitFor(t, "the ring to fill", func() bool {
			read := body.read.Load()
			if read > int64(consumed+readAheadBytes) {
				t.Fatalf("read %d bytes with %d consumed, more than the ring holds", read, consumed)
			}
			return read == int64(consumed+readAheadBytes)
		})
	}
	if _, err := io.ReadFull(ring, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	consumed++
	settle()
	if _, err := io.ReadFull(ring, make([]byte, seal.FrameSize)); err != nil {
		t.Fatal(err)
	}
	consumed += seal.FrameSize
	settle()

	ring.stop()
	waitFor(t, "the producer to exit", func() bool { return isReleased(ring) })
	if read := body.read.Load(); read != int64(consumed+readAheadBytes) {
		t.Fatalf("read %d bytes after stop, want %d", read, consumed+readAheadBytes)
	}
	if n, err := ring.Read(make([]byte, 8)); n != 8 || err != nil {
		t.Fatalf("read after stop: %d, %v, want 8 bytes straight from the body", n, err)
	}
	if read := body.read.Load(); read != int64(consumed+readAheadBytes+8) {
		t.Fatalf("body read %d bytes, want the ring's %d dropped and 8 read through", read, readAheadBytes)
	}
}

func TestReadAheadHoldsBytesThatArrivedBeforeTheBodyFailed(t *testing.T) {
	t.Parallel()
	content := randomBytes(t, 3*seal.FrameSize)
	errBody := errors.New("client cut")
	body := &countingBody{reader: io.MultiReader(bytes.NewReader(content), failingBody{errBody})}
	ring := newRing(body)
	head := make([]byte, 1)
	if _, err := io.ReadFull(ring, head); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the producer to hit the error", func() bool { return isDrained(ring) })
	if isReleased(ring) {
		t.Fatal("ring released while bytes are still unread")
	}
	rest, err := io.ReadAll(ring)
	if !errors.Is(err, errBody) {
		t.Fatalf("err %v, want the body's error", err)
	}
	if !bytes.Equal(slices.Concat(head, rest), content) {
		t.Fatal("bytes read before the failure were lost")
	}
	if !isReleased(ring) {
		t.Fatal("ring not returned to the pool after the error")
	}
}

func TestReadAheadStoppedBeforeTheFirstReadStartsNoProducer(t *testing.T) {
	t.Parallel()
	body := &countingBody{reader: endlessBody{}}
	ring := newRing(body)
	ring.stop()
	if n, err := ring.Read(make([]byte, 5)); n != 5 || err != nil {
		t.Fatalf("read after stop: %d, %v, want 5 bytes straight from the body", n, err)
	}
	ring.mu.Lock()
	started := ring.isStarted
	ring.mu.Unlock()
	if started || body.read.Load() != 5 {
		t.Fatalf("started %v, body read %d: a stopped ring must only pass reads through", started, body.read.Load())
	}
}

func TestReadAheadLeavesNoGoroutineBehind(t *testing.T) {
	before := drainGoroutines()
	content := randomBytes(t, 2*seal.FrameSize)
	if _, err := io.ReadAll(newRing(bytes.NewReader(content))); err != nil {
		t.Fatal(err)
	}
	failing := newRing(io.MultiReader(bytes.NewReader(content), failingBody{errors.New("cut")}))
	if _, err := io.ReadAll(failing); err == nil {
		t.Fatal("failing body ended without an error")
	}
	full := newRing(endlessBody{})
	if _, err := io.ReadFull(full, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	full.stop()
	waitFor(t, "read-ahead goroutines to exit", func() bool { return drainGoroutines() == before })
}
