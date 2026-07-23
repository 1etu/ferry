package auth

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestRedeemIsRateLimitedToTenPerMinutePerIP(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	p := f.pairings.Current()
	unknown := Redeem{Token: "AAAAAAAAAAAAAAAAAAAAAA", RemoteIP: "192.168.1.66"}

	for attempt := 1; attempt <= 10; attempt++ {
		if _, _, err := f.redeem(unknown); !errors.Is(err, ErrInvalid) {
			t.Fatalf("attempt %d: got %v, want ErrInvalid", attempt, err)
		}
	}
	if _, _, err := f.redeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.66"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("eleventh attempt with a valid token: got %v, want ErrRateLimited", err)
	}
	if _, _, err := f.redeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.67"}); err != nil {
		t.Fatalf("another IP: %v", err)
	}

	f.clock.Advance(time.Minute - time.Millisecond)
	if _, _, err := f.redeem(unknown); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("before the window ends: got %v, want ErrRateLimited", err)
	}
	f.clock.Advance(time.Millisecond)
	if _, _, err := f.redeem(unknown); !errors.Is(err, ErrInvalid) {
		t.Fatalf("after the window: got %v, want ErrInvalid", err)
	}
}

func TestRedeemRateLimitHoldsUnderConcurrency(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	const callers = 40
	results := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			_, _, err := f.redeem(Redeem{Token: "AAAAAAAAAAAAAAAAAAAAAA", RemoteIP: "192.168.1.66"})
			results <- err
		})
	}
	wg.Wait()
	close(results)

	limited := 0
	for err := range results {
		switch {
		case errors.Is(err, ErrRateLimited):
			limited++
		case !errors.Is(err, ErrInvalid):
			t.Fatalf("unexpected error %v", err)
		}
	}
	if limited != callers-10 {
		t.Fatalf("%d calls rate limited, want %d", limited, callers-10)
	}
}

func TestRedeemRateLimitIgnoresSourcePort(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	for port := range 10 {
		remote := "192.168.1.66:" + strconv.Itoa(50000+port)
		if _, _, err := f.redeem(Redeem{Token: "AAAAAAAAAAAAAAAAAAAAAA", RemoteIP: remote}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("attempt from %s: got %v, want ErrInvalid", remote, err)
		}
	}
	for _, remote := range []string{"192.168.1.66", "[::ffff:192.168.1.66]:50100"} {
		if _, _, err := f.redeem(Redeem{Token: "AAAAAAAAAAAAAAAAAAAAAA", RemoteIP: remote}); !errors.Is(err, ErrRateLimited) {
			t.Fatalf("attempt from %s: got %v, want ErrRateLimited", remote, err)
		}
	}
}
