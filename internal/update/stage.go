package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const downloadTimeout = 10 * time.Minute

var (
	ErrNothingPending = errors.New("no update to stage")
	ErrAssetMismatch  = errors.New("downloaded asset does not match the manifest")
)

func (u *Updater) Stage(ctx context.Context) error {
	if err := u.acquire(); err != nil {
		return err
	}
	defer u.release()
	m, ok := u.pendingManifest()
	if !ok {
		return ErrNothingPending
	}
	state, version, err := u.stageManifest(ctx, m)
	u.finish(state, version, err)
	return err
}

func (u *Updater) stageManifest(ctx context.Context, m Manifest) (State, string, error) {
	u.set(func(s *Status) { s.State, s.Available = StateDownloading, m.Version })
	u.setPending(m)
	if err := u.stage(ctx, m); err != nil {
		return StateFailed, m.Version, err
	}
	u.markStaged(m.Version)
	u.log.Info("update staged", "version", m.Version, "path", u.newExePath())
	return StateReady, m.Version, nil
}

func (u *Updater) stage(ctx context.Context, m Manifest) error {
	asset, err := m.asset(platformKey())
	if err != nil {
		return err
	}
	target := u.newExePath()
	matches, err := fileMatches(target, asset)
	if err != nil {
		u.log.Warn("staged file unreadable, downloading again", "path", target, "err", err)
	}
	if matches {
		u.log.Info("update already staged", "path", target)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	if err := u.download(ctx, u.assetURL(m.Version, asset.Name), target, asset); err != nil {
		if rmErr := os.Remove(target); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			return errors.Join(err, rmErr)
		}
		return err
	}
	return nil
}

func (u *Updater) download(ctx context.Context, rawURL, target string, asset Asset) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("request %s: %w", rawURL, err)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("get %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get %s: status %d", rawURL, resp.StatusCode)
	}
	//nolint:gosec
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, asset.Size+1))
	if err != nil {
		return fmt.Errorf("download %s: %w", rawURL, err)
	}
	return verifyAsset(asset, n, hash.Sum(nil))
}

func verifyAsset(asset Asset, size int64, sum []byte) error {
	if size != asset.Size {
		return fmt.Errorf("%w: %d bytes, manifest says %d", ErrAssetMismatch, size, asset.Size)
	}
	if hex.EncodeToString(sum) != asset.SHA256 {
		return fmt.Errorf("%w: sha256 differs", ErrAssetMismatch)
	}
	return nil
}

func fileMatches(path string, asset Asset) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, f)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	return verifyAsset(asset, n, hash.Sum(nil)) == nil, nil
}

func (u *Updater) assetURL(version, name string) string {
	return u.releaseURL + "/download/v" + version + "/" + name
}

func platformKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func (u *Updater) newExePath() string {
	return siblingExe(u.exePath, ".new")
}

func (u *Updater) oldExePath() string {
	return siblingExe(u.exePath, ".old")
}

func siblingExe(exePath, tag string) string {
	ext := filepath.Ext(exePath)
	return strings.TrimSuffix(exePath, ext) + tag + ext
}
