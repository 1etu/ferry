package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

func TestIdentify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		isDev         bool
		remote        string
		forwardedFor  []string
		cookie        store.DeviceStatus
		unknownCookie bool
		wantRole      Role
		wantRemote    string
		wantCleared   bool
	}{
		{name: "loopback without cookie is owner", remote: loopbackPeer, wantRole: RoleOwner},
		{name: "IPv6 loopback is owner", remote: "[::1]:51000", wantRole: RoleOwner},
		{name: "IPv4-mapped loopback is owner", remote: "[::ffff:127.0.0.1]:51000", wantRole: RoleOwner},
		{name: "LAN without cookie is none", remote: lanPeer, wantRole: RoleNone},
		{name: "unparsable remote is none", remote: "pipe", wantRole: RoleNone},
		{name: "approved cookie is device", remote: lanPeer, cookie: store.DeviceApproved, wantRole: RoleDevice},
		{name: "pending cookie is device", remote: lanPeer, cookie: store.DevicePending, wantRole: RoleDevice},
		{name: "cookie beats loopback", remote: loopbackPeer, cookie: store.DeviceApproved, wantRole: RoleDevice},
		{name: "revoked cookie is cleared", remote: lanPeer, cookie: store.DeviceRevoked, wantRole: RoleNone, wantCleared: true},
		{name: "revoked cookie on loopback is cleared and owner", remote: loopbackPeer, cookie: store.DeviceRevoked, wantRole: RoleOwner, wantCleared: true},
		{name: "unknown cookie is cleared", remote: lanPeer, unknownCookie: true, wantRole: RoleNone, wantCleared: true},
		{name: "dev forwarded LAN address replaces loopback", isDev: true, remote: loopbackPeer, forwardedFor: []string{"192.168.77.7"}, wantRole: RoleNone, wantRemote: "192.168.77.7:51000"},
		{name: "dev uses the last forwarded hop", isDev: true, remote: loopbackPeer, forwardedFor: []string{"127.0.0.1, 192.168.77.7"}, wantRole: RoleNone, wantRemote: "192.168.77.7:51000"},
		{name: "dev uses the last forwarded header", isDev: true, remote: loopbackPeer, forwardedFor: []string{"127.0.0.1", "192.168.77.7"}, wantRole: RoleNone, wantRemote: "192.168.77.7:51000"},
		{name: "dev forwarded loopback stays owner", isDev: true, remote: loopbackPeer, forwardedFor: []string{"127.0.0.1"}, wantRole: RoleOwner},
		{name: "dev ignores malformed forwarded address", isDev: true, remote: loopbackPeer, forwardedFor: []string{"192.168.77.7, garbage"}, wantRole: RoleOwner, wantRemote: loopbackPeer},
		{name: "dev ignores forwarded address from a LAN peer", isDev: true, remote: lanPeer, forwardedFor: []string{"127.0.0.1"}, wantRole: RoleNone, wantRemote: lanPeer},
		{name: "forwarded address is ignored outside dev", remote: loopbackPeer, forwardedFor: []string{"192.168.77.7"}, wantRole: RoleOwner, wantRemote: loopbackPeer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthFixture(t, tc.isDev)
			r := newRequest(tc.remote)
			for _, hops := range tc.forwardedFor {
				r.Header.Add("X-Forwarded-For", hops)
			}
			var device store.Device
			if tc.cookie != "" {
				var token string
				device, token = f.insertDevice(tc.cookie)
				withDeviceCookie(r, token)
			}
			if tc.unknownCookie {
				withDeviceCookie(r, strings.Repeat("A", 43))
			}

			rec, seen := f.identify(r)
			if seen.principal.Role != tc.wantRole {
				t.Fatalf("role %v, want %v", seen.principal.Role, tc.wantRole)
			}
			if tc.wantRole == RoleDevice && (seen.principal.Device.ID != device.ID || seen.principal.Device.Status != tc.cookie) {
				t.Fatalf("device %+v, want %s %s", seen.principal.Device, device.ID, tc.cookie)
			}
			if tc.wantRemote != "" && seen.remote != tc.wantRemote {
				t.Fatalf("remote %q, want %q", seen.remote, tc.wantRemote)
			}
			if got := isCookieCleared(rec); got != tc.wantCleared {
				t.Fatalf("cookie cleared = %v, want %v (Set-Cookie %v)", got, tc.wantCleared, deviceCookies(rec))
			}
		})
	}
}

func TestIdentifyTouchesLastSeenAtMostOncePerMinute(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t, false)
	device, token := f.insertDevice(store.DeviceApproved)
	lastSeen := func() time.Time {
		stored, err := f.store.Device(t.Context(), device.ID)
		if err != nil {
			t.Fatalf("device: %v", err)
		}
		return stored.LastSeenAt
	}
	request := func() Principal {
		_, seen := f.identify(withDeviceCookie(newRequest(lanPeer), token))
		return seen.principal
	}

	firstSeen := f.clock.Now()
	if p := request(); !p.Device.LastSeenAt.Equal(firstSeen) {
		t.Fatalf("principal last seen %v, want %v", p.Device.LastSeenAt, firstSeen)
	}
	if got := lastSeen(); !got.Equal(firstSeen) {
		t.Fatalf("stored last seen %v, want %v", got, firstSeen)
	}

	f.clock.Advance(time.Minute - time.Millisecond)
	request()
	if got := lastSeen(); !got.Equal(firstSeen) {
		t.Fatalf("last seen moved to %v within a minute", got)
	}

	f.clock.Advance(time.Millisecond)
	request()
	if got := lastSeen(); !got.Equal(f.clock.Now()) {
		t.Fatalf("last seen %v after a minute, want %v", got, f.clock.Now())
	}
}

