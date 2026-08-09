package server

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/seal"
)

const frameOverhead = 20

func (c *client) uploadMetadata(name string, nonce []byte) string {
	c.f.t.Helper()
	sealed, err := seal.SealString(&c.key, name)
	if err != nil {
		c.f.t.Fatal(err)
	}
	return "name " + standard64(sealed) + ",nonce " + standard64(base64.RawURLEncoding.EncodeToString(nonce))
}

func standard64(text string) string {
	return base64.StdEncoding.EncodeToString([]byte(text))
}

func (c *client) frames(nonce []byte, offset int64, plain []byte) []byte {
	c.f.t.Helper()
	var body []byte
	index := uint64(offset / seal.FrameSize)
	for start := 0; start < len(plain); start += seal.FrameSize {
		frame, err := seal.SealFrame(&c.key, nonce, index, plain[start:min(start+seal.FrameSize, len(plain))])
		if err != nil {
			c.f.t.Fatal(err)
		}
		body = append(body, frame...)
		index++
	}
	return body
}

func (c *client) createRequest(name string, size int64, body []byte) (*http.Request, []byte) {
	c.f.t.Helper()
	nonce := make([]byte, seal.UploadNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		c.f.t.Fatal(err)
	}
	var req *http.Request
	if body == nil {
		req = c.request(http.MethodPost, "/api/uploads/", nil)
	} else {
		req = c.request(http.MethodPost, "/api/uploads/", bytes.NewReader(c.frames(nonce, 0, body)))
		req.Header.Set("Content-Type", tusContentType)
	}
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Upload-Length", strconv.FormatInt(size, 10))
	req.Header.Set("Upload-Metadata", c.uploadMetadata(name, nonce))
	return req, nonce
}

func (f *fixture) createUpload(device *client, name string, size int64) (id, target string) {
	f.t.Helper()
	req, nonce := device.createRequest(name, size, nil)
	resp := device.do(req)
	expectStatus(f.t, resp, http.StatusCreated)
	return device.remember(resp, nonce)
}

func (c *client) remember(resp response, nonce []byte) (id, target string) {
	c.f.t.Helper()
	location, err := url.Parse(resp.header.Get("Location"))
	if err != nil {
		c.f.t.Fatal(err)
	}
	if c.nonces == nil {
		c.nonces = map[string][]byte{}
	}
	id = path.Base(location.Path)
	c.nonces[id] = nonce
	return id, location.Path
}

func patchRequest(device *client, target string, offset int64, plain []byte) *http.Request {
	body := device.frames(device.nonces[path.Base(target)], offset, plain)
	req := device.request(http.MethodPatch, target, bytes.NewReader(body))
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
	req.Header.Set("Content-Type", tusContentType)
	return req
}

type runningPatch struct {
	key   [32]byte
	nonce []byte
	index uint64
	body  *io.PipeWriter
	done  <-chan response
}

func (f *fixture) startPatch(device *client, target string, size int64) *runningPatch {
	f.t.Helper()
	reader, writer := io.Pipe()
	req := device.request(http.MethodPatch, target, reader)
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Upload-Offset", "0")
	req.Header.Set("Content-Type", tusContentType)
	req.ContentLength = size + (size+seal.FrameSize-1)/seal.FrameSize*frameOverhead
	done := make(chan response, 1)
	go func() {
		resp, err := device.http.Do(req)
		if err != nil {
			done <- response{}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			done <- response{}
			return
		}
		done <- response{status: resp.StatusCode, header: resp.Header, body: body}
	}()
	f.t.Cleanup(func() {
		if err := writer.Close(); err != nil {
			f.t.Error(err)
		}
	})
	return &runningPatch{key: device.key, nonce: device.nonces[path.Base(target)], body: writer, done: done}
}

func (p *runningPatch) write(t *testing.T, plain []byte) {
	t.Helper()
	frame, err := seal.SealFrame(&p.key, p.nonce, p.index, plain)
	if err != nil {
		t.Fatal(err)
	}
	p.index++
	if _, err := p.body.Write(frame); err != nil {
		t.Fatal(err)
	}
}

func (p *runningPatch) response(t *testing.T) response {
	t.Helper()
	select {
	case resp := <-p.done:
		return resp
	case <-time.After(waitTimeout):
		t.Fatal("PATCH did not finish")
	}
	return response{}
}

func TestCreationWithUploadThroughTheServer(t *testing.T) {
	t.Parallel()
	f := newFixture(t, options{})
	device, _ := f.approvedDevice()
	content := []byte("small enough for the creation request")
	req, _ := device.createRequest("small.txt", int64(len(content)), content)
	resp := device.do(req)
	expectStatus(t, resp, http.StatusCreated)
	if resp.header.Get("Upload-Offset") != strconv.Itoa(len(content)) {
		t.Fatalf("offset %q", resp.header.Get("Upload-Offset"))
	}
	got, err := os.ReadFile(filepath.Join(f.received, "small.txt"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("received %q, %v", got, err)
	}
}
