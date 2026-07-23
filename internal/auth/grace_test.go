package auth

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/store"
)

func TestApproveStartsTwentyFourHourGraceAndRotatesDisplayedSession(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	p := f.pairings.Current()
	safari := f.mustRedeem(Redeem{Token: p.Token})
	f.drainEvents()

	f.clock.Advance(time.Minute)
	approved := f.mustApprove(safari.ID)
	if approved.Status != store.DeviceApproved || !approved.ApprovedAt.Equal(f.clock.Now()) {
		t.Fatalf("approved %+v, want approved at %v", approved, f.clock.Now())
	}
	if stored := f.storedDevice(safari.ID); stored.Status != store.DeviceApproved || !stored.ApprovedAt.Equal(approved.ApprovedAt) {
		t.Fatalf("stored %+v, want %+v", stored, approved)
	}
	published := f.drainEvents()
	requireKinds(t, published, events.KindDevice, events.KindPairing)
	if published[0].DeviceID != safari.ID {
		t.Fatalf("approved event addressed to %q, want %q", published[0].DeviceID, safari.ID)
	}
	requireDeviceEvent(t, published[0], api.DeviceApproved, safari.ID)
	current := f.pairings.Current()
	requirePairingEvent(t, published[1], f.pairings.View(current))
	if current.Token == p.Token {
		t.Fatal("approval did not rotate the displayed session")
	}

	f.clock.Advance(24*time.Hour - time.Millisecond)
	homeScreen, _, err := f.redeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.51", HasSecret: true})
	if err != nil {
		t.Fatalf("redeem within grace: %v", err)
	}
	if homeScreen.Status != store.DeviceApproved || homeScreen.ID == safari.ID {
		t.Fatalf("second context %+v, want a new approved device", homeScreen)
	}
	if want, err := seal.DeviceSecret(p.Secret); err != nil || !bytes.Equal(f.storedDevice(homeScreen.ID).Secret, want) {
		t.Fatalf("home screen context did not get the QR device secret: %v", err)
	}
	requireDeviceEvent(t, f.drainEvents()[0], api.DeviceApproved, homeScreen.ID)
}

func TestGraceAdmitsOneRedemptionThenEndsTheSessionForGood(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	p := f.pairings.Current()
	safari := f.mustRedeem(Redeem{Token: p.Token})
	other := f.mustRedeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.60"})
	f.mustApprove(safari.ID)
	f.mustRedeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.51"})

	if _, _, err := f.redeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.52"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("second redemption within the grace: got %v, want ErrInvalid", err)
	}
	f.mustApprove(other.ID)
	if _, _, err := f.redeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.53"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("redemption after approving another device of the used session: got %v, want ErrInvalid", err)
	}
}

func TestApproveOfCodeRedemptionStartsNoGraceForTheQRToken(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	p := f.pairings.Current()
	phone := f.mustRedeem(Redeem{Code: p.Code})
	f.drainEvents()

	f.mustApprove(phone.ID)
	requireKinds(t, f.drainEvents(), events.KindDevice, events.KindPairing)
	if current := f.pairings.Current(); current.Token == p.Token {
		t.Fatal("approval did not rotate the displayed session")
	}

	stranger, _, err := f.redeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.66"})
	if err != nil {
		t.Fatalf("redeem the old QR token inside its ten minutes: %v", err)
	}
	if stranger.Status != store.DevicePending {
		t.Fatalf("device holding the QR token is %s, want pending", stranger.Status)
	}

	f.clock.Advance(sessionLifetime)
	if _, _, err := f.redeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.67"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("old QR token after its ten minutes: got %v, want ErrInvalid", err)
	}
}

func TestApproveOfDeviceFromReplacedSessionKeepsDisplayedSession(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	first := f.pairings.Current()
	displayed := f.pairings.Rotate()
	device := f.mustRedeem(Redeem{Token: first.Token})
	f.drainEvents()

	f.mustApprove(device.ID)
	requireKinds(t, f.drainEvents(), events.KindDevice)
	requireSamePairing(t, f.pairings.Current(), displayed)
}

func TestApproveAfterSessionExpiredStillStartsGrace(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	p := f.pairings.Current()
	device := f.mustRedeem(Redeem{Token: p.Token})
	f.clock.Advance(30 * time.Minute)
	f.pairings.Current()

	f.mustApprove(device.ID)
	second, _, err := f.redeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.51", HasSecret: true})
	if err != nil {
		t.Fatalf("redeem after late approval: %v", err)
	}
	if second.Status != store.DeviceApproved {
		t.Fatalf("second context status %s, want approved", second.Status)
	}
}

func TestPendingDevicesStayPendingWhenAnotherIsApproved(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	p := f.pairings.Current()
	first := f.mustRedeem(Redeem{Token: p.Token})
	second := f.mustRedeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.51"})

	f.mustApprove(first.ID)
	if stored := f.storedDevice(second.ID); stored.Status != store.DevicePending {
		t.Fatalf("second device status %s, want pending", stored.Status)
	}
}

