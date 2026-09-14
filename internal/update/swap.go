package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"time"
)

const (
	healthTimeout     = 15 * time.Second
	healthInterval    = 250 * time.Millisecond
	healthProbeLimit  = 4 << 10
	removeOldTimeout  = 5 * time.Second
	removeOldInterval = 250 * time.Millisecond
	updatedFlag       = "--updated"
)

var ErrNotReady = errors.New("no update is ready")

func (u *Updater) Swap() (restore func() error, err error) {
	if u.stagedVersion() == "" {
		return nil, ErrNotReady
	}
	exe, newExe, oldExe := u.exePath, u.newExePath(), u.oldExePath()
	if err := os.Remove(oldExe); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("remove %s: %w", oldExe, err)
	}
	if err := os.Rename(exe, oldExe); err != nil {
		return nil, fmt.Errorf("rename %s: %w", exe, err)
	}
	if err := os.Rename(newExe, exe); err != nil {
		return nil, errors.Join(fmt.Errorf("rename %s: %w", newExe, err), os.Rename(oldExe, exe))
	}
	restore = func() error {
		return errors.Join(os.Rename(exe, newExe), os.Rename(oldExe, exe))
	}
	return restore, nil
}

func (u *Updater) Apply(ctx context.Context, healthURL string) error {
	if err := u.acquire(); err != nil {
		return err
	}
	defer u.release()
	version := u.stagedVersion()
	if version == "" {
		return ErrNotReady
	}
	err := u.swapAndSupervise(ctx, version, healthURL)
	if err != nil {
		u.recordFailed(version, err)
		return err
	}
	u.clearStaged()
	u.log.Info("update applied", "version", version)
	return nil
}

func (u *Updater) swapAndSupervise(ctx context.Context, version, healthURL string) error {
	restore, err := u.Swap()
	if err != nil {
		return err
	}
	child, err := u.launch(u.exePath, updatedFlag)
	if err != nil {
		return errors.Join(fmt.Errorf("start %s: %w", u.exePath, err), restore())
	}
	if err := u.awaitHealthy(ctx, child, healthURL, version); err != nil {
		return errors.Join(err, kill(child), restore())
	}
	return nil
}

func (u *Updater) awaitHealthy(ctx context.Context, child *os.Process, healthURL, version string) error {
	exited := make(chan error, 1)
	go func() {
		state, err := child.Wait()
		if err != nil {
			exited <- fmt.Errorf("wait for new process: %w", err)
			return
		}
		exited <- fmt.Errorf("new process exited: %s", state)
	}()
	ctx, cancel := context.WithTimeout(ctx, u.healthTimeout)
	defer cancel()
	ticker := time.NewTicker(healthInterval)
	defer ticker.Stop()
	for {
		if u.answersWithVersion(ctx, healthURL, version) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("new process not healthy within %s: %w", u.healthTimeout, ctx.Err())
		case err := <-exited:
			return err
		case <-ticker.C:
		}
	}
}

func (u *Updater) answersWithVersion(ctx context.Context, healthURL, version string) bool {
	ctx, cancel := context.WithTimeout(ctx, healthInterval*4)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, http.NoBody)
	if err != nil {
		return false
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var health struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, healthProbeLimit)).Decode(&health); err != nil {
		return false
	}
	return resp.StatusCode == http.StatusOK && health.Version == version
}

func kill(child *os.Process) error {
	err := child.Kill()
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return fmt.Errorf("kill new process: %w", err)
}

func (u *Updater) recordFailed(version string, cause error) {
	c, err := loadCache(u.cachePath)
	if err != nil {
		u.log.Warn("update cache ignored", "path", u.cachePath, "err", err)
	}
	c.Failed = version
	if err := saveCache(u.cachePath, c); err != nil {
		u.log.Warn("update cache not saved", "path", u.cachePath, "err", err)
	}
	if err := os.Remove(u.newExePath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		u.log.Warn("staged update not removed", "path", u.newExePath(), "err", err)
	}
	u.clearStaged()
	u.finish(StateFailed, version, cause)
}

func (u *Updater) RemoveOld(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, removeOldTimeout)
	defer cancel()
	ticker := time.NewTicker(removeOldInterval)
	defer ticker.Stop()
	for {
		err := os.Remove(u.oldExePath())
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("remove %s: %w", u.oldExePath(), err)
		case <-ticker.C:
		}
	}
}
