package app

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/seal"
)

const (
	sealHeader     = "X-Ferry-Seal"
	tusContentType = "application/offset+octet-stream"
	frameOverhead  = 20
)

type device struct {
	l      *lifecycle
	http   *http.Client
	secret []byte
	sealID string
	key    [32]byte
}

func (l *lifecycle) pairDevice() *device {
	l.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		l.t.Fatal(err)
	}
	d := &device{l: l, http: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
	var pairing api.Pairing
	l.callJSON(l.owner, http.MethodGet, "/api/pairing", nil, http.StatusOK, &pairing)
	qr, err := url.Parse(pairing.QRURL)
	if err != nil {
		l.t.Fatal(err)
	}
	fragment, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(qr.Fragment, "s="))
	if err != nil {
		l.t.Fatal(err)
	}
	if d.secret, err = seal.DeviceSecret(fragment); err != nil {
		l.t.Fatal(err)
	}
	var paired api.Device
	l.callJSON(d.http, http.MethodPost, "/api/pair", api.PairRequest{Token: qr.Query().Get("pair"), Name: "iPhone", HasSecret: true}, http.StatusCreated, &paired)
	l.callJSON(l.owner, http.MethodPost, "/api/devices/"+paired.ID+"/approve", nil, http.StatusOK, nil)
	d.handshake()
	return d
}

func (d *device) handshake() {
	d.l.t.Helper()
	client, err := seal.NewClient()
	if err != nil {
		d.l.t.Fatal(err)
	}
	var answer api.SealResponse
	request := api.SealRequest{
		ClientKey: base64.RawURLEncoding.EncodeToString(client.Public),
		Proof:     base64.RawURLEncoding.EncodeToString(client.Proof(d.secret)),
	}
	d.l.callJSON(d.http, http.MethodPost, "/api/seal", request, http.StatusCreated, &answer)
	serverKey, err := base64.RawURLEncoding.DecodeString(answer.ServerKey)
	if err != nil {
		d.l.t.Fatal(err)
	}
	confirm, err := base64.RawURLEncoding.DecodeString(answer.Confirm)
	if err != nil {
		d.l.t.Fatal(err)
	}
	if d.key, _, err = client.Finish(d.secret, serverKey, confirm); err != nil {
		d.l.t.Fatal(err)
	}
	d.sealID = answer.SessionID
}

func (d *device) tus(method, target string, body io.Reader, header map[string]string) *http.Response {
	d.l.t.Helper()
	full := map[string]string{"Tus-Resumable": "1.0.0", sealHeader: d.sealID}
	for key, value := range header {
		full[key] = value
	}
	return d.l.call(d.http, method, target, body, full)
}

func (d *device) create(name string, size int64) (target string, nonce []byte) {
	d.l.t.Helper()
	nonce = make([]byte, seal.UploadNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		d.l.t.Fatal(err)
	}
	sealed, err := seal.SealString(&d.key, name)
	if err != nil {
		d.l.t.Fatal(err)
	}
	metadata := "name " + base64.StdEncoding.EncodeToString([]byte(sealed)) +
		",nonce " + base64.StdEncoding.EncodeToString([]byte(base64.RawURLEncoding.EncodeToString(nonce)))
	resp := d.tus(http.MethodPost, "/api/uploads/", http.NoBody, map[string]string{
		"Upload-Length":   strconv.FormatInt(size, 10),
		"Upload-Metadata": metadata,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		d.l.t.Fatalf("create: %d", resp.StatusCode)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		d.l.t.Fatal(err)
	}
	return location.Path, nonce
}

func (d *device) frames(nonce []byte, offset int64, plain []byte) []byte {
	d.l.t.Helper()
	var body []byte
	index := uint64(offset / seal.FrameSize)
	for start := 0; start < len(plain); start += seal.FrameSize {
		frame, err := seal.SealFrame(&d.key, nonce, index, plain[start:min(start+seal.FrameSize, len(plain))])
		if err != nil {
			d.l.t.Fatal(err)
		}
		body = append(body, frame...)
		index++
	}
	return body
}

func (d *device) patch(target string, nonce []byte, offset int64, plain []byte) int {
	d.l.t.Helper()
	resp := d.tus(http.MethodPatch, target, bytes.NewReader(d.frames(nonce, offset, plain)), map[string]string{
		"Upload-Offset": strconv.FormatInt(offset, 10),
		"Content-Type":  tusContentType,
	})
	resp.Body.Close()
	return resp.StatusCode
}

func (d *device) offset(target string) string {
	d.l.t.Helper()
	resp := d.tus(http.MethodHead, target, http.NoBody, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		d.l.t.Fatalf("HEAD %s: %d", target, resp.StatusCode)
	}
	return resp.Header.Get("Upload-Offset")
}

func (d *device) startPatch(target string, nonce []byte, declared int64, first []byte) <-chan int {
	d.l.t.Helper()
	reader, writer := io.Pipe()
	req, err := http.NewRequestWithContext(d.l.t.Context(), http.MethodPatch, d.l.baseURL+target, reader)
	if err != nil {
		d.l.t.Fatal(err)
	}
	req.ContentLength = declared + (declared+seal.FrameSize-1)/seal.FrameSize*frameOverhead
	for key, value := range map[string]string{"Tus-Resumable": "1.0.0", sealHeader: d.sealID, "Upload-Offset": "0", "Content-Type": tusContentType} {
		req.Header.Set(key, value)
	}
	patched := make(chan int, 1)
	go func() {
		resp, err := d.http.Do(req)
		if err != nil {
			patched <- 0
			return
		}
		resp.Body.Close()
		patched <- resp.StatusCode
	}()
	go func() {
		if _, err := writer.Write(d.frames(nonce, 0, first)); err != nil {
			d.l.t.Error(err)
		}
	}()
	d.l.t.Cleanup(func() {
		if err := writer.Close(); err != nil {
			d.l.t.Error(err)
		}
	})
	return patched
}

func uploadID(target string) string {
	return path.Base(target)
}