func TestIdentifyRenewsCookieOncePerDay(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t, false)
	_, token := f.insertDevice(store.DeviceApproved)
	renewal := func() []string {
		rec, _ := f.identify(withDeviceCookie(newRequest(lanPeer), token))
		return deviceCookies(rec)
	}

	if got := renewal(); len(got) != 1 || !strings.HasPrefix(got[0], CookieName+"="+token+";") || !strings.Contains(got[0], "Max-Age=31536000") {
		t.Fatalf("first request Set-Cookie %v, want a one-year renewal", got)
	}
	f.clock.Advance(24*time.Hour - time.Millisecond)
	if got := renewal(); len(got) != 0 {
		t.Fatalf("Set-Cookie %v within a day, want none", got)
	}
	f.clock.Advance(time.Millisecond)
	if got := renewal(); len(got) != 1 {
		t.Fatalf("Set-Cookie %v after a day, want a renewal", got)
	}
}

func TestIdentifyFailsClosedWhenStoreFails(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t, false)
	_, token := f.insertDevice(store.DeviceApproved)
	if err := f.store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	rec, seen := f.identify(withDeviceCookie(newRequest(loopbackPeer), token))
	if seen.principal.Role != RoleNone || seen.remote != "" {
		t.Fatal("handler ran although identification failed")
	}
	requireErrorResponse(t, rec, http.StatusInternalServerError, api.CodeInternal)
	if len(deviceCookies(rec)) != 0 {
		t.Fatal("cookie touched although identification failed")
	}
}

func TestRequireGuards(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t, false)
	none := Principal{}
	owner := Principal{Role: RoleOwner}
	pending := Principal{Role: RoleDevice, Device: store.Device{ID: "P", Status: store.DevicePending}}
	approved := Principal{Role: RoleDevice, Device: store.Device{ID: "A", Status: store.DeviceApproved}}
	type outcome struct {
		status int
		code   api.ErrorCode
	}
	pass := outcome{http.StatusNoContent, ""}
	unauthorized := outcome{http.StatusUnauthorized, api.CodeUnauthorized}
	forbidden := outcome{http.StatusForbidden, api.CodeForbidden}
	pendingApproval := outcome{http.StatusForbidden, api.CodePendingApproval}

	guards := []struct {
		name  string
		guard func(http.Handler) http.Handler
		want  map[string]outcome
	}{
		{"RequireOwner", f.auth.RequireOwner, map[string]outcome{"none": forbidden, "owner": pass, "pending": forbidden, "approved": forbidden}},
		{"RequireDevice", f.auth.RequireDevice, map[string]outcome{"none": unauthorized, "owner": forbidden, "pending": pass, "approved": pass}},
		{"RequireApprovedDevice", f.auth.RequireApprovedDevice, map[string]outcome{"none": unauthorized, "owner": forbidden, "pending": pendingApproval, "approved": pass}},
		{"RequireOwnerOrApprovedDevice", f.auth.RequireOwnerOrApprovedDevice, map[string]outcome{"none": unauthorized, "owner": pass, "pending": pendingApproval, "approved": pass}},
	}
	principals := map[string]Principal{"none": none, "owner": owner, "pending": pending, "approved": approved}
	for _, g := range guards {
		for principalName, p := range principals {
			t.Run(g.name+" "+principalName, func(t *testing.T) {
				t.Parallel()
				r := newRequest(lanPeer)
				r = r.WithContext(withPrincipal(r.Context(), p))
				rec := httptest.NewRecorder()
				g.guard(reached).ServeHTTP(rec, r)
				want := g.want[principalName]
				if want == pass {
					if rec.Code != http.StatusNoContent {
						t.Fatalf("status %d, want the request to pass", rec.Code)
					}
					return
				}
				requireErrorResponse(t, rec, want.status, want.code)
			})
		}
	}
}

func TestRevokedCookieOnGuardedRouteIsUnauthorizedAndCleared(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t, false)
	_, token := f.insertDevice(store.DeviceRevoked)
	rec := httptest.NewRecorder()
	handler := f.auth.Identify(f.auth.RequireOwnerOrApprovedDevice(reached))
	handler.ServeHTTP(rec, withDeviceCookie(newRequest(lanPeer), token))

	requireErrorResponse(t, rec, http.StatusUnauthorized, api.CodeUnauthorized)
	if !isCookieCleared(rec) {
		t.Fatalf("Set-Cookie %v, want the device cookie cleared", deviceCookies(rec))
	}
}
