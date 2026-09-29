package kit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func NoError(tb testing.TB, err error, action ...string) {
	tb.Helper()
	if err == nil {
		return
	}
	if len(action) > 0 {
		tb.Fatalf("%s: %v", action[0], err)
	}
	tb.Fatalf("unexpected error: %v", err)
}

func Equal[T any](tb testing.TB, got, want T, opts ...cmp.Option) {
	tb.Helper()
	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func RepoRoot(tb testing.TB) string {
	tb.Helper()
	dir, err := os.Getwd()
	NoError(tb, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
