package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/api"
)

func TestQuitRunning(t *testing.T) {
	t.Parallel()
	nothing := httptest.NewServer(http.NotFoundHandler())
	nothing.Close()
	if err := quitRunning(t.Context(), nothing.Client(), nothing.URL, time.Second, 10*time.Millisecond); err != nil {
		t.Fatalf("nothing running: %v", err)
	}
	tests := []struct {
		name         string
		stopsOnQuit  bool
		wantErr      bool
		wantQuitPost int32
	}{
		{"instance quits", true, false, 1},
		{"instance keeps answering", false, true, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var quits atomic.Int32
			var running *httptest.Server
			running = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/health":
					if err := json.NewEncoder(w).Encode(api.Health{App: api.AppName}); err != nil {
						t.Error(err)
					}
				case quitEndpoint:
					quits.Add(1)
					w.WriteHeader(http.StatusNoContent)
					if tc.stopsOnQuit {
						go running.Close()
					}
				}
			}))
			t.Cleanup(running.Close)
			client := &http.Client{Timeout: 200 * time.Millisecond}
			err := quitRunning(t.Context(), client, running.URL, 300*time.Millisecond, 10*time.Millisecond)
			if (err != nil) != tc.wantErr || quits.Load() != tc.wantQuitPost {
				t.Fatalf("err %v after %d quit posts", err, quits.Load())
			}
		})
	}
}
