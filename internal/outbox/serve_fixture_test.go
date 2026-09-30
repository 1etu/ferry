package outbox

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/1etu/ferry/internal/store"
)

func testContent(size int) []byte {
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i % 251)
	}
	return content
}

func contentRequest(t *testing.T, fileID string, header http.Header) *http.Request {
	t.Helper()
	ctx := withDevice(t.Context(), testDeviceID)
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/files/"+fileID+"/content", http.NoBody)
	for key, values := range header {
		r.Header[key] = values
	}
	return r
}

func (fx fixture) serve(t *testing.T, fileID string, header http.Header) {
	t.Helper()
	fx.outbox.Serve(httptest.NewRecorder(), contentRequest(t, fileID, header), fileID)
}

func (fx fixture) transfers(t *testing.T) []store.Transfer {
	t.Helper()
	all, err := fx.store.Transfers(t.Context(), "", 100)
	if err != nil {
		t.Fatalf("list transfers: %v", err)
	}
	return all
}

func (fx fixture) onlyTransfer(t *testing.T) store.Transfer {
	t.Helper()
	all := fx.transfers(t)
	if len(all) != 1 {
		t.Fatalf("got %d transfers, want 1", len(all))
	}
	return all[0]
}
