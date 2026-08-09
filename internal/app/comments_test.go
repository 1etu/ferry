package app

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRepositoryHasNoComments(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	checker := filepath.Join(root, "tools", "check-comments.mjs")
	output, err := exec.CommandContext(t.Context(), node, checker, root).CombinedOutput()
	if err != nil {
		t.Fatalf("check-comments failed: %v\n%s", err, output)
	}
}
