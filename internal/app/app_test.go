package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/1etu/ferry/internal/config"
)

func TestRunDispatchesCommands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   []string
		code   int
		stdout string
	}{
		{"version", []string{"version"}, exitOK, Version + "\n"},
		{"unknown command", []string{"serve"}, exitUsage, ""},
		{"send without paths", []string{"send"}, exitUsage, ""},
		{"version with extra arguments", []string{"version", "now"}, exitUsage, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			if code := Run(t.Context(), tc.args, &stdout, io.Discard); code != tc.code {
				t.Fatalf("exit %d, want %d", code, tc.code)
			}
			if stdout.String() != tc.stdout {
				t.Fatalf("stdout %q, want %q", stdout.String(), tc.stdout)
			}
		})
	}
}

func TestLoadConfigAppliesPortOverrideAndValidates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeConfig(t, dir, map[string]any{"receivedDir": t.TempDir(), "port": 9000})
	tests := []struct {
		name    string
		envPort int
		want    int
		isValid bool
	}{
		{"config port", 0, 9000, true},
		{"env override", 9100, 9100, true},
		{"env out of range", 70000, 0, false},
	}
	for _, tc := range tests {
		cfg, err := loadConfig(config.Env{Port: tc.envPort}, dir, "testpc")
		if (err == nil) != tc.isValid {
			t.Fatalf("%s: err %v", tc.name, err)
		}
		if tc.isValid && cfg.Port != tc.want {
			t.Fatalf("%s: port %d, want %d", tc.name, cfg.Port, tc.want)
		}
	}
}

func TestOriginsAndHostsFollowTheLAN(t *testing.T) {
	t.Parallel()
	lan := []net.IP{net.ParseIP("192.168.1.23"), net.ParseIP("10.0.0.5")}
	tests := []struct {
		name      string
		list      func() ([]net.IP, error)
		wantIP    string
		wantHosts []string
	}{
		{
			"first lan address", func() ([]net.IP, error) { return lan, nil }, "http://192.168.1.23:8080",
			[]string{"testpc", "testpc.local", "192.168.1.23", "10.0.0.5"},
		},
		{
			"no lan address", func() ([]net.IP, error) { return nil, nil }, "http://127.0.0.1:8080",
			[]string{"testpc", "testpc.local"},
		},
		{
			"listing fails", func() ([]net.IP, error) { return nil, errors.New("no interfaces") }, "http://127.0.0.1:8080",
			[]string{"testpc", "testpc.local"},
		},
	}
	for _, tc := range tests {
		m := &machine{port: 8080, hostname: "testpc", listLAN: tc.list, log: slog.New(slog.DiscardHandler)}
		local, ip := m.origins()
		if local != "http://testpc.local:8080" || ip != tc.wantIP {
			t.Errorf("%s: origins %q %q", tc.name, local, ip)
		}
		if diff := cmp.Diff(tc.wantHosts, m.hosts()); diff != "" {
			t.Errorf("%s: hosts (-want +got):\n%s", tc.name, diff)
		}
	}
}

func TestOpenLogRotatesOnlyOversizedFiles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		size        int64
		wantRotated bool
	}{
		{"small file is appended to", 100, false},
		{"file over 10 MiB is rotated", maxLogFileBytes + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			logDir := filepath.Join(dir, logDirName)
			if err := os.MkdirAll(logDir, 0o750); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(logDir, logFileName)
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(path, tc.size); err != nil {
				t.Fatal(err)
			}
			log, closeLog, err := openLog(dir, false, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			log.Info("started")
			log.Debug("hidden at info level")
			if err := closeLog(); err != nil {
				t.Fatal(err)
			}
			_, rotatedErr := os.Stat(filepath.Join(logDir, rotatedLogName))
			if (rotatedErr == nil) != tc.wantRotated {
				t.Fatalf("rotated file: %v", rotatedErr)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(content), `"msg":"started"`) || strings.Contains(string(content), "hidden") {
				t.Fatalf("log tail %q", content[max(0, len(content)-200):])
			}
		})
	}
}

func TestDevLogWritesDebugTextToStderr(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	log, closeLog, err := openLog(t.TempDir(), true, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	log.Debug("request", "route", "GET /api/health")
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), `level=DEBUG msg=request route="GET /api/health"`) {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestIsRunningRecognizesOnlyFerry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"ferry", `{"app":"ferry","name":"pc","version":"dev"}`, true},
		{"another app", `{"app":"other"}`, false},
		{"not json", `<html>`, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if _, err := io.WriteString(w, tc.body); err != nil {
				t.Error(err)
			}
		}))
		if got := isRunning(t.Context(), server.Client(), server.URL); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
		server.Close()
	}
	if isRunning(t.Context(), http.DefaultClient, loopbackURL(freePort(t))) {
		t.Error("nothing listening counts as running")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("address %T", listener.Addr())
	}
	return addr.Port
}

func writeConfig(t *testing.T, dir string, values map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, configFileName), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}
