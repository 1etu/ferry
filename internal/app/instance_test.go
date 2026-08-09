package app

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/1etu/ferry/internal/config"
)

func TestServeExitsWhenAnotherAppHoldsThePort(t *testing.T) {
	t.Parallel()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, `{"app":"other"}`); err != nil {
			t.Error(err)
		}
	}))
	defer other.Close()
	addr, ok := other.Listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("address %T", other.Listener.Addr())
	}
	dir := t.TempDir()
	writeConfig(t, dir, map[string]any{"receivedDir": t.TempDir(), "port": addr.Port})
	logs := &syncBuffer{}
	code := serve(t.Context(), config.Env{DataDir: dir, Headless: true}, dir, loopbackHost, slog.New(slog.NewTextHandler(logs, nil)), launch{})
	if code != exitFailure {
		t.Fatalf("exit %d, want %d", code, exitFailure)
	}
	if !strings.Contains(logs.String(), "listen failed") {
		t.Fatalf("logs %q lack the listen failure", logs.String())
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
