package update

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunChecksOnlyWhileEnabled(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("new"))
	f := newFixture(t, "1.0.0", r)
	f.u.firstDelay, f.u.interval = 5*time.Millisecond, 5*time.Millisecond
	var enabled atomic.Bool
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		f.u.Run(ctx, enabled.Load)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	if n := f.r.count(manifestPath); n != 0 {
		t.Fatalf("%d checks while disabled", n)
	}
	enabled.Store(true)
	waitFor(t, func() bool { return f.u.Status().State == StateReady })
	cancel()
	<-done
	if n := f.r.count(manifestPath); n == 0 {
		t.Fatal("no check after enabling")
	}
}

func TestStartCheckAnswersCheckingAndFinishesInTheBackground(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("new binary"))
	f := newFixture(t, "1.0.0", r)

	started := f.u.StartCheck(t.Context())

	if started.State != StateChecking || !started.CheckedAt.IsZero() {
		t.Fatalf("started %+v, want checking", started)
	}
	if st := f.u.StartCheck(t.Context()); st.State != StateChecking {
		t.Fatalf("second start answered %+v, want the running check", st)
	}
	waitFor(t, func() bool { return f.u.Status().State == StateReady })
	want := []State{StateChecking, StateDownloading, StateReady}
	if got := f.states(); !slices.Equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
	if n := f.r.count(manifestPath); n != 1 {
		t.Fatalf("manifest fetched %d times, want 1", n)
	}
}

func TestCheckIsSingleFlight(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("new"))
	f := newFixture(t, "1.0.0", r)
	if err := f.u.acquire(); err != nil {
		t.Fatal(err)
	}
	if st := f.u.Check(t.Context()); st.State != StateIdle || !st.CheckedAt.IsZero() {
		t.Fatalf("concurrent check ran: %+v", st)
	}
	if err := f.u.Stage(t.Context()); !errors.Is(err, ErrBusy) {
		t.Fatalf("Stage: %v", err)
	}
	f.u.release()
	if st := f.u.Check(t.Context()); st.State != StateReady {
		t.Fatalf("got %+v", st)
	}
}
