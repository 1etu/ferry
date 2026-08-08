package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/platform"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOfferValidatesPaths(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	existing := writeFile(t, "a.txt", "a")
	tooMany := make([]string, maxOfferPaths+1)
	for i := range tooMany {
		tooMany[i] = existing
	}
	tests := []struct {
		name   string
		paths  []string
		status int
		code   api.ErrorCode
	}{
		{"no paths", nil, http.StatusBadRequest, api.CodeInvalidRequest},
		{"relative path", []string{"a.txt"}, http.StatusBadRequest, api.CodeInvalidRequest},
		{"501 paths", tooMany, http.StatusBadRequest, api.CodeInvalidRequest},
		{"missing file", []string{filepath.Join(t.TempDir(), "gone.txt")}, http.StatusNotFound, api.CodeFileMissing},
		{"directory", []string{t.TempDir()}, http.StatusNotFound, api.CodeFileMissing},
	}
	for _, tc := range tests {
		resp := f.owner().send(http.MethodPost, "/api/files", api.OfferRequest{Paths: tc.paths})
		if resp.status != tc.status || errorCode(t, resp) != tc.code {
			t.Errorf("%s: %d %s", tc.name, resp.status, resp.body)
		}
	}
}

func TestOfferedFileDownloadsWithRangeAndCanBeRemoved(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, _ := f.approvedDevice()
	source := writeFile(t, "report.pdf", "0123456789")
	resp := f.owner().send(http.MethodPost, "/api/files", api.OfferRequest{Paths: []string{source}})
	expectStatus(t, resp, http.StatusCreated)
	offered := decode[[]api.OfferedFile](t, resp)
	if len(offered) != 1 || offered[0].Name != "report.pdf" || offered[0].Size != 10 {
		t.Fatalf("offered %+v", offered)
	}
	listed := decode[[]api.OfferedFile](t, device.send(http.MethodGet, "/api/files", nil))
	if len(listed) != 1 || listed[0].ID != offered[0].ID || device.open(t, listed[0].Name) != "report.pdf" {
		t.Fatalf("listed %+v", listed)
	}

	req := device.request(http.MethodGet, "/api/files/"+offered[0].ID+"/content", nil)
	req.Header.Set("Range", "bytes=4-")
	ranged := device.do(req)
	expectStatus(t, ranged, http.StatusPartialContent)
	if string(ranged.body) != "456789" {
		t.Fatalf("range body %q", ranged.body)
	}
	if got := ranged.header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Fatalf("Content-Disposition %q", got)
	}
	if ranged.header.Get("Content-Security-Policy") == "" {
		t.Fatal("download lacks the global security headers")
	}

	expectStatus(t, device.send(http.MethodDelete, "/api/files/"+offered[0].ID, nil), http.StatusNoContent)
	expectError(t, device.send(http.MethodDelete, "/api/files/"+offered[0].ID, nil), http.StatusNotFound, api.CodeNotFound)
}

func TestPickFiles(t *testing.T) {
	t.Parallel()
	picked := writeFile(t, "picked.mov", "movie")
	tests := []struct {
		name   string
		pick   func(context.Context) ([]string, error)
		status int
		count  int
	}{
		{"unsupported", func(context.Context) ([]string, error) { return nil, platform.ErrUnsupported }, http.StatusNotImplemented, 0},
		{"canceled dialog", func(context.Context) ([]string, error) { return nil, nil }, http.StatusCreated, 0},
		{"chosen file", func(context.Context) ([]string, error) { return []string{picked}, nil }, http.StatusCreated, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, options{pick: tc.pick})
			resp := f.owner().send(http.MethodPost, "/api/files/pick", nil)
			expectStatus(t, resp, tc.status)
			if tc.status == http.StatusNotImplemented {
				if code := errorCode(t, resp); code != api.CodeUnsupported {
					t.Fatalf("code %q", code)
				}
				return
			}
			if got := decode[[]api.OfferedFile](t, resp); len(got) != tc.count {
				t.Fatalf("offered %+v", got)
			}
		})
	}
}

func TestEventStreamsSealNamesForDevicesOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, _ := f.approvedDevice()
	deviceLines := f.openStream(device, "/api/events?seal="+device.sealID)
	ownerLines := f.openStream(f.owner(), "/api/events")
	source := writeFile(t, "song.m4a", "music")
	expectStatus(t, f.owner().send(http.MethodPost, "/api/files", api.OfferRequest{Paths: []string{source}}), http.StatusCreated)

	expectLine(t, deviceLines, "event: file")
	sealed := decodeData[api.FileChange](t, <-deviceLines)
	if sealed.Action != api.FileAdded || sealed.File.Name == "song.m4a" || device.open(t, sealed.File.Name) != "song.m4a" {
		t.Fatalf("device event %+v", sealed)
	}
	expectLine(t, ownerLines, "event: file")
	if plain := decodeData[api.FileChange](t, <-ownerLines); plain.File.Name != "song.m4a" {
		t.Fatalf("owner event %+v", plain)
	}
}

func (f *fixture) openStream(c *client, target string) <-chan string {
	f.t.Helper()
	resp, err := c.http.Do(c.request(http.MethodGet, target, nil))
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		f.t.Fatalf("stream %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	lines := make(chan string, 64)
	go scanLines(resp.Body, lines)
	expectLine(f.t, lines, "retry: 2000")
	return lines
}

func decodeData[T any](t *testing.T, line string) T {
	t.Helper()
	data, ok := strings.CutPrefix(line, "data: ")
	if !ok {
		t.Fatalf("line %q is not data", line)
	}
	var v T
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		t.Fatalf("decode %q: %v", data, err)
	}
	return v
}

func scanLines(r io.Reader, lines chan<- string) {
	defer close(lines)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines <- scanner.Text()
	}
}

func expectLine(t *testing.T, lines <-chan string, want string) {
	t.Helper()
	timeout := time.After(waitTimeout)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("stream ended before %q", want)
			}
			if line == want {
				return
			}
		case <-timeout:
			t.Fatalf("no %q on the stream", want)
		}
	}
}
