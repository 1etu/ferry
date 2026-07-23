package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/oklog/ulid/v2"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/store"
)

func requireSamePairing(t *testing.T, got, want Pairing) {
	t.Helper()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("pairing differs (-want +got):\n%s", diff)
	}
}

func TestCurrentCreatesSessionAndRotatesAfterTenMinutes(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)

	first := f.pairings.Current()
	if len(first.Token) != 22 || len(first.Code) != 6 || len(first.Secret) != pairingSecretBytes {
		t.Fatalf("pairing %+v has a malformed token or code", first)
	}
	if want := f.clock.Now().Add(10 * time.Minute); !first.ExpiresAt.Equal(want) {
		t.Fatalf("expires at %v, want %v", first.ExpiresAt, want)
	}
	requireKinds(t, f.drainEvents(), events.KindPairing)

	f.clock.Advance(10*time.Minute - time.Millisecond)
	requireSamePairing(t, f.pairings.Current(), first)
	requireKinds(t, f.drainEvents())

	f.clock.Advance(time.Millisecond)
	next := f.pairings.Current()
	if next.Token == first.Token || bytes.Equal(next.Secret, first.Secret) {
		t.Fatal("expired session was not rotated with a fresh token and secret")
	}
	if want := f.clock.Now().Add(10 * time.Minute); !next.ExpiresAt.Equal(want) {
		t.Fatalf("expires at %v, want %v", next.ExpiresAt, want)
	}
	requireKinds(t, f.drainEvents(), events.KindPairing)
}

func TestPairingEventsCarryTheDisplayedSessionWithBothOrigins(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)

	first := f.pairings.Current()
	fragment := "#s=" + base64.RawURLEncoding.EncodeToString(first.Secret)
	want := api.Pairing{
		QRURL:     testIPOrigin + "/?pair=" + first.Token + fragment,
		LocalURL:  testLocalOrigin + "/?pair=" + first.Token + fragment,
		Code:      first.Code,
		ExpiresAt: "2026-10-02T14:13:07.123Z",
	}
	if got := f.pairings.View(first); got != want {
		t.Fatalf("view %+v, want %+v", got, want)
	}
	requirePairingEvent(t, f.drainEvents()[0], want)

	rotated := f.pairings.Rotate()
	requirePairingEvent(t, f.drainEvents()[0], f.pairings.View(rotated))

	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	wantJSON := `{"qrUrl":"` + want.QRURL + `","localUrl":"` + want.LocalURL + `","code":"` + first.Code + `","expiresAt":"2026-10-02T14:13:07.123Z"}`
	if string(encoded) != wantJSON {
		t.Fatalf("payload %s, want %s", encoded, wantJSON)
	}
}

func TestRotateReplacesDisplayedSessionAndPublishes(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	first := f.pairings.Current()
	f.drainEvents()

	rotated := f.pairings.Rotate()
	if rotated.Token == first.Token {
		t.Fatal("rotate kept the token")
	}
	requireSamePairing(t, f.pairings.Current(), rotated)
	requireKinds(t, f.drainEvents(), events.KindPairing)
}

func TestRedeemByTokenMatchesAnyLiveSession(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	first := f.pairings.Current()
	f.pairings.Rotate()

	if _, _, err := f.redeem(Redeem{Token: first.Token}); err != nil {
		t.Fatalf("token of a live replaced session: %v", err)
	}
}

func TestRedeemByCodeMatchesOnlyDisplayedSession(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	first := f.pairings.Current()
	rotated := f.pairings.Rotate()
	for rotated.Code == first.Code {
		rotated = f.pairings.Rotate()
	}

	if _, _, err := f.redeem(Redeem{Code: first.Code}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("code of a replaced session: got %v, want ErrInvalid", err)
	}
	if _, _, err := f.redeem(Redeem{Code: rotated.Code}); err != nil {
		t.Fatalf("code of the displayed session: %v", err)
	}
}

