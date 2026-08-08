package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type readFromRecorder struct {
	*httptest.ResponseRecorder
	readFromBytes int64
}

func (r *readFromRecorder) ReadFrom(src io.Reader) (int64, error) {
	n, err := io.Copy(r.ResponseRecorder, src)
	r.readFromBytes += n
	return n, err
}

func TestMiddlewareChainForwardsReadFromToTheConnection(t *testing.T) {
	t.Parallel()
	logs := &lockedBuffer{}
	f := newFixture(t, options{log: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	const body = "file body served without an intermediate buffer"
	handler := f.handler.chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		readerFrom, ok := w.(io.ReaderFrom)
		if !ok {
			t.Fatalf("writer %T hides io.ReaderFrom", w)
		}
		if _, err := readerFrom.ReadFrom(&io.LimitedReader{R: strings.NewReader(body), N: int64(len(body))}); err != nil {
			t.Errorf("read from: %v", err)
		}
	}))
	rec := &readFromRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	req.Host = "127.0.0.1:1"
	req.RemoteAddr = "127.0.0.1:50000"

	handler.ServeHTTP(rec, req)

	if rec.readFromBytes != int64(len(body)) || rec.Body.String() != body {
		t.Fatalf("connection read %d bytes via ReadFrom, body %q, want %d via ReadFrom", rec.readFromBytes, rec.Body.String(), len(body))
	}
	if !strings.Contains(logs.String(), "status=200") {
		t.Fatalf("request log %q lacks status=200", logs.String())
	}
}

func TestStatusWriterKeepsTheFirstStatusAcrossReadFrom(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		header     int
		wantStatus int
	}{
		{name: "read from without a header is 200", wantStatus: http.StatusOK},
		{name: "read from after a 206 stays 206", header: http.StatusPartialContent, wantStatus: http.StatusPartialContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sw := &statusWriter{ResponseWriter: &readFromRecorder{ResponseRecorder: httptest.NewRecorder()}}
			if tt.header != 0 {
				sw.WriteHeader(tt.header)
			}
			if _, err := sw.ReadFrom(strings.NewReader("x")); err != nil {
				t.Fatalf("read from: %v", err)
			}
			if got := sw.sentStatus(); got != tt.wantStatus {
				t.Fatalf("got status %d, want %d", got, tt.wantStatus)
			}
			if sw.Unwrap() == nil {
				t.Fatal("unwrap lost the underlying writer")
			}
		})
	}
}
