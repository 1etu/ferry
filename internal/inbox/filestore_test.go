package inbox

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/tus/tusd/v2/pkg/handler"

	"github.com/1etu/ferry/internal/seal"
)

func requireUploadInfo(t *testing.T, f *fixture, id string, offset int64, name string) {
	t.Helper()
	upload, err := f.in.files.GetUpload(t.Context(), id)
	if err != nil {
		t.Fatalf("get upload %s: %v", id, err)
	}
	info, err := upload.GetInfo(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != id || info.Offset != offset || info.Size != 100 || info.MetaData[metadataName] != name || uploadNonce(info.MetaData) == nil {
		t.Fatalf("info %+v, want %s at offset %d named %s with a nonce", info, id, offset, name)
	}
}

func TestGetUploadOfALiveUploadIsServedFromTheRegistry(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	id, target := f.create(t, deviceOne, "live.bin", 100)
	if resp := f.patch(t, deviceOne, target, 0, []byte("0123456789")); resp.status != http.StatusNoContent {
		t.Fatalf("patch: %d %q", resp.status, resp.body)
	}
	if err := os.Remove(filepath.Join(f.incoming, id+infoSuffix)); err != nil {
		t.Fatal(err)
	}

	requireUploadInfo(t, f, id, 10, "live.bin")
	if head := f.head(t, target); head.status != http.StatusOK || head.header.Get("Upload-Offset") != "10" {
		t.Fatalf("HEAD without the info file on disk: %d offset %q", head.status, head.header.Get("Upload-Offset"))
	}
}

func TestGetUploadOfAnUploadThatIsNotLiveReadsTheDisk(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	id, target := f.create(t, deviceOne, "stored.bin", 100)
	if resp := f.patch(t, deviceOne, target, 0, []byte("0123456789")); resp.status != http.StatusNoContent {
		t.Fatalf("patch: %d %q", resp.status, resp.body)
	}
	f.in.untrack(id)

	requireUploadInfo(t, f, id, 10, "stored.bin")
	if _, err := f.in.files.GetUpload(t.Context(), "01J9ZK0X5S8V7Q2M3N4P5R6T7W"); !errors.Is(err, handler.ErrNotFound) {
		t.Fatalf("unknown upload: got %v, want ErrNotFound", err)
	}
}

func TestRecoveredUploadLoadsItsInfoFromDiskOnceAndResumesSealed(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(cfg *Config) { cfg.MaxUploadBytes = 2 * seal.FrameSize })
	content := randomBytes(t, seal.FrameSize+100)
	id, target := f.create(t, deviceOne, "recovered.bin", int64(len(content)))
	if resp := f.patch(t, deviceOne, target, 0, content[:seal.FrameSize]); resp.status != http.StatusNoContent {
		t.Fatalf("patch: %d %q", resp.status, resp.body)
	}
	f.in.untrack(id)
	if err := f.in.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if done, ok := f.in.Progress(id); !ok || done != seal.FrameSize {
		t.Fatalf("recovered progress %d, %v; want %d", done, ok, seal.FrameSize)
	}
	if err := os.Remove(filepath.Join(f.incoming, id+infoSuffix)); err != nil {
		t.Fatal(err)
	}

	if resp := f.patch(t, deviceOne, target, seal.FrameSize, content[seal.FrameSize:]); resp.status != http.StatusNoContent {
		t.Fatalf("resume of the recovered upload: %d %q", resp.status, resp.body)
	}
	f.requireReceived(t, "recovered.bin", content)
}
