package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/platform"
)

const (
	maxServerNameRunes = 64
	receivedDirPerm    = 0o750
)

func (s *server) settings(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.Settings())
}

func (s *server) patchSettings(w http.ResponseWriter, r *http.Request) {
	var patch api.SettingsPatch
	if err := readJSON(w, r, &patch); err != nil {
		s.rejectInvalid(w, r, err)
		return
	}
	s.applyPatch(w, r, patch)
}

func (s *server) pickReceivedDir(w http.ResponseWriter, r *http.Request) {
	dir, err := s.PickFolder(r.Context())
	switch {
	case errors.Is(err, platform.ErrUnsupported):
		s.writeError(w, http.StatusNotImplemented, api.CodeUnsupported, "no native folder picker")
	case err != nil:
		s.fail(w, r, err)
	case dir == "":
		s.writeJSON(w, http.StatusOK, s.Settings())
	default:
		s.applyPatch(w, r, api.SettingsPatch{ReceivedDir: &dir})
	}
}

func (s *server) applyPatch(w http.ResponseWriter, r *http.Request, patch api.SettingsPatch) {
	next, err := patched(s.Settings(), patch)
	if err != nil {
		s.rejectInvalid(w, r, err)
		return
	}
	err = s.ApplySettings(r.Context(), next)
	if errors.Is(err, platform.ErrUnsupported) {
		s.writeError(w, http.StatusNotImplemented, api.CodeUnsupported, "start at login is unsupported")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, s.Settings())
}

func patched(current api.Settings, patch api.SettingsPatch) (api.Settings, error) {
	next := current
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if length := utf8.RuneCountInString(name); length < 1 || length > maxServerNameRunes {
			return api.Settings{}, errors.New("name is not 1 to 64 characters")
		}
		next.Name = name
	}
	if patch.ReceivedDir != nil {
		dir, err := creatableDir(*patch.ReceivedDir)
		if err != nil {
			return api.Settings{}, err
		}
		next.ReceivedDir = dir
	}
	if patch.StartAtLogin != nil {
		next.StartAtLogin = *patch.StartAtLogin
	}
	if patch.CheckUpdates != nil {
		next.CheckUpdates = *patch.CheckUpdates
	}
	return next, nil
}

func creatableDir(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("received dir %q is not absolute", dir)
	}
	dir = filepath.Clean(dir)
	if err := os.MkdirAll(dir, receivedDirPerm); err != nil {
		return "", fmt.Errorf("create received dir %s: %w", dir, err)
	}
	return dir, nil
}