func TestApproveIsIdempotentForApprovedDevices(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	device := f.mustRedeem(Redeem{Token: f.pairings.Current().Token})
	first := f.mustApprove(device.ID)
	f.drainEvents()

	f.clock.Advance(time.Hour)
	again := f.mustApprove(device.ID)
	if !again.ApprovedAt.Equal(first.ApprovedAt) {
		t.Fatalf("approved at moved from %v to %v", first.ApprovedAt, again.ApprovedAt)
	}
	requireKinds(t, f.drainEvents())
}

func TestApproveRejectsUnknownAndRevokedDevices(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	if _, err := f.pairings.Approve(t.Context(), "01J9ZK0X5S8V7Q2M3N4P5R6T7W"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown device: got %v, want ErrNotFound", err)
	}

	device := f.mustRedeem(Redeem{Token: f.pairings.Current().Token})
	if _, err := f.pairings.Revoke(t.Context(), device.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := f.pairings.Approve(t.Context(), device.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked device: got %v, want ErrNotFound", err)
	}
	if stored := f.storedDevice(device.ID); stored.Status != store.DeviceRevoked {
		t.Fatalf("status %s, want revoked", stored.Status)
	}
}

func TestRevokeEndsGraceAndNotifiesDevice(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	p := f.pairings.Current()
	device := f.mustRedeem(Redeem{Token: p.Token})
	f.mustApprove(device.ID)
	f.drainEvents()
	var dropped []string
	f.pairings.OnRevoke = func(deviceID string) { dropped = append(dropped, deviceID) }

	revoked, err := f.pairings.Revoke(t.Context(), device.ID)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if revoked.Status != store.DeviceRevoked || f.storedDevice(device.ID).Status != store.DeviceRevoked {
		t.Fatalf("device %+v not revoked", revoked)
	}
	published := f.drainEvents()
	requireKinds(t, published, events.KindDevice)
	if published[0].DeviceID != device.ID {
		t.Fatalf("revoked event addressed to %q, want %q", published[0].DeviceID, device.ID)
	}
	requireDeviceEvent(t, published[0], api.DeviceRevoked, device.ID)
	if len(dropped) != 1 || dropped[0] != device.ID {
		t.Fatalf("seal sessions dropped for %v, want exactly %s", dropped, device.ID)
	}

	if _, _, err := f.redeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.51"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("grace token after revoke: got %v, want ErrInvalid", err)
	}
}

func TestApproveEndsDeviceStreamsAfterDeliveringApproval(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	device := f.mustRedeem(Redeem{Token: f.pairings.Current().Token})
	other := f.mustRedeem(Redeem{Token: f.pairings.Current().Token, RemoteIP: "192.168.1.51"})
	stream := f.deviceStream(device.ID)
	otherStream := f.deviceStream(other.ID)

	f.mustApprove(device.ID)

	requireStreamEnded(t, stream, events.Event{Kind: events.KindDevice, DeviceID: device.ID})
	select {
	case e := <-otherStream:
		t.Fatalf("other device's stream received %+v", e)
	default:
	}
}

func TestRevokeEndsDeviceStreamsAfterDeliveringRevocation(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	device := f.mustRedeem(Redeem{Token: f.pairings.Current().Token})
	f.mustApprove(device.ID)
	stream := f.deviceStream(device.ID)

	if _, err := f.pairings.Revoke(t.Context(), device.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	requireStreamEnded(t, stream, events.Event{Kind: events.KindDevice, DeviceID: device.ID})
}

func TestRevokeOfDeviceFromDisplayedSessionRotatesIt(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	p := f.pairings.Current()
	device := f.mustRedeem(Redeem{Code: p.Code})
	f.drainEvents()

	if _, err := f.pairings.Revoke(t.Context(), device.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	requireKinds(t, f.drainEvents(), events.KindDevice, events.KindPairing)
	if current := f.pairings.Current(); current.Token == p.Token {
		t.Fatal("displayed session survived the revocation of its device")
	}
	if _, _, err := f.redeem(Redeem{Token: p.Token}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("token of the revoked session: got %v, want ErrInvalid", err)
	}
}

func TestRevokeUnknownDeviceIsNotFound(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	f.pairings.OnRevoke = func(deviceID string) { t.Errorf("seal session of %s dropped by a failed revoke", deviceID) }
	if _, err := f.pairings.Revoke(t.Context(), "01J9ZK0X5S8V7Q2M3N4P5R6T7W"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestApproveAndRevokeRacesEndRevoked(t *testing.T) {
	t.Parallel()
	for range 20 {
		f := newPairingsFixture(t)
		device := f.mustRedeem(Redeem{Token: f.pairings.Current().Token})
		var wg sync.WaitGroup
		var revokeErr error
		wg.Go(func() {
			if _, err := f.pairings.Approve(t.Context(), device.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
				t.Errorf("approve: %v", err)
			}
		})
		wg.Go(func() {
			_, revokeErr = f.pairings.Revoke(t.Context(), device.ID)
		})
		wg.Wait()
		if revokeErr != nil {
			t.Fatalf("revoke: %v", revokeErr)
		}
		if stored := f.storedDevice(device.ID); stored.Status != store.DeviceRevoked {
			t.Fatalf("status %s after concurrent approve and revoke, want revoked", stored.Status)
		}
	}
}
