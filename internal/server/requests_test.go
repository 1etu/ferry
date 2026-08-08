package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

type client struct {
	f            *fixture
	http         *http.Client
	forwardedFor string
	sealID       string
	key          [32]byte
	secret       []byte
	nonces       map[string][]byte
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func (f *fixture) owner() *client {
	return &client{f: f, http: f.server.Client()}
}

func (f *fixture) stranger() *client {
	return &client{f: f, http: f.server.Client(), forwardedFor: strangerIP}
}

func (f *fixture) newPeer() *client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		f.t.Fatal(err)
	}
	httpClient := *f.server.Client()
	httpClient.Jar = jar
	peer := "192.168.50." + strconv.Itoa(int(f.nextPeer.Add(1)))
	return &client{f: f, http: &httpClient, forwardedFor: peer}
}

func (f *fixture) pendingDevice() (*client, api.Device) {
	f.t.Helper()
	token := pairingToken(f.t, decode[api.Pairing](f.t, f.owner().send(http.MethodGet, "/api/pairing", nil)))
	device := f.newPeer()
	resp := device.send(http.MethodPost, "/api/pair", api.PairRequest{Token: token, Name: "iPhone"})
	if resp.status != http.StatusCreated {
		f.t.Fatalf("pair: %d %s", resp.status, resp.body)
	}
	return device, decode[api.Device](f.t, resp)
}

func (f *fixture) approvedDevice() (*client, api.Device) {
	f.t.Helper()
	device, d := f.pendingDevice()
	resp := f.owner().send(http.MethodPost, "/api/devices/"+d.ID+"/approve", nil)
	if resp.status != http.StatusOK {
		f.t.Fatalf("approve: %d %s", resp.status, resp.body)
	}
	if hs := device.handshake(); hs.status != http.StatusCreated {
		f.t.Fatalf("handshake: %d %s", hs.status, hs.body)
	}
	return device, decode[api.Device](f.t, resp)
}

func pairingToken(t *testing.T, p api.Pairing) string {
	t.Helper()
	u, err := url.Parse(p.QRURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("pair")
}

func (c *client) request(method, target string, body any) *http.Request {
	c.f.t.Helper()
	var reader io.Reader = http.NoBody
	switch b := body.(type) {
	case nil:
	case io.Reader:
		reader = b
	case string:
		reader = bytes.NewBufferString(b)
	default:
		encoded, err := json.Marshal(b)
		if err != nil {
			c.f.t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(c.f.t.Context(), method, c.f.server.URL+target, reader)
	if err != nil {
		c.f.t.Fatal(err)
	}
	if c.forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", c.forwardedFor)
	}
	if c.sealID != "" {
		req.Header.Set(sealHeader, c.sealID)
	}
	return req
}

func (c *client) do(req *http.Request) response {
	c.f.t.Helper()
	resp, err := c.http.Do(req)
	if err != nil {
		c.f.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.f.t.Fatal(err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: body}
}

func (c *client) send(method, target string, body any) response {
	c.f.t.Helper()
	return c.do(c.request(method, target, body))
}

func decode[T any](t *testing.T, resp response) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(resp.body, &v); err != nil {
		t.Fatalf("decode %T from %d %q: %v", v, resp.status, resp.body, err)
	}
	return v
}

func errorCode(t *testing.T, resp response) api.ErrorCode {
	t.Helper()
	if got := resp.header.Get("Content-Type"); got != jsonContentType {
		t.Fatalf("error content type %q, body %q", got, resp.body)
	}
	return decode[api.Error](t, resp).Error.Code
}

func expectStatus(t *testing.T, resp response, status int) {
	t.Helper()
	if resp.status != status {
		t.Fatalf("status %d, want %d, body %q", resp.status, status, resp.body)
	}
}

func expectError(t *testing.T, resp response, status int, code api.ErrorCode) {
	t.Helper()
	expectStatus(t, resp, status)
	if got := errorCode(t, resp); got != code {
		t.Fatalf("error code %q, want %q", got, code)
	}
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (f *fixture) insertTransfer(deviceID string, direction store.Direction, status store.TransferStatus) store.Transfer {
	f.t.Helper()
	now := time.UnixMilli(time.Now().UnixMilli()).UTC()
	t := store.Transfer{
		ID:        newID(),
		DeviceID:  deviceID,
		Direction: direction,
		Name:      "photo.jpg",
		Size:      100,
		Status:    status,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if status == store.TransferDone {
		t.Done = t.Size
		t.Path = filepath.Join(f.received, t.Name)
	}
	if err := f.store.InsertTransfer(f.t.Context(), t); err != nil {
		f.t.Fatal(err)
	}
	return t
}

func newID() string {
	return ulid.Make().String()
}
