package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	cdnHost         = "objects.githubusercontent.com"
	manifestTimeout = 30 * time.Second
	maxRedirects    = 5
)

func redirectGuardedClient(base *http.Client, check func(*http.Request, []*http.Request) error) *http.Client {
	client := http.Client{}
	if base != nil {
		client = *base
	}
	client.CheckRedirect = check
	return &client
}

func (u *Updater) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("too many redirects")
	}
	sameOrigin := req.URL.Scheme == u.manifestURL.Scheme && req.URL.Host == u.manifestURL.Host
	isCDN := req.URL.Scheme == "https" && req.URL.Host == cdnHost
	if !sameOrigin && !isCDN {
		return fmt.Errorf("redirect to %s://%s refused", req.URL.Scheme, req.URL.Host)
	}
	return nil
}

type fetched struct {
	manifest    Manifest
	version     Version
	etag        string
	isUnchanged bool
}

func (u *Updater) fetchManifest(ctx context.Context, etag string) (fetched, error) {
	ctx, cancel := context.WithTimeout(ctx, manifestTimeout)
	defer cancel()
	manifestURL := u.manifestURL.String()
	manifest, err := u.get(ctx, manifestURL, etag, maxManifestBytes)
	if err != nil {
		return fetched{}, err
	}
	if manifest.isUnchanged {
		return fetched{isUnchanged: true}, nil
	}
	sig, err := u.get(ctx, strings.TrimSuffix(manifestURL, ".json")+".sig", "", maxSignatureBytes)
	if err != nil {
		return fetched{}, err
	}
	m, v, err := parseSignedManifest(manifest.body, sig.body, u.key)
	if err != nil {
		return fetched{}, err
	}
	return fetched{manifest: m, version: v, etag: manifest.etag}, nil
}

type response struct {
	body        []byte
	etag        string
	isUnchanged bool
}

func (u *Updater) get(ctx context.Context, rawURL, etag string, limit int64) (response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return response{}, fmt.Errorf("request %s: %w", rawURL, err)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("get %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	switch {
	case etag != "" && resp.StatusCode == http.StatusNotModified:
		return response{isUnchanged: true}, nil
	case resp.StatusCode != http.StatusOK:
		return response{}, fmt.Errorf("get %s: status %d", rawURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return response{}, fmt.Errorf("read %s: %w", rawURL, err)
	}
	if int64(len(body)) > limit {
		return response{}, fmt.Errorf("read %s: larger than %d bytes", rawURL, limit)
	}
	return response{body: body, etag: resp.Header.Get("ETag")}, nil
}
