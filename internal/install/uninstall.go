package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	removeRetryTimeout  = 5 * time.Second
	removeRetryInterval = 250 * time.Millisecond
)

var dataDirEntries = []string{"config.json", "ferry.db", "ferry.db-wal", "ferry.db-shm", "update.json", "logs", "webview"}

func Uninstall(ctx context.Context, dataDir string, quit func(context.Context) error) error {
	l, err := defaultLayout()
	if err != nil {
		return err
	}
	return l.uninstall(ctx, dataDir, quit)
}

func (l layout) uninstall(ctx context.Context, dataDir string, quit func(context.Context) error) error {
	if !filepath.IsAbs(dataDir) {
		return fmt.Errorf("uninstall: data dir %q is not absolute", dataDir)
	}
	if err := quit(ctx); err != nil {
		return fmt.Errorf("uninstall: quit running instance: %w", err)
	}
	return errors.Join(
		l.registry.SetRunAtLogin("", false),
		removeIfExists(l.startMenuLink),
		removeIfExists(l.sendToLink),
		l.registry.RemoveUninstallEntry(),
		removeDataDir(ctx, dataDir),
		l.removeInstallDir(),
	)
}

func removeDataDir(ctx context.Context, dataDir string) error {
	ctx, cancel := context.WithTimeout(ctx, removeRetryTimeout)
	defer cancel()
	ticker := time.NewTicker(removeRetryInterval)
	defer ticker.Stop()
	for {
		err := removeDataEntries(dataDir)
		if err == nil {
			return removeIfEmpty(dataDir)
		}
		select {
		case <-ctx.Done():
			return err
		case <-ticker.C:
		}
	}
}

func removeDataEntries(dataDir string) error {
	var errs []error
	for _, name := range dataDirEntries {
		path := filepath.Join(dataDir, name)
		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}

func removeIfEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list data dir %s: %w", dir, err)
	}
	if len(entries) > 0 {
		return nil
	}
	if err := os.Remove(dir); err != nil {
		return fmt.Errorf("remove data dir %s: %w", dir, err)
	}
	return nil
}

func (l layout) removeInstallDir() error {
	if err := l.removeDirAfterExit(l.installDir); err != nil {
		return fmt.Errorf("schedule removal of %s: %w", l.installDir, err)
	}
	return nil
}
