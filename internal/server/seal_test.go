package server

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/store"
)

func (c *client) handshake() response {
	c.f.t.Helper()
	sc, err := seal.NewClient()
	if err != nil {
		c.f.t.Fatal(err)
	}
	req := api.SealRequest{ClientKey: base64.RawURLEncoding.EncodeToString(sc.Public)}
	if c.secret != nil {
		req.Proof = base64.RawURLEncoding.EncodeToString(sc.Proof(c.secret))
	}
	resp := c.send(http.MethodPost, "/api/seal", req)
	if resp.status != http.StatusCreated {
		return resp
	}
	answer := decode[api.SealResponse](c.f.t, resp)
	key, derived, err := sc.Finish(c.secret, decode64(c.f.t, answer.ServerKey), decode64(c.f.t, answer.Confirm))
	if err != nil {
		c.f.t.Fatalf("finish handshake: %v", err)
	}
	c.key, c.sealID = key, answer.SessionID
	if derived != nil {
		c.secret = derived
	}
	return resp
}

func (c *client) open(t *testing.T, sealed string) string {
	t.Helper()
	plain, err := seal.OpenString(&c.key, sealed)
	if err != nil {
		t.Fatalf("open sealed name %q: %v", sealed, err)
	}
	return plain
}

func decode64(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	return raw
}

func (f *fixture) qrPairedDevice() *client {
	f.t.Helper()
	pairing := decode[api.Pairing](f.t, f.owner().send(http.MethodGet, "/api/pairing", nil))
	qr, err := url.Parse(pairing.QRURL)
	if err != nil {
		f.t.Fatal(err)
	}
	fragment, ok := strings.CutPrefix(qr.Fragment, "s=")
	if !ok {
		f.t.Fatalf("qr url %q has no secret fragment", pairing.QRURL)
	}
	device := f.newPeer()
	if device.secret, err = seal.DeviceSecret(decode64(f.t, fragment)); err != nil {
		f.t.Fatal(err)
	}
	resp := device.send(http.MethodPost, "/api/pair", api.PairRequest{Token: qr.Query().Get("pair"), Name: "iPhone", HasSecret: true})
	expectStatus(f.t, resp, http.StatusCreated)
	expectStatus(f.t, f.owner().send(http.MethodPost, "/api/devices/"+decode[api.Device](f.t, resp).ID+"/approve", nil), http.StatusOK)
	return device
}

func TestFirstUseHandshakeStoresTheSecretAndLaterRequiresProof(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, d := f.approvedDevice()
	stored, err := f.store.Device(t.Context(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(device.secret) != 32 || !bytes.Equal(stored.Secret, device.secret) {
		t.Fatalf("stored secret %x, client secret %x", stored.Secret, device.secret)
	}
	expectStatus(t, device.handshake(), http.StatusCreated)
	thief := *device
	thief.secret = nil
	expectError(t, thief.handshake(), http.StatusForbidden, api.CodeSealInvalid)
}

func TestQRPairedHandshakeNeedsTheFragmentSecret(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device := f.qrPairedDevice()
	secret := device.secret
	expectStatus(t, device.handshake(), http.StatusCreated)
	if !bytes.Equal(device.secret, secret) {
		t.Fatal("a QR-paired handshake derived a new secret")
	}
	cookieOnly := *device
	cookieOnly.secret = nil
	expectError(t, cookieOnly.handshake(), http.StatusForbidden, api.CodeSealInvalid)
	wrong := *device
	wrong.secret = bytes.Repeat([]byte{1}, 32)
	expectError(t, wrong.handshake(), http.StatusForbidden, api.CodeSealInvalid)
}

func TestHandshakeIsForApprovedDevicesWithAValidKey(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	pending, _ := f.pendingDevice()
	approved, _ := f.approvedDevice()
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	tests := []struct {
		name   string
		client *client
		body   any
		status int
		code   api.ErrorCode
	}{
		{"owner", f.owner(), api.SealRequest{ClientKey: key}, http.StatusForbidden, api.CodeForbidden},
		{"stranger", f.stranger(), api.SealRequest{ClientKey: key}, http.StatusUnauthorized, api.CodeUnauthorized},
		{"pending device", pending, api.SealRequest{ClientKey: key}, http.StatusForbidden, api.CodePendingApproval},
		{"short key", approved, api.SealRequest{ClientKey: "AAAA"}, http.StatusBadRequest, api.CodeInvalidRequest},
		{"key not base64url", approved, api.SealRequest{ClientKey: "+/+/"}, http.StatusBadRequest, api.CodeInvalidRequest},
		{"unknown field", approved, `{"clientKey":"` + key + `","extra":1}`, http.StatusBadRequest, api.CodeInvalidRequest},
	}
	for _, tc := range tests {
		resp := tc.client.send(http.MethodPost, "/api/seal", tc.body)
		if resp.status != tc.status || errorCode(t, resp) != tc.code {
			t.Errorf("%s: %d %s, want %d %s", tc.name, resp.status, resp.body, tc.status, tc.code)
		}
	}
}

func TestSealedRoutesAnswerSealExpiredWithoutALiveSession(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, _ := f.approvedDevice()
	missing := *device
	missing.sealID = ""
	stale := *device
	stale.sealID = "AAAAAAAAAAAAAAAAAAAAAA"
	targets := []struct{ method, target string }{
		{http.MethodGet, "/api/transfers"},
		{http.MethodGet, "/api/files"},
		{http.MethodGet, "/api/events"},
		{http.MethodGet, "/api/events?seal=" + stale.sealID},
		{http.MethodPost, "/api/uploads/"},
		{http.MethodHead, "/api/uploads/" + newID()},
	}
	for _, c := range []*client{&missing, &stale} {
		for _, tc := range targets {
			req := c.request(tc.method, tc.target, nil)
			req.Header.Set("Tus-Resumable", "1.0.0")
			if resp := c.do(req); resp.status != http.StatusForbidden || (tc.method != http.MethodHead && errorCode(t, resp) != api.CodeSealExpired) {
				t.Errorf("%s %s with seal %q: %d %s", tc.method, tc.target, c.sealID, resp.status, resp.body)
			}
		}
	}
	expectStatus(t, missing.send(http.MethodDelete, "/api/transfers", nil), http.StatusNoContent)
	expectStatus(t, f.owner().send(http.MethodGet, "/api/transfers", nil), http.StatusOK)
	pending, _ := f.pendingDevice()
	f.openStream(pending, "/api/events")
}

func TestRevokeDropsTheSealedSession(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, d := f.approvedDevice()
	if _, ok := f.sessions.Lookup(d.ID, device.sealID); !ok {
		t.Fatal("no live session after the handshake")
	}
	expectStatus(t, f.owner().send(http.MethodDelete, "/api/devices/"+d.ID, nil), http.StatusNoContent)
	if _, ok := f.sessions.Lookup(d.ID, device.sealID); ok {
		t.Fatal("session survived the revoke")
	}
}

func TestDeviceTransferListSealsNames(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, d := f.approvedDevice()
	f.insertTransfer(d.ID, store.DirectionIn, store.TransferDone)
	listed := decode[[]api.Transfer](t, device.send(http.MethodGet, "/api/transfers", nil))
	if len(listed) != 1 || listed[0].Name == "photo.jpg" || device.open(t, listed[0].Name) != "photo.jpg" {
		t.Fatalf("device sees %+v", listed)
	}
	if owned := decode[[]api.Transfer](t, f.owner().send(http.MethodGet, "/api/transfers", nil)); owned[0].Name != "photo.jpg" {
		t.Fatalf("owner sees %+v", owned)
	}
}
