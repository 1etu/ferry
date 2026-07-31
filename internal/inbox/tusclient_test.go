package inbox

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/seal"
)

const (
	testNameHeader = "X-Test-Name"
	frameOverhead  = 20
)

type sealedClient struct {
	sessionID string
	key       [32]byte
}

func (c sealedClient) metadata(t *testing.T, name string, nonce []byte) string {
	t.Helper()
	sealed, err := seal.SealString(&c.key, name)
	if err != nil {
		t.Fatal(err)
	}
	return "name " + standard64(sealed) + ",nonce " + standard64(base64.RawURLEncoding.EncodeToString(nonce))
}

func (c sealedClient) frames(t *testing.T, nonce []byte, offset int64, plain []byte) []byte {
	t.Helper()
	var body []byte
	index := uint64(offset / seal.FrameSize)
	for start := 0; start < len(plain); start += seal.FrameSize {
		frame, err := seal.SealFrame(&c.key, nonce, index, plain[start:min(start+seal.FrameSize, len(plain))])
		if err != nil {
			t.Fatal(err)
		}
		body = append(body, frame...)
		index++
	}
	return body
}

func standard64(text string) string {
	return base64.StdEncoding.EncodeToString([]byte(text))
}

func sealedLength(plain int64) int64 {
	return plain + (plain+seal.FrameSize-1)/seal.FrameSize*frameOverhead
}

func newNonce(t *testing.T) []byte {
	t.Helper()
	return randomBytes(t, seal.UploadNonceSize)
}

func createHeaders(name string, size int64) map[string]string {
	return map[string]string{"Upload-Length": strconv.FormatInt(size, 10), testNameHeader: name}
}

func (f *fixture) request(t *testing.T, method, target, device string, body io.Reader, headers map[string]string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, f.server.URL+target, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Tus-Resumable", "1.0.0")
	client, hasSession := f.clients[device]
	if device != "" {
		req.Header.Set(deviceHeader, device)
	}
	if hasSession {
		req.Header.Set(sealHeader, client.sessionID)
	}
	for key, value := range headers {
		if key == testNameHeader {
			req.Header.Set("Upload-Metadata", client.metadata(t, value, newNonce(t)))
			continue
		}
		req.Header.Set(key, value)
	}
	return req
}

