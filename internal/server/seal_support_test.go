package server

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/seal"
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

func decode64(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	return raw
}
