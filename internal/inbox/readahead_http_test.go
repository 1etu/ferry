package inbox

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/1etu/ferry/internal/seal"
)

const (
	fourFrames        = 4 * seal.FrameSize
	secondFrameTagEnd = 2*(seal.FrameSize+frameOverhead) - 1
)

func TestTamperedFrameWithIntactFramesBehindItWritesOnlyTheVerifiedPrefix(t *testing.T) {
	t.Parallel()
	f := newSealedFixture(t, nil)
	content := randomBytes(t, fourFrames)
	id, target := f.create(t, deviceOne, "behind.bin", fourFrames)
	frames := f.clients[deviceOne].frames(t, f.nonces[id], 0, content)
	frames[secondFrameTagEnd] ^= 0x01

	if resp := f.patchRaw(t, deviceOne, target, 0, frames); resp.status != http.StatusBadRequest {
		t.Fatalf("tampered PATCH: %d %q, want 400", resp.status, resp.body)
	}
	if head := f.head(t, target); head.header.Get("Upload-Offset") != strconv.Itoa(seal.FrameSize) {
		t.Fatalf("offset after the tampered frame %q, want %d", head.header.Get("Upload-Offset"), seal.FrameSize)
	}
	partial, err := os.ReadFile(filepath.Join(f.incoming, id))
	if err != nil || !bytes.Equal(partial, content[:seal.FrameSize]) {
		t.Fatalf("partial file is not exactly the verified prefix: %d bytes, %v", len(partial), err)
	}
	if resp := f.patch(t, deviceOne, target, seal.FrameSize, content[seal.FrameSize:]); resp.status != http.StatusNoContent {
		t.Fatalf("resume with intact frames: %d %q", resp.status, resp.body)
	}
	f.requireReceived(t, "behind.bin", content)
}

func TestSealedRequestsLeaveNoReadAheadGoroutineBehind(t *testing.T) {
	f := newSealedFixture(t, nil)
	before := drainGoroutines()
	content := randomBytes(t, fourFrames)
	if resp, _, _ := f.createWithBody(t, deviceOne, "whole.bin", fourFrames, content); resp.status != http.StatusCreated {
		t.Fatalf("creation with upload: %d %q", resp.status, resp.body)
	}
	f.requireReceived(t, "whole.bin", content)

	id, target := f.create(t, deviceOne, "cut.bin", fourFrames)
	frames := f.clients[deviceOne].frames(t, f.nonces[id], 0, content)
	frames[secondFrameTagEnd] ^= 0x01
	if resp := f.patchRaw(t, deviceOne, target, 0, frames); resp.status != http.StatusBadRequest {
		t.Fatalf("tampered PATCH: %d %q, want 400", resp.status, resp.body)
	}
	rest := f.clients[deviceOne].frames(t, f.nonces[id], seal.FrameSize, content[seal.FrameSize:])
	headers := map[string]string{"Upload-Offset": strconv.Itoa(seal.FrameSize)}
	cutInsideTheThirdFrame := seal.FrameSize + frameOverhead + seal.FrameSize/2
	if status := f.cut(t, http.MethodPatch, target, headers, int64(len(rest)), rest[:cutInsideTheThirdFrame]); status != http.StatusBadRequest {
		t.Fatalf("cut PATCH: %d, want 400", status)
	}
	if head := f.head(t, target); head.header.Get("Upload-Offset") != strconv.Itoa(2*seal.FrameSize) {
		t.Fatalf("offset after the cut %q, want %d", head.header.Get("Upload-Offset"), 2*seal.FrameSize)
	}
	waitFor(t, "read-ahead goroutines to exit", func() bool { return drainGoroutines() == before })
}
