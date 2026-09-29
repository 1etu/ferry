package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/store"
)

const (
	tusVersion     = "1.0.0"
	tusContentType = "application/offset+octet-stream"
	forwardedFor   = "192.168.77.7"
	sealHeader     = "X-Ferry-Seal"
	uploadsPath    = "/api/uploads/"
	sealAheadDepth = 4
)

type device struct {
	server  *server
	cookie  string
	session string
	key     [32]byte
}

func runUpload(ctx context.Context, s *server, d *device, o options) (result, error) {
	plain := pattern(o.chunk)
	name := "bench-upload.bin"
	time.Sleep(o.settle)
	start := time.Now()
	latencies, err := d.upload(ctx, name, o.size, plain, o.chunk)
	elapsed := time.Since(start)
	if err != nil {
		return result{}, err
	}
	if o.verify {
		if err := verifyReceived(filepath.Join(s.received, name), plain, o.size); err != nil {
			return result{}, err
		}
	}
	return result{elapsed: elapsed, bytes: o.size, latencies: latencies}, nil
}

func (d *device) upload(ctx context.Context, name string, size int64, plain []byte, chunk int64) ([]time.Duration, error) {
	nonce := randomBytes(seal.UploadNonceSize)
	ahead := sealAhead(ctx, &d.key, nonce, plain, size, chunk)
	var latencies []time.Duration
	var location string
	for future := range ahead {
		next := <-future
		began := time.Now()
		var err error
		if location == "" {
			location, err = d.create(ctx, name, size, nonce, next)
		} else {
			err = d.patch(ctx, location, next)
		}
		if err != nil {
			return nil, err
		}
		latencies = append(latencies, time.Since(began))
	}
	return latencies, nil
}

func (d *device) create(ctx context.Context, name string, size int64, nonce []byte, first sealedChunk) (string, error) {
	sealedName, err := seal.SealString(&d.key, name)
	if err != nil {
		return "", err
	}
	metadata := "name " + base64.StdEncoding.EncodeToString([]byte(sealedName)) +
		",nonce " + base64.StdEncoding.EncodeToString([]byte(base64.RawURLEncoding.EncodeToString(nonce))) +
		",filetype " + base64.StdEncoding.EncodeToString([]byte("application/octet-stream"))
	req, err := d.request(ctx, http.MethodPost, uploadsPath, first.body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Upload-Length", strconv.FormatInt(size, 10))
	req.Header.Set("Upload-Metadata", metadata)
	resp, err := d.server.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("create upload: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", unexpected(resp, "create upload")
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		return "", fmt.Errorf("upload location: %w", err)
	}
	return location.Path, expectOffset(resp, first.end)
}

func (d *device) patch(ctx context.Context, location string, chunk sealedChunk) error {
	req, err := d.request(ctx, http.MethodPatch, location, chunk.body)
	if err != nil {
		return err
	}
	offset := strconv.FormatInt(chunk.start, 10)
	req.Header.Set("Upload-Offset", offset)
	resp, err := d.server.client.Do(req)
	if err != nil {
		return fmt.Errorf("patch at %s: %w", offset, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return unexpected(resp, "patch at "+offset)
	}
	return expectOffset(resp, chunk.end)
}

func (d *device) request(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.server.url+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Tus-Resumable", tusVersion)
	req.Header.Set("Content-Type", tusContentType)
	req.Header.Set("Cookie", auth.CookieName+"="+d.cookie)
	req.Header.Set("X-Forwarded-For", forwardedFor)
	req.Header.Set(sealHeader, d.session)
	return req, nil
}

func expectOffset(resp *http.Response, want int64) error {
	got := resp.Header.Get("Upload-Offset")
	if got != strconv.FormatInt(want, 10) {
		return fmt.Errorf("server offset %s, want %d", got, want)
	}
	return nil
}

func pair(ctx context.Context, s *server) (*device, error) {
	var pairing api.Pairing
	if err := s.owner(ctx, http.MethodGet, "/api/pairing", nil, &pairing); err != nil {
		return nil, err
	}
	body, err := json.Marshal(api.PairRequest{Code: pairing.Code, Name: "Bench"})
	if err != nil {
		return nil, err
	}
	resp, err := s.post(ctx, "/api/pair", body, http.Header{"X-Forwarded-For": {forwardedFor}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var paired api.Device
	if err := decode(resp, http.StatusCreated, &paired, "pair"); err != nil {
		return nil, err
	}
	d := &device{server: s}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == auth.CookieName {
			d.cookie = cookie.Value
		}
	}
	if paired.Status != store.DeviceApproved {
		if err := s.owner(ctx, http.MethodPost, "/api/devices/"+paired.ID+"/approve", nil, nil); err != nil {
			return nil, err
		}
	}
	if err := d.handshake(ctx); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *device) handshake(ctx context.Context) error {
	client, err := seal.NewClient()
	if err != nil {
		return err
	}
	body, err := json.Marshal(api.SealRequest{ClientKey: base64.RawURLEncoding.EncodeToString(client.Public)})
	if err != nil {
		return err
	}
	header := http.Header{"X-Forwarded-For": {forwardedFor}, "Cookie": {auth.CookieName + "=" + d.cookie}}
	resp, err := d.server.post(ctx, "/api/seal", body, header)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var sealed api.SealResponse
	if err := decode(resp, http.StatusCreated, &sealed, "seal handshake"); err != nil {
		return err
	}
	serverKey, err := base64.RawURLEncoding.DecodeString(sealed.ServerKey)
	if err != nil {
		return fmt.Errorf("decode server key: %w", err)
	}
	confirm, err := base64.RawURLEncoding.DecodeString(sealed.Confirm)
	if err != nil {
		return fmt.Errorf("decode confirm: %w", err)
	}
	d.key, _, err = client.Finish(nil, serverKey, confirm)
	d.session = sealed.SessionID
	return err
}

func (s *server) post(ctx context.Context, path string, body []byte, header http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = header
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post %s: %w", path, err)
	}
	return resp, nil
}

func (s *server) owner(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader = http.NoBody
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.url+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices {
		return unexpected(resp, method+" "+path)
	}
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return decode(resp, resp.StatusCode, out, method+" "+path)
}

func decode(resp *http.Response, status int, out any, what string) error {
	if resp.StatusCode != status {
		return unexpected(resp, what)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decode reply: %w", what, err)
	}
	return nil
}

func unexpected(resp *http.Response, what string) error {
	detail, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("%s: status %d, body unreadable: %w", what, resp.StatusCode, err)
	}
	return fmt.Errorf("%s: status %d: %s", what, resp.StatusCode, bytes.TrimSpace(detail))
}
