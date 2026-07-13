package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func DataDir() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, appDirName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home folder: %w", err)
	}
	return filepath.Join(home, ".local", "share", appDirName), nil
}

func DefaultReceivedDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home folder: %w", err)
	}
	return filepath.Join(home, "Downloads", appDirName), nil
}

func FreeSpace(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, fmt.Errorf("query free space of %s: %w", dir, err)
	}
	if st.Bsize <= 0 {
		return 0, fmt.Errorf("query free space of %s: block size %d", dir, st.Bsize)
	}
	return st.Bavail * uint64(st.Bsize), nil
}

func OpenURL(url string) error {
	return runXDGOpen(url)
}

func OpenFolder(path string) error {
	return runXDGOpen(path)
}

func RevealFile(string) error {
	return ErrUnsupported
}

func runXDGOpen(target string) error {
	if err := exec.CommandContext(context.Background(), "xdg-open", target).Run(); err != nil {
		return fmt.Errorf("xdg-open %s: %w", target, err)
	}
	return nil
}

func PickFiles(context.Context, string) ([]string, error) {
	return nil, ErrUnsupported
}

func InstallSendTo(string) error {
	return ErrUnsupported
}

func InstallDir() (string, error) {
	return "", ErrUnsupported
}

func PickFolder(context.Context, string) (string, error) {
	return "", ErrUnsupported
}

func WriteShortcut(_, _, _, _ string) error {
	return ErrUnsupported
}

func (RegistryRoot) RunAtLogin() (bool, error) {
	return false, ErrUnsupported
}

func (RegistryRoot) SetRunAtLogin(string, bool) error {
	return ErrUnsupported
}

func (RegistryRoot) WriteUninstallEntry(UninstallEntry) error {
	return ErrUnsupported
}

func (RegistryRoot) RemoveUninstallEntry() error {
	return ErrUnsupported
}

func FirewallState(context.Context, string) (Firewall, error) {
	return Firewall{Profile: ProfileUnknown}, ErrUnsupported
}

func AddFirewallRule(context.Context, string) error {
	return ErrUnsupported
}

func HasWebView2() bool {
	return false
}

func RemoveDirAfterExit(string) error {
	return ErrUnsupported
}

func StartDetached(exePath string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), exePath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", exePath, err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release %s process: %w", exePath, err)
	}
	return nil
}

func IsConnectionRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}
