package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/1etu/ferry/internal/update"
)

const (
	defaultKeyEnv   = "FERRY_SIGNING_KEY"
	defaultPlatform = "windows-amd64"
	manifestName    = "manifest.json"
	signatureName   = "manifest.sig"
	sumsName        = "SHA256SUMS"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("sign", flag.ContinueOnError)
	version := flags.String("version", "", "release version, MAJOR.MINOR.PATCH")
	keyEnv := flags.String("key-env", defaultKeyEnv, "environment variable holding the base64 seed")
	assetPath := flags.String("asset", "Ferry.exe", "binary to describe in the manifest")
	platform := flags.String("platform", defaultPlatform, "manifest key for the asset")
	outDir := flags.String("out", ".", "directory that receives the manifest, signature and sums")
	if err := flags.Parse(args); err != nil {
		return err
	}
	key, err := signingKey(os.Getenv(*keyEnv), *keyEnv)
	if err != nil {
		return err
	}
	asset, err := describeAsset(*assetPath)
	if err != nil {
		return err
	}
	manifest := update.Manifest{Version: *version, Assets: map[string]update.Asset{*platform: asset}}
	body, err := update.MarshalManifest(manifest)
	if err != nil {
		return err
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(key, body))
	manifestSum := sha256.Sum256(body)
	sums := fmt.Sprintf("%s  %s\n%s  %s\n", asset.SHA256, asset.Name, hex.EncodeToString(manifestSum[:]), manifestName)
	files := map[string]string{manifestName: string(body), signatureName: signature, sumsName: sums}
	for name, content := range files {
		path := filepath.Join(*outDir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	_, err = fmt.Fprintf(out, "signed %s: %s %d bytes sha256 %s\n", *version, asset.Name, asset.Size, asset.SHA256)
	return err
}

func signingKey(encoded, envName string) (ed25519.PrivateKey, error) {
	if encoded == "" {
		return nil, errors.New(envName + " is not set")
	}
	seed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", envName, err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s holds %d bytes, want %d", envName, len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func describeAsset(path string) (update.Asset, error) {
	f, err := os.Open(path)
	if err != nil {
		return update.Asset{}, fmt.Errorf("open asset: %w", err)
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, f)
	if err != nil {
		return update.Asset{}, fmt.Errorf("read asset %s: %w", path, err)
	}
	return update.Asset{
		Name:   filepath.Base(path),
		SHA256: hex.EncodeToString(hash.Sum(nil)),
		Size:   size,
	}, nil
}