func (f *fixture) try(req *http.Request) (response, error) {
	resp, err := f.server.Client().Do(req)
	if err != nil {
		return response{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return response{}, err
	}
	return response{status: resp.StatusCode, header: resp.Header, body: string(body)}, nil
}

func (f *fixture) send(t *testing.T, req *http.Request) response {
	t.Helper()
	resp, err := f.try(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	return resp
}

func (f *fixture) uploadHeaders(t *testing.T, device, name string, size int64, nonce []byte) map[string]string {
	t.Helper()
	return map[string]string{
		"Upload-Length":   strconv.FormatInt(size, 10),
		"Upload-Metadata": f.clients[device].metadata(t, name, nonce),
	}
}

func (f *fixture) create(t *testing.T, device, name string, size int64) (id, target string) {
	t.Helper()
	nonce := newNonce(t)
	resp := f.send(t, f.request(t, http.MethodPost, "/api/uploads/", device, http.NoBody, f.uploadHeaders(t, device, name, size, nonce)))
	if resp.status != http.StatusCreated {
		t.Fatalf("create: status %d body %q", resp.status, resp.body)
	}
	return f.remember(t, resp, nonce)
}

func (f *fixture) createWithBody(t *testing.T, device, name string, size int64, plain []byte) (resp response, id, target string) {
	t.Helper()
	nonce := newNonce(t)
	headers := f.uploadHeaders(t, device, name, size, nonce)
	headers["Content-Type"] = tusContentType
	body := bytes.NewReader(f.clients[device].frames(t, nonce, 0, plain))
	resp = f.send(t, f.request(t, http.MethodPost, "/api/uploads/", device, body, headers))
	if resp.status != http.StatusCreated {
		return resp, "", ""
	}
	id, target = f.remember(t, resp, nonce)
	return resp, id, target
}

func (f *fixture) remember(t *testing.T, resp response, nonce []byte) (id, target string) {
	t.Helper()
	location, err := url.Parse(resp.header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	id = path.Base(location.Path)
	f.nonces[id] = nonce
	return id, location.Path
}

func (f *fixture) patch(t *testing.T, device, target string, offset int64, plain []byte) response {
	t.Helper()
	body := f.clients[device].frames(t, f.nonces[path.Base(target)], offset, plain)
	return f.patchRaw(t, device, target, offset, body)
}

func (f *fixture) patchRaw(t *testing.T, device, target string, offset int64, body []byte) response {
	t.Helper()
	headers := map[string]string{"Upload-Offset": strconv.FormatInt(offset, 10), "Content-Type": tusContentType}
	return f.send(t, f.request(t, http.MethodPatch, target, device, bytes.NewReader(body), headers))
}

func (f *fixture) head(t *testing.T, target string) response {
	t.Helper()
	return f.send(t, f.request(t, http.MethodHead, target, deviceOne, http.NoBody, nil))
}

func (f *fixture) cut(t *testing.T, method, target string, headers map[string]string, declared int64, body []byte) int {
	t.Helper()
	serverURL, err := url.Parse(f.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", serverURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var head strings.Builder
	fmt.Fprintf(&head, "%s %s HTTP/1.1\r\nHost: %s\r\nTus-Resumable: 1.0.0\r\n%s: %s\r\n%s: %s\r\nContent-Type: %s\r\nContent-Length: %d\r\n",
		method, target, serverURL.Host, deviceHeader, deviceOne, sealHeader, f.clients[deviceOne].sessionID, tusContentType, declared)
	for key, value := range headers {
		fmt.Fprintf(&head, "%s: %s\r\n", key, value)
	}
	if _, err := io.WriteString(conn, head.String()+"\r\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(body); err != nil {
		t.Fatal(err)
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		t.Fatalf("connection is %T, want TCP", conn)
	}
	if err := tcp.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response of cut %s: %v", method, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

type runningPatch struct {
	client sealedClient
	nonce  []byte
	index  uint64
	body   *io.PipeWriter
	done   <-chan response
	err    <-chan error
}

func (f *fixture) startPatch(t *testing.T, target string, declared int64) *runningPatch {
	t.Helper()
	headers := map[string]string{"Upload-Offset": "0", "Content-Type": tusContentType}
	return f.startUpload(t, http.MethodPatch, target, headers, declared, f.nonces[path.Base(target)])
}

func (f *fixture) startUpload(t *testing.T, method, target string, headers map[string]string, declared int64, nonce []byte) *runningPatch {
	t.Helper()
	reader, writer := io.Pipe()
	req := f.request(t, method, target, deviceOne, reader, headers)
	req.ContentLength = sealedLength(declared)
	done := make(chan response, 1)
	errs := make(chan error, 1)
	go func() {
		resp, err := f.try(req)
		if err != nil {
			errs <- err
			return
		}
		done <- resp
	}()
	t.Cleanup(func() {
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	})
	return &runningPatch{client: f.clients[deviceOne], nonce: nonce, body: writer, done: done, err: errs}
}

func (p *runningPatch) write(t *testing.T, chunk []byte) {
	t.Helper()
	frame, err := seal.SealFrame(&p.client.key, p.nonce, p.index, chunk)
	if err != nil {
		t.Fatal(err)
	}
	p.index++
	if _, err := p.body.Write(frame); err != nil {
		t.Fatalf("write to running request: %v", err)
	}
}

func (p *runningPatch) response(t *testing.T) response {
	t.Helper()
	select {
	case resp := <-p.done:
		return resp
	case err := <-p.err:
		t.Fatalf("running request failed: %v", err)
	case <-time.After(waitTimeout):
		t.Fatal("running request did not finish")
	}
	return response{}
}

func (p *runningPatch) finish(t *testing.T) {
	t.Helper()
	if err := p.body.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
	case <-p.err:
	case <-time.After(waitTimeout):
		t.Fatal("running request did not finish")
	}
}
