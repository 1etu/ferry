package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func testKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return public, private
}

func sampleManifest() Manifest {
	return Manifest{
		Version: "1.2.3",
		Assets: map[string]Asset{
			"windows-arm64": {Name: "Ferry-arm64.exe", Size: 2, SHA256: strings.Repeat("b", 64)},
			"windows-amd64": {Name: "Ferry.exe", Size: 1, SHA256: strings.Repeat("a", 64)},
		},
	}
}

func TestMarshalManifestIsCanonical(t *testing.T) {
	t.Parallel()
	got, err := MarshalManifest(sampleManifest())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"assets":{"windows-amd64":{"name":"Ferry.exe","sha256":"` + strings.Repeat("a", 64) +
		`","size":1},"windows-arm64":{"name":"Ferry-arm64.exe","sha256":"` + strings.Repeat("b", 64) +
		`","size":2}},"version":"1.2.3"}`
	if string(got) != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	if _, err := MarshalManifest(Manifest{Version: "1.2"}); !errors.Is(err, ErrVersion) {
		t.Fatalf("bad version accepted: %v", err)
	}
}

func TestParseSignedManifest(t *testing.T) {
	t.Parallel()
	public, private := testKey(t)
	_, other := testKey(t)
	body, err := MarshalManifest(sampleManifest())
	if err != nil {
		t.Fatal(err)
	}
	sign := func(key ed25519.PrivateKey, b []byte) []byte {
		return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, b)))
	}
	flipped := append([]byte(nil), body...)
	flipped[len(flipped)-3] ^= 1
	unknown := []byte(`{"assets":{},"version":"1.2.3","extra":1}`)
	badVersion := []byte(`{"assets":{},"version":"1.2.3-rc1"}`)

	tests := []struct {
		name     string
		body     []byte
		sig      []byte
		key      ed25519.PublicKey
		wantErr  error
		wantText string
	}{
		{"valid", body, sign(private, body), public, nil, ""},
		{"tampered body", flipped, sign(private, body), public, ErrSignature, ""},
		{"wrong key", body, sign(other, body), public, ErrSignature, ""},
		{"signature not base64", body, []byte("!!"), public, ErrSignature, ""},
		{"signature too short", body, []byte(base64.StdEncoding.EncodeToString([]byte("short"))), public, ErrSignature, ""},
		{"signed but unknown field", unknown, sign(private, unknown), public, nil, "unknown field"},
		{"signed but bad version", badVersion, sign(private, badVersion), public, ErrVersion, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, v, err := parseSignedManifest(tc.body, tc.sig, tc.key)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got %v, want %v", err, tc.wantErr)
				}
			case tc.wantText != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantText) {
					t.Fatalf("got %v, want %q", err, tc.wantText)
				}
			case err != nil:
				t.Fatalf("unexpected error %v", err)
			case m.Version != "1.2.3" || v != (Version{1, 2, 3}) || len(m.Assets) != 2:
				t.Fatalf("got %+v %v", m, v)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want Version
		ok   bool
	}{
		{"1.2.3", Version{1, 2, 3}, true},
		{"v1.2.3", Version{1, 2, 3}, true},
		{"0.0.0", Version{}, true},
		{"10.20.30", Version{10, 20, 30}, true},
		{"01.2.3", Version{}, false},
		{"1.2", Version{}, false},
		{"1.2.3.4", Version{}, false},
		{"1.2.3-rc1", Version{}, false},
		{"1.-2.3", Version{}, false},
		{"+1.2.3", Version{}, false},
		{"1. 2.3", Version{}, false},
		{"", Version{}, false},
		{"dev", Version{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseVersion(tc.in)
			if (err == nil) != tc.ok {
				t.Fatalf("err %v, want ok=%v", err, tc.ok)
			}
			if err != nil && !errors.Is(err, ErrVersion) {
				t.Fatalf("error %v is not ErrVersion", err)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVersionCompare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.9.9", "1.10.0", -1},
		{"2.0.0", "1.99.99", 1},
	}
	for _, tc := range tests {
		t.Run(tc.a+" vs "+tc.b, func(t *testing.T) {
			t.Parallel()
			a, err := ParseVersion(tc.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := ParseVersion(tc.b)
			if err != nil {
				t.Fatal(err)
			}
			if got := a.Compare(b); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
			if a.Less(b) != (tc.want < 0) {
				t.Fatalf("Less disagrees with Compare")
			}
			if a.String() != strings.TrimPrefix(tc.a, "v") {
				t.Fatalf("String %q", a.String())
			}
		})
	}
}

func TestManifestAssetValidation(t *testing.T) {
	t.Parallel()
	good := Asset{Name: "Ferry.exe", Size: 10, SHA256: strings.Repeat("0", 64)}
	tests := []struct {
		name  string
		asset Asset
		ok    bool
	}{
		{"valid", good, true},
		{"path in name", Asset{Name: "../Ferry.exe", Size: 10, SHA256: good.SHA256}, false},
		{"empty name", Asset{Size: 10, SHA256: good.SHA256}, false},
		{"zero size", Asset{Name: "Ferry.exe", SHA256: good.SHA256}, false},
		{"short hash", Asset{Name: "Ferry.exe", Size: 10, SHA256: "abcd"}, false},
		{"non hex hash", Asset{Name: "Ferry.exe", Size: 10, SHA256: strings.Repeat("z", 64)}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := Manifest{Version: "1.0.0", Assets: map[string]Asset{"windows-amd64": tc.asset}}
			_, err := m.asset("windows-amd64")
			if (err == nil) != tc.ok {
				t.Fatalf("err %v, want ok=%v", err, tc.ok)
			}
			if err != nil && !errors.Is(err, ErrAsset) {
				t.Fatalf("error %v is not ErrAsset", err)
			}
		})
	}
	empty := Manifest{Version: "1.0.0"}
	if _, err := empty.asset("linux-amd64"); !errors.Is(err, ErrAsset) {
		t.Fatalf("missing platform: %v", err)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), cacheFileName)
	if c, err := loadCache(path); err != nil || c != (cache{}) {
		t.Fatalf("missing file: %+v %v", c, err)
	}
	want := cache{ETag: `"abc"`, Version: "1.0.1", Failed: "1.0.0"}
	if err := saveCache(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadCache(path)
	if err != nil || got != want {
		t.Fatalf("got %+v %v, want %+v", got, err, want)
	}
	if exists(t, path+".tmp") {
		t.Fatal("temp file left behind")
	}
	writeFile(t, path, "{not json")
	if _, err := loadCache(path); err == nil {
		t.Fatal("corrupt cache accepted")
	}
}
