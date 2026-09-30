package repo_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/1etu/ferry/tests/kit"
)

func TestRepositoryHasNoComments(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	root := kit.RepoRoot(t)
	checker := filepath.Join(root, "tools", "check-comments.mjs")
	output, err := exec.CommandContext(t.Context(), node, checker, root).CombinedOutput()
	if err != nil {
		t.Fatalf("check-comments failed: %v\n%s", err, output)
	}
}
