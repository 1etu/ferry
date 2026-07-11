package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func testDefaults(t *testing.T) Config {
	t.Helper()
	return Defaults("desktop-pc", filepath.Join(t.TempDir(), "received"))
}

func TestDefaults(t *testing.T) {
	t.Parallel()
	got := Defaults("desktop-pc", "/home/me/Downloads/Ferry")
	want := Config{
		Name:                      "desktop-pc",
		Port:                      8080,
		ReceivedDir:               "/home/me/Downloads/Ferry",
		MaxUploadBytes:            68719476736,
		ReserveBytes:              2147483648,
		MaxActiveUploadsPerDevice: 8,
		CheckUpdates:              true,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("defaults mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadCreatesMissingFileWithDefaults(t *testing.T) {
	t.Parallel()
	defaults := testDefaults(t)
	path := filepath.Join(t.TempDir(), "nested", "config.json")

	got, err := Load(path, defaults)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if diff := cmp.Diff(defaults, got); diff != "" {
		t.Fatalf("loaded config mismatch (-want +got):\n%s", diff)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	var written Config
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("decode written config: %v", err)
	}
	if diff := cmp.Diff(defaults, written); diff != "" {
		t.Fatalf("written config mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadReadsBackWrittenFile(t *testing.T) {
	t.Parallel()
	defaults := testDefaults(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if _, err := Load(path, defaults); err != nil {
		t.Fatalf("first load: %v", err)
	}
	got, err := Load(path, Defaults("other", "/elsewhere"))
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if diff := cmp.Diff(defaults, got); diff != "" {
		t.Fatalf("reloaded config mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadFillsMissingKeysFromDefaults(t *testing.T) {
	t.Parallel()
	defaults := testDefaults(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"port": 9090, "maxActiveUploadsPerDevice": 3}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := Load(path, defaults)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := defaults
	want.Port = 9090
	want.MaxActiveUploadsPerDevice = 3
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("merged config mismatch (-want +got):\n%s", diff)
	}
}

func TestSaveThenLoad(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		modify func(*Config)
	}{
		{name: "defaults", modify: func(*Config) {}},
		{name: "every field changed", modify: func(c *Config) {
			c.Name = "Ana’s PC"
			c.Port = 9191
			c.ReceivedDir = filepath.Join(c.ReceivedDir, "elsewhere")
			c.MaxUploadBytes = 1 << 20
			c.ReserveBytes = 0
			c.MaxActiveUploadsPerDevice = 2
			c.CheckUpdates = false
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := testDefaults(t)
			tt.modify(&want)
			path := filepath.Join(t.TempDir(), "nested", "config.json")
			if err := want.Save(path); err != nil {
				t.Fatalf("save: %v", err)
			}
			got, err := Load(path, Defaults("other", "/elsewhere"))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("saved config mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSaveReplacesExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c := testDefaults(t)
	if _, err := Load(path, c); err != nil {
		t.Fatalf("first load: %v", err)
	}
	c.Port = 9292
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path, testDefaults(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Port != 9292 {
		t.Fatalf("port = %d, want 9292", got.Port)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir holds %d entries, want only config.json", len(entries))
	}
}

func TestLoadRejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{name: "unknown key", content: `{"port": 8080, "theme": "dark"}`, wantErr: `unknown field "theme"`},
		{name: "removed v1 key", content: `{"openBrowserOnStart": false}`, wantErr: `unknown field "openBrowserOnStart"`},
		{name: "wrong type", content: `{"port": "8080"}`, wantErr: "cannot unmarshal"},
		{name: "malformed json", content: `{"port": `, wantErr: "unexpected EOF"},
		{name: "empty file", content: ``, wantErr: "EOF"},
		{name: "trailing data", content: `{"port": 8080} {}`, wantErr: "unexpected data after config object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path, testDefaults(t))
			if err == nil {
				t.Fatal("got nil error, want one")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr string
	}{
		{name: "defaults are valid", modify: func(*Config) {}},
		{name: "lowest port", modify: func(c *Config) { c.Port = 1 }},
		{name: "highest port", modify: func(c *Config) { c.Port = 65535 }},
		{name: "zero reserve is allowed", modify: func(c *Config) { c.ReserveBytes = 0 }},
		{name: "empty name", modify: func(c *Config) { c.Name = " " }, wantErr: "name is empty"},
		{name: "port zero", modify: func(c *Config) { c.Port = 0 }, wantErr: "port 0 is outside"},
		{name: "port above range", modify: func(c *Config) { c.Port = 65536 }, wantErr: "port 65536 is outside"},
		{name: "negative port", modify: func(c *Config) { c.Port = -1 }, wantErr: "port -1 is outside"},
		{name: "relative received dir", modify: func(c *Config) { c.ReceivedDir = "received" }, wantErr: "not an absolute path"},
		{name: "empty received dir", modify: func(c *Config) { c.ReceivedDir = "" }, wantErr: "not an absolute path"},
		{name: "zero max upload", modify: func(c *Config) { c.MaxUploadBytes = 0 }, wantErr: "maxUploadBytes 0 is not positive"},
		{name: "negative reserve", modify: func(c *Config) { c.ReserveBytes = -1 }, wantErr: "reserveBytes -1 is negative"},
		{name: "zero active uploads", modify: func(c *Config) { c.MaxActiveUploadsPerDevice = 0 }, wantErr: "maxActiveUploadsPerDevice 0 is not positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := testDefaults(t)
			tt.modify(&c)
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("got error %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("got nil error, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateReportsEveryInvalidField(t *testing.T) {
	t.Parallel()
	c := testDefaults(t)
	c.Port = 0
	c.MaxUploadBytes = -5
	err := c.Validate()
	if err == nil {
		t.Fatal("got nil error, want one")
	}
	for _, want := range []string{"port 0", "maxUploadBytes -5"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("got error %q, want it to contain %q", err, want)
		}
	}
}

func TestIncomingDirIsInsideReceivedDir(t *testing.T) {
	t.Parallel()
	c := testDefaults(t)
	want := filepath.Join(c.ReceivedDir, ".incoming")
	if got := c.IncomingDir(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadEnv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		vars map[string]string
		want Env
	}{
		{name: "nothing set", vars: map[string]string{}, want: Env{}},
		{
			name: "everything set",
			vars: map[string]string{
				"FERRY_DATA_DIR": "/data",
				"FERRY_HEADLESS": "1",
				"FERRY_DEV":      "1",
				"FERRY_PORT":     "18080",
			},
			want: Env{DataDir: "/data", Headless: true, Dev: true, Port: 18080},
		},
		{
			name: "flags other than 1 are off",
			vars: map[string]string{"FERRY_HEADLESS": "true", "FERRY_DEV": "0"},
			want: Env{},
		},
		{name: "port with spaces", vars: map[string]string{"FERRY_PORT": " 9000 "}, want: Env{Port: 9000}},
		{name: "non-numeric port is ignored", vars: map[string]string{"FERRY_PORT": "http"}, want: Env{}},
		{name: "out of range port is kept for Validate", vars: map[string]string{"FERRY_PORT": "70000"}, want: Env{Port: 70000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := readEnv(func(key string) string { return tt.vars[key] })
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("env mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
