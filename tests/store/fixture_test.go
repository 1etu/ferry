package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/store"
	"github.com/1etu/ferry/tests/kit"
)

var epoch = time.UnixMilli(1_790_000_000_000).UTC()

func at(minutes int) time.Time {
	return epoch.Add(time.Duration(minutes) * time.Minute)
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	return openStoreAt(t, filepath.Join(t.TempDir(), "ferry.db"))
}

func openStoreAt(t *testing.T, path string) *store.Store {
	t.Helper()
	s, err := store.Open(t.Context(), path)
	kit.NoError(t, err, "open store")
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return s
}

func file(id string, createdAt time.Time) store.File {
	return store.File{
		ID:        id,
		Path:      `C:\Users\owner\` + id + ".mov",
		Name:      id + ".mov",
		Size:      2048,
		ModTime:   at(-60),
		CreatedAt: createdAt,
	}
}

func mustInsertFiles(t *testing.T, s *store.Store, files ...store.File) {
	t.Helper()
	for _, f := range files {
		kit.NoError(t, s.InsertFile(t.Context(), f))
	}
}

func fileIDs(files []store.File) []string {
	ids := make([]string, 0, len(files))
	for _, f := range files {
		ids = append(ids, f.ID)
	}
	return ids
}
