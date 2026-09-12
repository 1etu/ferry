package update

import (
	"bytes"
	"cmp"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	maxManifestBytes  = 64 << 10
	maxSignatureBytes = 1 << 10
	sha256HexLength   = 64
)

var (
	ErrSignature = errors.New("manifest signature invalid")
	ErrVersion   = errors.New("version is not MAJOR.MINOR.PATCH")
	ErrAsset     = errors.New("manifest asset invalid")
)

type Manifest struct {
	Assets  map[string]Asset `json:"assets"`
	Version string           `json:"version"`
}

type Asset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func MarshalManifest(m Manifest) ([]byte, error) {
	if _, err := ParseVersion(m.Version); err != nil {
		return nil, fmt.Errorf("manifest version %q: %w", m.Version, err)
	}
	return json.Marshal(m)
}

func parseSignedManifest(body, sig []byte, key ed25519.PublicKey) (Manifest, Version, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil {
		return Manifest{}, Version{}, fmt.Errorf("%w: %w", ErrSignature, err)
	}
	if len(raw) != ed25519.SignatureSize || !ed25519.Verify(key, body, raw) {
		return Manifest{}, Version{}, ErrSignature
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, Version{}, fmt.Errorf("decode manifest: %w", err)
	}
	v, err := ParseVersion(m.Version)
	if err != nil {
		return Manifest{}, Version{}, fmt.Errorf("manifest version %q: %w", m.Version, err)
	}
	return m, v, nil
}

func (m Manifest) asset(platform string) (Asset, error) {
	a, ok := m.Assets[platform]
	if !ok {
		return Asset{}, fmt.Errorf("%w: no asset for %s", ErrAsset, platform)
	}
	if a.Name == "" || a.Name != filepath.Base(a.Name) || a.Size <= 0 {
		return Asset{}, fmt.Errorf("%w: bad name or size for %s", ErrAsset, platform)
	}
	if sum, err := hex.DecodeString(a.SHA256); err != nil || len(sum) != sha256HexLength/2 {
		return Asset{}, fmt.Errorf("%w: bad sha256 for %s", ErrAsset, platform)
	}
	return a, nil
}

type Version [3]int

func ParseVersion(s string) (Version, error) {
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return Version{}, ErrVersion
	}
	var v Version
	for i, part := range parts {
		if !isCanonicalNumber(part) {
			return Version{}, ErrVersion
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return Version{}, fmt.Errorf("%w: %w", ErrVersion, err)
		}
		v[i] = n
	}
	return v, nil
}

func isCanonicalNumber(s string) bool {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (v Version) Compare(o Version) int {
	for i := range v {
		if c := cmp.Compare(v[i], o[i]); c != 0 {
			return c
		}
	}
	return 0
}

func (v Version) Less(o Version) bool {
	return v.Compare(o) < 0
}

func (v Version) String() string {
	return strconv.Itoa(v[0]) + "." + strconv.Itoa(v[1]) + "." + strconv.Itoa(v[2])
}

type cache struct {
	ETag    string `json:"etag"`
	Version string `json:"version"`
	Failed  string `json:"failed"`
}

func loadCache(path string) (cache, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cache{}, nil
	}
	if err != nil {
		return cache{}, fmt.Errorf("read %s: %w", path, err)
	}
	var c cache
	if err := json.Unmarshal(body, &c); err != nil {
		return cache{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return c, nil
}

func saveCache(path string, c cache) error {
	body, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}
