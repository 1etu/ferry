package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

func TestPairRejectsMalformedRequests(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	token := pairingToken(t, decode[api.Pairing](t, f.owner().send(http.MethodGet, "/api/pairing", nil)))
	tests := []struct {
		name string
		body any
	}{
		{"token and code", api.PairRequest{Token: token, Code: "123456", Name: "iPhone"}},
		{"neither token nor code", api.PairRequest{Name: "iPhone"}},
		{"code of five digits", api.PairRequest{Code: "12345", Name: "iPhone"}},
		{"code with letters", api.PairRequest{Code: "12a456", Name: "iPhone"}},
		{"blank name", api.PairRequest{Token: token, Name: "   "}},
		{"name of 65 characters", api.PairRequest{Token: token, Name: strings.Repeat("a", 65)}},
		{"unknown field", `{"token":"x","name":"iPhone","extra":1}`},
		{"not json", `token=x`},
		{"empty body", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expectError(t, f.stranger().send(http.MethodPost, "/api/pair", tc.body), http.StatusBadRequest, api.CodeInvalidRequest)
		})
	}
}

func TestPairAcceptsSixtyFourCharacterNamesAndTrimsThem(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	token := pairingToken(t, decode[api.Pairing](t, f.owner().send(http.MethodGet, "/api/pairing", nil)))
	name := strings.Repeat("é", 64)
	resp := f.newPeer().send(http.MethodPost, "/api/pair", api.PairRequest{Token: token, Name: "  " + name + " "})
	expectStatus(t, resp, http.StatusCreated)
	if got := decode[api.Device](t, resp).Name; got != name {
		t.Fatalf("name %q, want %q", got, name)
	}
}

func TestPairByTokenSetsCookieAndAwaitsApproval(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	token := pairingToken(t, decode[api.Pairing](t, f.owner().send(http.MethodGet, "/api/pairing", nil)))
	device := f.newPeer()
	resp := device.send(http.MethodPost, "/api/pair", api.PairRequest{Token: token, Name: "iPhone"})
	expectStatus(t, resp, http.StatusCreated)
	if cookie := resp.header.Get("Set-Cookie"); !strings.HasPrefix(cookie, "ferry_device=") || !strings.Contains(cookie, "HttpOnly") {
		t.Fatalf("Set-Cookie %q", cookie)
	}
	d := decode[api.Device](t, resp)
	if d.Status != store.DevicePending {
		t.Fatalf("status %q", d.Status)
	}
	expectError(t, device.send(http.MethodGet, "/api/files", nil), http.StatusForbidden, api.CodePendingApproval)

	approved := decode[api.Device](t, f.owner().send(http.MethodPost, "/api/devices/"+d.ID+"/approve", nil))
	if approved.Status != store.DeviceApproved {
		t.Fatalf("approved status %q", approved.Status)
	}
	expectError(t, device.send(http.MethodGet, "/api/files", nil), http.StatusForbidden, api.CodeSealExpired)
	expectStatus(t, device.handshake(), http.StatusCreated)
	expectStatus(t, device.send(http.MethodGet, "/api/files", nil), http.StatusOK)
	devices := decode[[]api.Device](t, f.owner().send(http.MethodGet, "/api/devices", nil))
	if len(devices) != 1 || devices[0].ID != d.ID {
		t.Fatalf("devices %+v", devices)
	}
}

func TestPairWithUnknownCodeIsInvalid(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	code := decode[api.Pairing](t, f.owner().send(http.MethodGet, "/api/pairing", nil)).Code
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	resp := f.newPeer().send(http.MethodPost, "/api/pair", api.PairRequest{Code: wrong, Name: "iPhone"})
	expectError(t, resp, http.StatusNotFound, api.CodePairingInvalid)
	expectStatus(t, f.newPeer().send(http.MethodPost, "/api/pair", api.PairRequest{Code: code, Name: "iPhone"}), http.StatusCreated)
}

func TestPairRateLimitAnswersRetryAfter(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	attacker := f.newPeer()
	var resp response
	for range 11 {
		resp = attacker.send(http.MethodPost, "/api/pair", api.PairRequest{Token: "not-a-token", Name: "iPhone"})
	}
	expectError(t, resp, http.StatusTooManyRequests, api.CodeRateLimited)
	if got := resp.header.Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After %q", got)
	}
}

func TestApproveUnknownOrRevokedDeviceIsNotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	_, d := f.pendingDevice()
	expectStatus(t, f.owner().send(http.MethodDelete, "/api/devices/"+d.ID, nil), http.StatusNoContent)
	expectError(t, f.owner().send(http.MethodPost, "/api/devices/"+d.ID+"/approve", nil), http.StatusNotFound, api.CodeNotFound)
	expectError(t, f.owner().send(http.MethodPost, "/api/devices/"+newID()+"/approve", nil), http.StatusNotFound, api.CodeNotFound)
}

func TestRotatePairingReplacesTheCode(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	first := decode[api.Pairing](t, f.owner().send(http.MethodGet, "/api/pairing", nil))
	rotated := decode[api.Pairing](t, f.owner().send(http.MethodPost, "/api/pairing", nil))
	if rotated.QRURL == first.QRURL {
		t.Fatal("rotation kept the token")
	}
	if !strings.HasPrefix(rotated.QRURL, "http://"+testLANIP+":") || !strings.HasPrefix(rotated.LocalURL, "http://testpc.local:") {
		t.Fatalf("pairing urls %+v", rotated)
	}
}

func TestDeviceMayOnlyRevokeItself(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	first, firstDevice := f.approvedDevice()
	second, secondDevice := f.pendingDevice()

	expectError(t, f.stranger().send(http.MethodDelete, "/api/devices/"+firstDevice.ID, nil), http.StatusUnauthorized, api.CodeUnauthorized)
	expectError(t, second.send(http.MethodDelete, "/api/devices/"+firstDevice.ID, nil), http.StatusForbidden, api.CodeForbidden)

	resp := second.send(http.MethodDelete, "/api/devices/"+secondDevice.ID, nil)
	expectStatus(t, resp, http.StatusNoContent)
	if cookie := resp.header.Get("Set-Cookie"); !strings.HasPrefix(cookie, "ferry_device=;") || !strings.Contains(cookie, "Max-Age=0") {
		t.Fatalf("Set-Cookie %q", cookie)
	}
	if role := decode[api.Session](t, second.send(http.MethodGet, "/api/session", nil)).Role; role != api.RoleNone {
		t.Fatalf("role after self-revoke %q", role)
	}

	expectStatus(t, f.owner().send(http.MethodDelete, "/api/devices/"+firstDevice.ID, nil), http.StatusNoContent)
	expectError(t, first.send(http.MethodGet, "/api/files", nil), http.StatusUnauthorized, api.CodeUnauthorized)
	expectError(t, f.owner().send(http.MethodDelete, "/api/devices/"+newID(), nil), http.StatusNotFound, api.CodeNotFound)
}

func TestRevokeCancelsTheDevicesUploads(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, d := f.approvedDevice()
	id, _ := f.createUpload(device, "video.mov", 1000)
	expectStatus(t, f.owner().send(http.MethodDelete, "/api/devices/"+d.ID, nil), http.StatusNoContent)
	tr, err := f.store.Transfer(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Status != store.TransferCanceled {
		t.Fatalf("upload of revoked device is %q", tr.Status)
	}
}
