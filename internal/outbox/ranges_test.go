package outbox

import (
	"bytes"
	"net/http"
	"os"
	"testing"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

func TestServeRanges(t *testing.T) {
	t.Parallel()
	content := testContent(1000)
	tests := []struct {
		name         string
		rangeHeader  string
		wantStatus   int
		wantBody     []byte
		wantDone     int64
		wantStatusOf store.TransferStatus
	}{
		{
			name:         "leading slice records its end as progress",
			rangeHeader:  "bytes=0-199",
			wantStatus:   http.StatusPartialContent,
			wantBody:     content[:200],
			wantDone:     200,
			wantStatusOf: store.TransferActive,
		},
		{
			name:         "middle slice leaves the covered prefix at zero",
			rangeHeader:  "bytes=100-199",
			wantStatus:   http.StatusPartialContent,
			wantBody:     content[100:200],
			wantDone:     0,
			wantStatusOf: store.TransferActive,
		},
		{
			name:         "suffix range reaching the end does not complete the transfer",
			rangeHeader:  "bytes=-100",
			wantStatus:   http.StatusPartialContent,
			wantBody:     content[900:],
			wantDone:     0,
			wantStatusOf: store.TransferActive,
		},
		{
			name:         "open range from an offset does not complete the transfer",
			rangeHeader:  "bytes=500-",
			wantStatus:   http.StatusPartialContent,
			wantBody:     content[500:],
			wantDone:     0,
			wantStatusOf: store.TransferActive,
		},
		{
			name:         "unsatisfiable range serves nothing",
			rangeHeader:  "bytes=5000-6000",
			wantStatus:   http.StatusRequestedRangeNotSatisfiable,
			wantDone:     0,
			wantStatusOf: store.TransferActive,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newFixture(t)
			f := fx.offer(t, "data.bin", content)

			w := fx.serve(t, f.ID, testDeviceID, http.Header{"Range": {tt.rangeHeader}})

			if w.Code != tt.wantStatus {
				t.Fatalf("got status %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantBody != nil && !bytes.Equal(w.Body.Bytes(), tt.wantBody) {
				t.Fatalf("got %d body bytes, want %d", w.Body.Len(), len(tt.wantBody))
			}
			got := fx.onlyTransfer(t)
			if got.Done != tt.wantDone || got.Status != tt.wantStatusOf {
				t.Fatalf("got done %d status %s, want %d %s", got.Done, got.Status, tt.wantDone, tt.wantStatusOf)
			}
		})
	}
}

func TestServeProgressReachesDoneAcrossRanges(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	f := fx.offer(t, "movie.mov", testContent(1000))

	fx.serve(t, f.ID, testDeviceID, http.Header{"Range": {"bytes=0-599"}})
	first := fx.onlyTransfer(t)
	if first.Done != 600 || first.Status != store.TransferActive {
		t.Fatalf("got done %d status %s, want 600 active", first.Done, first.Status)
	}
	if first.Direction != store.DirectionOut || first.FileID != f.ID || first.DeviceID != testDeviceID || first.Size != 1000 {
		t.Fatalf("got transfer %+v, want an outgoing transfer of %s to the device", first, f.ID)
	}

	fx.serve(t, f.ID, testDeviceID, http.Header{"Range": {"bytes=600-"}})
	second := fx.onlyTransfer(t)
	if second.ID != first.ID || second.Done != 1000 || second.Status != store.TransferDone {
		t.Fatalf("got %+v, want %s done at 1000", second, first.ID)
	}

	published := fx.drain()
	last := published[len(published)-1]
	payload, ok := last.Payload.(api.Transfer)
	if !ok || last.DeviceID != testDeviceID || payload.Status != store.TransferDone || payload.Done != 1000 {
		t.Fatalf("got last event %+v, want the transfer done", last)
	}

	fx.serve(t, f.ID, testDeviceID, nil)
	if got := fx.transfers(t); len(got) != 2 || got[0].Status != store.TransferDone {
		t.Fatalf("got %+v, want a second finished transfer after the first completed", got)
	}
}

func TestIfRangeResumesOnlyAnUnchangedFile(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	content := testContent(1000)
	f := fx.offer(t, "movie.mov", content)
	etag := fx.serve(t, f.ID, "", nil).Header().Get("ETag")
	if etag == "" || etag[0] != '"' {
		t.Fatalf("got ETag %q, want a strong validator", etag)
	}

	resumed := fx.serve(t, f.ID, "", http.Header{"Range": {"bytes=500-"}, "If-Range": {etag}})
	if resumed.Code != http.StatusPartialContent || !bytes.Equal(resumed.Body.Bytes(), content[500:]) {
		t.Fatalf("got status %d with %d bytes, want 206 with the tail", resumed.Code, resumed.Body.Len())
	}

	changed := append(testContent(1000), 'x')
	if err := os.WriteFile(f.Path, changed, 0o600); err != nil {
		t.Fatalf("rewrite offered file: %v", err)
	}
	restarted := fx.serve(t, f.ID, "", http.Header{"Range": {"bytes=500-"}, "If-Range": {etag}})
	if restarted.Code != http.StatusOK || !bytes.Equal(restarted.Body.Bytes(), changed) {
		t.Fatalf("got status %d with %d bytes, want 200 with the whole changed file", restarted.Code, restarted.Body.Len())
	}
}
