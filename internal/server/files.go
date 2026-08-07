package server

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/outbox"
	"github.com/1etu/ferry/internal/platform"
	"github.com/1etu/ferry/internal/store"
)

const maxOfferPaths = 500

func (s *server) files(w http.ResponseWriter, r *http.Request) {
	files, err := s.Store.Files(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	views := fileViews(files)
	if err := sealNames(r, views, func(f *api.OfferedFile) *string { return &f.Name }); err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, views)
}

func (s *server) offerFiles(w http.ResponseWriter, r *http.Request) {
	var req api.OfferRequest
	if err := readJSON(w, r, &req); err != nil {
		s.rejectInvalid(w, r, err)
		return
	}
	if err := validateOffer(req.Paths); err != nil {
		s.rejectInvalid(w, r, err)
		return
	}
	s.offer(w, r, req.Paths)
}

func validateOffer(paths []string) error {
	if len(paths) < 1 || len(paths) > maxOfferPaths {
		return fmt.Errorf("%d paths, want 1 to %d", len(paths), maxOfferPaths)
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("path %q is not absolute", path)
		}
	}
	return nil
}

func (s *server) pickFiles(w http.ResponseWriter, r *http.Request) {
	paths, err := s.Pick(r.Context())
	if errors.Is(err, platform.ErrUnsupported) {
		s.writeError(w, http.StatusNotImplemented, api.CodeUnsupported, "no native file picker")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(paths) == 0 {
		s.writeJSON(w, http.StatusCreated, []api.OfferedFile{})
		return
	}
	s.offer(w, r, paths)
}

func (s *server) offer(w http.ResponseWriter, r *http.Request, paths []string) {
	files, err := s.Outbox.Offer(r.Context(), paths)
	if errors.Is(err, outbox.ErrNotRegularFile) {
		s.Log.Warn("offer rejected, not a regular file", "err", err)
		s.writeError(w, http.StatusNotFound, api.CodeFileMissing, "file missing")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, fileViews(files))
}

func fileViews(files []store.File) []api.OfferedFile {
	views := make([]api.OfferedFile, 0, len(files))
	for _, f := range files {
		views = append(views, api.FileFrom(f))
	}
	return views
}

func (s *server) removeFile(w http.ResponseWriter, r *http.Request) {
	err := s.Outbox.Remove(r.Context(), r.PathValue("fileId"))
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

func (s *server) downloadFile(w http.ResponseWriter, r *http.Request) {
	s.Outbox.Serve(w, r, r.PathValue("fileId"))
}
