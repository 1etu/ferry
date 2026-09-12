package install

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/1etu/ferry/internal/platform"
)

const (
	exeName           = "Ferry.exe"
	startMenuLinkName = "Ferry.lnk"
	productName       = "Ferry"
)

type layout struct {
	installDir         string
	startMenuLink      string
	sendToLink         string
	registry           platform.RegistryRoot
	removeDirAfterExit func(dir string) error
}

func IsInstalled(exePath string) (bool, error) {
	l, err := defaultLayout()
	if err != nil {
		return false, err
	}
	return l.isInstalled(exePath)
}

func Install(exePath, version string) (installedExe string, err error) {
	l, err := defaultLayout()
	if err != nil {
		return "", err
	}
	return l.install(exePath, version)
}

func (l layout) installedExe() string {
	return filepath.Join(l.installDir, exeName)
}

func (l layout) isInstalled(exePath string) (bool, error) {
	running, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return false, fmt.Errorf("resolve running exe %s: %w", exePath, err)
	}
	installed, err := filepath.EvalSymlinks(l.installedExe())
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("resolve installed exe %s: %w", l.installedExe(), err)
	}
	return strings.EqualFold(running, installed), nil
}

func (l layout) install(exePath, version string) (string, error) {
	exe := l.installedExe()
	isInstalled, err := l.isInstalled(exePath)
	if err != nil {
		return "", err
	}
	if !isInstalled {
		if err := os.MkdirAll(l.installDir, 0o750); err != nil {
			return "", fmt.Errorf("create install dir %s: %w", l.installDir, err)
		}
		if err := copyExe(exePath, exe); err != nil {
			return "", err
		}
	}
	info, err := os.Stat(exe)
	if err != nil {
		return "", fmt.Errorf("stat installed exe: %w", err)
	}
	if err := platform.WriteShortcut(l.startMenuLink, exe, "", exe); err != nil {
		return "", fmt.Errorf("install start menu shortcut: %w", err)
	}
	if err := platform.WriteShortcut(l.sendToLink, exe, "send", exe); err != nil {
		return "", fmt.Errorf("install send to shortcut: %w", err)
	}
	if err := l.registry.WriteUninstallEntry(uninstallEntry(exe, l.installDir, version, info.Size())); err != nil {
		return "", fmt.Errorf("install uninstall entry: %w", err)
	}
	if err := l.registry.SetRunAtLogin(exe, true); err != nil {
		return "", fmt.Errorf("install run at login: %w", err)
	}
	return exe, nil
}

func uninstallEntry(exe, installDir, version string, sizeBytes int64) platform.UninstallEntry {
	return platform.UninstallEntry{
		DisplayName:     productName,
		DisplayVersion:  version,
		DisplayIcon:     exe + ",0",
		Publisher:       productName,
		InstallLocation: installDir,
		UninstallString: `"` + exe + `" uninstall`,
		EstimatedSizeKB: sizeKB(sizeBytes),
	}
}

func sizeKB(sizeBytes int64) uint32 {
	kb := (max(sizeBytes, 0) + 1023) / 1024
	return uint32(min(kb, math.MaxUint32))
}

func copyExe(src, dst string) error {
	tmp := dst + ".tmp"
	if err := writeCopy(src, tmp); err != nil {
		return errors.Join(fmt.Errorf("copy %s to %s: %w", src, tmp, err), removeIfExists(tmp))
	}
	if err := os.Rename(tmp, dst); err != nil {
		return errors.Join(fmt.Errorf("replace %s: %w", dst, err), removeIfExists(tmp))
	}
	return nil
}

func writeCopy(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
