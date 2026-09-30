package auth_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/1etu/ferry/internal/auth"
)

func TestSetCookieAttributes(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t, 8080, false)
	rec := httptest.NewRecorder()
	f.auth.SetCookie(rec, "tok")
	want := auth.CookieName + "=tok; Path=/; Max-Age=31536000; HttpOnly; SameSite=Lax"
	if got := deviceCookies(rec); len(got) != 1 || got[0] != want {
		t.Fatalf("Set-Cookie %v, want %q", got, want)
	}

	cleared := httptest.NewRecorder()
	f.auth.ClearCookie(cleared)
	wantCleared := auth.CookieName + "=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax"
	if got := deviceCookies(cleared); len(got) != 1 || got[0] != wantCleared {
		t.Fatalf("Set-Cookie %v, want %q", got, wantCleared)
	}
}

func TestSetCookieReplacesEarlierDeviceCookieOnly(t *testing.T) {
	t.Parallel()
	f := newAuthFixture(t, 8080, false)
	rec := httptest.NewRecorder()
	rec.Header().Add("Set-Cookie", "other=1; Path=/")
	f.auth.ClearCookie(rec)
	f.auth.SetCookie(rec, "fresh")

	all := rec.Result().Header.Values("Set-Cookie")
	if len(all) != 2 || all[0] != "other=1; Path=/" || !strings.HasPrefix(all[1], auth.CookieName+"=fresh;") {
		t.Fatalf("Set-Cookie %v, want the other cookie and one fresh device cookie", all)
	}
}
