package seal

import (
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func fixedClock() func() time.Time {
	return (&fakeClock{now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}).Now
}

func openSession(t *testing.T, sessions *Sessions, deviceID string) Handshake {
	t.Helper()
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	hs, err := sessions.Handshake(deviceID, nil, client.Public, nil)
	if err != nil {
		t.Fatal(err)
	}
	return hs
}

func TestSessionIDIs16RandomBytesAsUnpaddedBase64url(t *testing.T) {
	t.Parallel()
	sessions := NewSessions(fixedClock())
	first := openSession(t, sessions, "a").Session
	second := openSession(t, sessions, "b").Session
	for _, s := range []Session{first, second} {
		raw, err := base64.RawURLEncoding.DecodeString(s.ID)
		if err != nil || len(raw) != sessionIDSize || len(s.ID) != 22 {
			t.Fatalf("id %q: %v", s.ID, err)
		}
	}
	if first.ID == second.ID || first.Key == second.Key {
		t.Fatal("two sessions share an id or a key")
	}
	if first.DeviceID != "a" || second.DeviceID != "b" {
		t.Fatal("sessions do not carry their device id")
	}
}

func TestOneSessionPerDeviceReplacedByEveryHandshake(t *testing.T) {
	t.Parallel()
	sessions := NewSessions(fixedClock())
	first := openSession(t, sessions, "phone").Session
	other := openSession(t, sessions, "tablet").Session
	second := openSession(t, sessions, "phone").Session
	if _, ok := sessions.Lookup("phone", first.ID); ok {
		t.Fatal("the replaced session is still live")
	}
	got, ok := sessions.Lookup("phone", second.ID)
	if !ok || got != second {
		t.Fatalf("lookup %+v, ok %v", got.ID, ok)
	}
	if _, ok := sessions.Lookup("tablet", other.ID); !ok {
		t.Fatal("another device's session was disturbed")
	}
}

func TestLookupRejectsUnknownDevicesAndForeignIDs(t *testing.T) {
	t.Parallel()
	sessions := NewSessions(fixedClock())
	phone := openSession(t, sessions, "phone").Session
	tablet := openSession(t, sessions, "tablet").Session
	cases := []struct {
		name      string
		deviceID  string
		sessionID string
	}{
		{"unknown device", "watch", phone.ID},
		{"another device's id", "phone", tablet.ID},
		{"empty id", "phone", ""},
		{"id with a trailing byte", "phone", phone.ID + "A"},
		{"id prefix", "phone", phone.ID[:21]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, ok := sessions.Lookup(tc.deviceID, tc.sessionID); ok || got != (Session{}) {
				t.Fatalf("lookup returned %+v", got)
			}
		})
	}
}

func TestDropForgetsTheDevice(t *testing.T) {
	t.Parallel()
	sessions := NewSessions(fixedClock())
	phone := openSession(t, sessions, "phone").Session
	sealer := sessions.Sealer("phone")
	if _, ok := sealer("before"); !ok {
		t.Fatal("sealer failed before the drop")
	}
	sessions.Drop("phone")
	sessions.Drop("never seen")
	if _, ok := sessions.Lookup("phone", phone.ID); ok {
		t.Fatal("a dropped session is still live")
	}
	if sealed, ok := sealer("after"); ok || sealed != "" {
		t.Fatal("sealer still works after the drop")
	}
}

func TestSealerFollowsTheLiveSessionWithoutResubscribing(t *testing.T) {
	t.Parallel()
	sessions := NewSessions(fixedClock())
	sealer := sessions.Sealer("phone")
	if _, ok := sealer("too early"); ok {
		t.Fatal("sealer worked before any handshake")
	}
	first := openSession(t, sessions, "phone").Session
	sealed, ok := sealer("IMG_0001.HEIC")
	if !ok {
		t.Fatal("sealer failed")
	}
	if plain, err := OpenString(&first.Key, sealed); err != nil || plain != "IMG_0001.HEIC" {
		t.Fatalf("opened %q, err %v", plain, err)
	}
	second := openSession(t, sessions, "phone").Session
	resealed, ok := sealer("IMG_0002.HEIC")
	if !ok {
		t.Fatal("sealer failed after the second handshake")
	}
	if _, err := OpenString(&first.Key, resealed); !errors.Is(err, ErrString) {
		t.Fatal("the old key still opens names sealed after the re-handshake")
	}
	if plain, err := OpenString(&second.Key, resealed); err != nil || plain != "IMG_0002.HEIC" {
		t.Fatalf("opened %q, err %v", plain, err)
	}
}

func TestSessionsIdleForSevenDaysAreDroppedOnTheNextHandshake(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	sessions := NewSessions(clock.Now)
	active := openSession(t, sessions, "active").Session
	idle := openSession(t, sessions, "idle").Session
	clock.Advance(4 * 24 * time.Hour)
	if _, ok := sessions.Lookup("active", active.ID); !ok {
		t.Fatal("lookup failed on day four")
	}
	clock.Advance(3*24*time.Hour + time.Second)
	idleSealer := sessions.Sealer("idle")
	if _, ok := idleSealer("still here"); !ok {
		t.Fatal("an idle session is kept until a handshake prunes it")
	}
	openSession(t, sessions, "third")
	if _, ok := idleSealer("gone"); ok {
		t.Fatal("a session idle for over seven days survived a handshake")
	}
	if _, ok := sessions.Lookup("idle", idle.ID); ok {
		t.Fatal("a pruned session still answers lookups")
	}
	if _, ok := sessions.Lookup("active", active.ID); !ok {
		t.Fatal("a session used four days ago was dropped")
	}
}

func TestSessionsAreSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	sessions := NewSessions(fixedClock())
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			deviceID := string(rune('a' + i))
			sealer := sessions.Sealer(deviceID)
			for range 50 {
				hs := openSession(t, sessions, deviceID)
				sessions.Lookup(deviceID, hs.Session.ID)
				sealer("name")
				if i%2 == 0 {
					sessions.Drop(deviceID)
				}
			}
		})
	}
	wg.Wait()
}