func TestRedeemRejectsUnknownAndExpiredSecrets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		redeem  func(p Pairing) Redeem
		advance time.Duration
		rotate  bool
		want    error
	}{
		{"unknown token is invalid", func(Pairing) Redeem { return Redeem{Token: "AAAAAAAAAAAAAAAAAAAAAA"} }, 0, false, ErrInvalid},
		{"empty token and code is invalid", func(Pairing) Redeem { return Redeem{} }, 0, false, ErrInvalid},
		{"displayed token after ten minutes is expired", func(p Pairing) Redeem { return Redeem{Token: p.Token} }, 10 * time.Minute, false, ErrExpired},
		{"displayed code after ten minutes is expired", func(p Pairing) Redeem { return Redeem{Code: p.Code} }, 10 * time.Minute, false, ErrExpired},
		{"replaced token after ten minutes is invalid", func(p Pairing) Redeem { return Redeem{Token: p.Token} }, 10 * time.Minute, true, ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newPairingsFixture(t)
			p := f.pairings.Current()
			if tc.rotate {
				f.pairings.Rotate()
			}
			f.clock.Advance(tc.advance)
			if _, _, err := f.redeem(tc.redeem(p)); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRedeemCreatesPendingDeviceAndRequestsApprovalFromOwner(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	p := f.pairings.Current()
	f.drainEvents()

	device, token, err := f.redeem(Redeem{Code: p.Code, Name: "Ege's iPhone"})
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if device.Status != store.DevicePending || device.Name != "Ege's iPhone" {
		t.Fatalf("device %+v, want pending named Ege's iPhone", device)
	}
	id, err := ulid.ParseStrict(device.ID)
	if err != nil {
		t.Fatalf("device id %q is not a ULID: %v", device.ID, err)
	}
	if got, want := ulid.Time(id.Time()), f.clock.Now().Truncate(time.Millisecond); !got.Equal(want) {
		t.Fatalf("ULID time %v, want %v", got, want)
	}
	stored, err := f.store.DeviceByTokenHash(t.Context(), HashToken(token))
	if err != nil {
		t.Fatalf("device by returned token: %v", err)
	}
	if stored.ID != device.ID || stored.Status != store.DevicePending || !stored.CreatedAt.Equal(device.CreatedAt) {
		t.Fatalf("stored %+v, want %+v", stored, device)
	}
	if strings.Contains(string(stored.TokenHash), token) {
		t.Fatal("store holds the plain token")
	}

	published := f.drainEvents()
	requireKinds(t, published, events.KindDevice)
	requested := published[0]
	if requested.DeviceID != "" || !requested.OwnerOnly {
		t.Fatalf("requested event scope DeviceID=%q OwnerOnly=%v, want empty and true", requested.DeviceID, requested.OwnerOnly)
	}
	requireDeviceEvent(t, requested, api.DeviceRequested, device.ID)
	encoded, err := json.Marshal(requested.Payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	want := `{"action":"requested","device":{"id":"` + device.ID + `","name":"Ege's iPhone","status":"pending","createdAt":"2026-10-02T14:03:07.123Z"}}`
	if string(encoded) != want {
		t.Fatalf("payload %s, want %s", encoded, want)
	}
}

func TestFiveWrongCodesRotateTheSession(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	p := f.pairings.Current()
	f.drainEvents()
	wrong := Redeem{Code: otherCode(p.Code)}

	for attempt := 1; attempt <= 4; attempt++ {
		if _, _, err := f.redeem(wrong); !errors.Is(err, ErrInvalid) {
			t.Fatalf("wrong code %d: got %v, want ErrInvalid", attempt, err)
		}
	}
	requireSamePairing(t, f.pairings.Current(), p)
	requireKinds(t, f.drainEvents())

	if _, _, err := f.redeem(wrong); !errors.Is(err, ErrInvalid) {
		t.Fatalf("fifth wrong code: got %v, want ErrInvalid", err)
	}
	requireKinds(t, f.drainEvents(), events.KindPairing)
	rotated := f.pairings.Current()
	if rotated.Token == p.Token {
		t.Fatal("fifth wrong code did not rotate the session")
	}
	if rotated.Code != p.Code {
		if _, _, err := f.redeem(Redeem{Code: p.Code}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("old code after rotation: got %v, want ErrInvalid", err)
		}
	}
}

func TestRedeemStoresTheDerivedSecretOnlyForATokenWithTheFragment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		redeem    func(p Pairing) Redeem
		hasSecret bool
	}{
		{"token with the fragment", func(p Pairing) Redeem { return Redeem{Token: p.Token, HasSecret: true} }, true},
		{"token without the fragment", func(p Pairing) Redeem { return Redeem{Token: p.Token} }, false},
		{"code claiming a fragment", func(p Pairing) Redeem { return Redeem{Code: p.Code, HasSecret: true} }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newPairingsFixture(t)
			p := f.pairings.Current()
			device := f.mustRedeem(tc.redeem(p))
			stored := f.storedDevice(device.ID)
			if !tc.hasSecret {
				if stored.Secret != nil {
					t.Fatalf("stored secret %x, want none (trust on first use)", stored.Secret)
				}
				return
			}
			want, err := seal.DeviceSecret(p.Secret)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stored.Secret, want) || !bytes.Equal(device.Secret, want) {
				t.Fatalf("stored secret %x, returned %x, want %x", stored.Secret, device.Secret, want)
			}
		})
	}
}

func TestUnusedGraceEndsAfterTwentyFourHours(t *testing.T) {
	t.Parallel()
	f := newPairingsFixture(t)
	p := f.pairings.Current()
	f.mustApprove(f.mustRedeem(Redeem{Token: p.Token}).ID)
	f.clock.Advance(24 * time.Hour)
	if _, _, err := f.redeem(Redeem{Token: p.Token, RemoteIP: "192.168.1.51"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("redeem after grace: got %v, want ErrInvalid", err)
	}
}
