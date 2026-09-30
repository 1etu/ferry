package auth_test

import (
	"errors"
	"testing"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
	"github.com/1etu/ferry/internal/store"
)

func TestGraceRedemptionWithoutTheFragmentIsPendingAndEndsTheGrace(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	p := f.pairings.Current()
	safari := f.mustRedeem(auth.Redeem{Token: p.Token})
	f.mustApprove(safari.ID)
	f.drainEvents()

	sniffer := f.mustRedeem(auth.Redeem{Token: p.Token, RemoteIP: "192.168.1.99"})

	if sniffer.Status != store.DevicePending || sniffer.ID == safari.ID {
		t.Fatalf("token holder without the fragment got %+v, want a new pending device", sniffer)
	}
	if stored := f.storedDevice(sniffer.ID); stored.Status != store.DevicePending || stored.Secret != nil {
		t.Fatalf("stored %+v, want pending without a secret", stored)
	}
	requireDeviceEvent(t, f.drainEvents()[0], api.DeviceRequested, sniffer.ID)
	if _, _, err := f.redeem(auth.Redeem{Token: p.Token, RemoteIP: "192.168.1.51", HasSecret: true}); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("grace after a redemption without the fragment: got %v, want ErrInvalid", err)
	}
}
