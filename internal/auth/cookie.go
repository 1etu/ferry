package auth

import (
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	CookieName = "ferry_device"

	cookieMaxAge        = 365 * 24 * time.Hour
	cookieRenewInterval = 24 * time.Hour
)

func (a *Auth) SetCookie(w http.ResponseWriter, token string) {
	//nolint:gosec
	replaceDeviceCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(cookieMaxAge / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *Auth) ClearCookie(w http.ResponseWriter) {
	//nolint:gosec
	replaceDeviceCookie(w, &http.Cookie{
		Name:     CookieName,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func replaceDeviceCookie(w http.ResponseWriter, c *http.Cookie) {
	header := w.Header()
	others := slices.DeleteFunc(slices.Clone(header.Values("Set-Cookie")), func(v string) bool {
		return strings.HasPrefix(v, CookieName+"=")
	})
	header["Set-Cookie"] = append(others, c.String())
}
