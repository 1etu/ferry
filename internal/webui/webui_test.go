package webui

import (
	"io/fs"
	"testing"
)

func TestFSContainsIndexHTML(t *testing.T) {
	t.Parallel()
	info, err := fs.Stat(FS(), "index.html")
	if err != nil {
		t.Fatalf("stat index.html: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("index.html is empty")
	}
}
