package server

import (
	"bytes"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

const (
	indexFile           = "index.html"
	manifestFile        = "manifest.webmanifest"
	manifestContentType = "application/manifest+json"
	assetsPrefix        = "assets/"
	immutableCache      = "public, max-age=31536000, immutable"
	revalidateCache     = "no-cache"
	noStoreCache        = "no-store"
)

func (s *server) manifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", manifestContentType)
	s.serveEmbedded(w, r, manifestFile, noStoreCache)
}

func (s *server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.notFound(w)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	switch {
	case strings.HasPrefix(name, assetsPrefix):
		if !s.isEmbeddedFile(name) {
			s.notFound(w)
			return
		}
		s.serveEmbedded(w, r, name, immutableCache)
	case s.isEmbeddedFile(name):
		s.serveEmbedded(w, r, name, revalidateCache)
	default:
		s.serveEmbedded(w, r, indexFile, revalidateCache)
	}
}

func (s *server) isEmbeddedFile(name string) bool {
	if !fs.ValidPath(name) || name == "." {
		return false
	}
	info, err := fs.Stat(s.WebUI, name)
	return err == nil && info.Mode().IsRegular()
}

func (s *server) serveEmbedded(w http.ResponseWriter, r *http.Request, name, cacheControl string) {
	content, err := fs.ReadFile(s.WebUI, name)
	if errors.Is(err, fs.ErrNotExist) {
		s.notFound(w)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", cacheControl)
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(content))
}

func (s *server) apiNotFound(w http.ResponseWriter, _ *http.Request) {
	s.notFound(w)
}
