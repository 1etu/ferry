package inbox

import (
	"io"
	"sync"

	"github.com/1etu/ferry/internal/seal"
)

const readAheadBytes = 8 * seal.FrameSize

type readAhead struct {
	body io.Reader
	pool *bufferPool

	mu        sync.Mutex
	pooled    *[]byte
	ring      []byte
	head      int
	tail      int
	count     int
	err       error
	isStarted bool
	isDrained bool
	isEnded   bool
	isStopped bool

	filled   chan struct{}
	freed    chan struct{}
	stopped  chan struct{}
	exited   chan struct{}
	stopOnce sync.Once
}

func newReadAhead(body io.Reader, pool *bufferPool) *readAhead {
	return &readAhead{
		body:    body,
		pool:    pool,
		filled:  make(chan struct{}, 1),
		freed:   make(chan struct{}, 1),
		stopped: make(chan struct{}),
		exited:  make(chan struct{}),
	}
}

func (r *readAhead) Read(p []byte) (int, error) {
	for {
		r.mu.Lock()
		if r.isStopped {
			started := r.isStarted
			r.mu.Unlock()
			return r.readThrough(p, started)
		}
		if r.isEnded {
			err := r.err
			r.mu.Unlock()
			return 0, err
		}
		if !r.isStarted {
			r.startLocked()
		}
		if r.count > 0 {
			wasFull := r.count == len(r.ring)
			n := r.takeLocked(p)
			r.mu.Unlock()
			if wasFull {
				signal(r.freed)
			}
			return n, nil
		}
		if r.isDrained {
			r.isEnded = true
			r.releaseLocked()
			err := r.err
			r.mu.Unlock()
			return 0, err
		}
		r.mu.Unlock()
		<-r.filled
	}
}

func (r *readAhead) stop() {
	r.stopOnce.Do(func() {
		r.mu.Lock()
		r.isStopped = true
		if r.isDrained || !r.isStarted {
			r.releaseLocked()
		}
		r.mu.Unlock()
		close(r.stopped)
	})
}

func (r *readAhead) readThrough(p []byte, started bool) (int, error) {
	if started {
		<-r.exited
	}
	return r.body.Read(p)
}

func (r *readAhead) startLocked() {
	r.isStarted = true
	r.pooled = r.pool.get()
	r.ring = *r.pooled
	go r.drain()
}

func (r *readAhead) takeLocked(p []byte) int {
	n := min(len(p), r.count, len(r.ring)-r.head)
	copy(p, r.ring[r.head:r.head+n])
	r.head = (r.head + n) % len(r.ring)
	r.count -= n
	return n
}

func (r *readAhead) releaseLocked() {
	if r.pooled == nil {
		return
	}
	r.pool.put(r.pooled)
	r.pooled, r.ring = nil, nil
}

func (r *readAhead) drain() {
	defer close(r.exited)
	for {
		region, ok := r.awaitSpace()
		if !ok {
			r.exit(nil)
			return
		}
		n, err := r.body.Read(region)
		r.mu.Lock()
		wasEmpty := r.count == 0
		r.tail = (r.tail + n) % len(r.ring)
		r.count += n
		r.mu.Unlock()
		if err != nil {
			r.exit(err)
			return
		}
		if wasEmpty {
			signal(r.filled)
		}
	}
}

func (r *readAhead) awaitSpace() ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.count == len(r.ring) && !r.isStopped {
		r.mu.Unlock()
		select {
		case <-r.freed:
		case <-r.stopped:
		}
		r.mu.Lock()
	}
	if r.isStopped {
		return nil, false
	}
	end := min(len(r.ring), r.tail+len(r.ring)-r.count)
	return r.ring[r.tail:end], true
}

func (r *readAhead) exit(err error) {
	r.mu.Lock()
	r.isDrained = true
	r.err = err
	if r.isStopped {
		r.releaseLocked()
	}
	r.mu.Unlock()
	signal(r.filled)
}

func signal(wake chan<- struct{}) {
	select {
	case wake <- struct{}{}:
	default:
	}
}
