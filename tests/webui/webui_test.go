package webui_test

import (
	"io/fs"
	"testing"

	"github.com/1etu/ferry/internal/webui"
	"github.com/1etu/ferry/tests/kit"
)

func TestFSContainsIndexHTML(t *testing.T) {
	t.Parallel()
	info, err := fs.Stat(webui.FS(), "index.html")
	kit.NoError(t, err, "stat index.html")
	if info.Size() == 0 {
		t.Fatal("index.html is empty")
	}
}
