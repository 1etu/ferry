package install

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveDataDirKeepsWhatFerryDidNotCreate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		extra     []string
		isDirGone bool
	}{
		{"only ferry's entries", nil, true},
		{"received folder inside the data dir", []string{filepath.Join("Received", "photo.heic"), filepath.Join("Received", ".incoming", "01J")}, false},
		{"stray file", []string{"notes.txt"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), "Ferry")
			for _, name := range []string{"config.json", "ferry.db", "ferry.db-wal", "ferry.db-shm", "update.json", filepath.Join("logs", "ferry.log"), filepath.Join("webview", "EBWebView", "Local State")} {
				writeTestFile(t, filepath.Join(dir, name))
			}
			for _, name := range tc.extra {
				writeTestFile(t, filepath.Join(dir, name))
			}
			if err := removeDataDir(t.Context(), dir); err != nil {
				t.Fatalf("remove data dir: %v", err)
			}
			for _, name := range dataDirEntries {
				if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("%s survived: %v", name, err)
				}
			}
			for _, name := range tc.extra {
				if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
					t.Fatalf("%s was removed: %v", name, err)
				}
			}
			if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) != tc.isDirGone {
				t.Fatalf("data dir gone %v, want %v", errors.Is(err, fs.ErrNotExist), tc.isDirGone)
			}
		})
	}
}

func TestRemoveDataDirOfAMissingDirSucceeds(t *testing.T) {
	t.Parallel()
	if err := removeDataDir(t.Context(), filepath.Join(t.TempDir(), "never-created")); err != nil {
		t.Fatalf("remove missing data dir: %v", err)
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}
