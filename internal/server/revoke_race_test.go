package server

import (
	"net/http"
	"sync"
	"testing"
)

func TestHandshakeRacingARevokeLeavesNoSession(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	for range 20 {
		device, d := f.approvedDevice()
		previous := device.sealID
		owner := f.owner()
		var wg sync.WaitGroup
		wg.Go(func() { owner.send(http.MethodDelete, "/api/devices/"+d.ID, nil) })
		device.handshake()
		wg.Wait()
		for _, id := range []string{previous, device.sealID} {
			if _, ok := f.sessions.Lookup(d.ID, id); ok {
				t.Fatalf("session %q of revoked device %s is live", id, d.ID)
			}
		}
		if device.send(http.MethodGet, "/api/session", nil).status != http.StatusOK {
			t.Fatal("revoked device cannot read the public session route")
		}
	}
}
