package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const dialogHostScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
Add-Type -AssemblyName System.Windows.Forms
$owner = New-Object System.Windows.Forms.Form -Property @{
	TopMost = $true
	ShowInTaskbar = $false
	FormBorderStyle = 'None'
	StartPosition = 'CenterScreen'
	Width = 1
	Height = 1
	Opacity = 0
}
$owner.Show()
$owner.Activate()
`

const pickFilesScript = dialogHostScript + `$dialog = New-Object System.Windows.Forms.OpenFileDialog
$dialog.Multiselect = $true
$dialog.Title = $env:FERRY_PICK_TITLE
if ($dialog.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) { $dialog.FileNames }
$owner.Dispose()
`

const pickFolderScript = dialogHostScript + `$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.Description = $env:FERRY_PICK_TITLE
$dialog.ShowNewFolderButton = $true
if ($dialog | Get-Member -Name UseDescriptionForTitle) { $dialog.UseDescriptionForTitle = $true }
if ($dialog.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) { $dialog.SelectedPath }
$owner.Dispose()
`

const writeShortcutScript = `$ErrorActionPreference = 'Stop'
$shell = New-Object -ComObject WScript.Shell
$link = $shell.CreateShortcut($env:FERRY_LINK_PATH)
$link.TargetPath = $env:FERRY_LINK_TARGET
$link.Arguments = $env:FERRY_LINK_ARGS
$link.IconLocation = $env:FERRY_LINK_ICON + ',0'
$link.WorkingDirectory = Split-Path -Parent $env:FERRY_LINK_TARGET
$link.Save()
`

const removeDirScript = `ping -n 4 127.0.0.1 >nul & rd /s /q "%FERRY_REMOVE_DIR%"`

func DataDir() (string, error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", fmt.Errorf("resolve local app data folder: %w", err)
	}
	return filepath.Join(dir, appDirName), nil
}

func InstallDir() (string, error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", fmt.Errorf("resolve local app data folder: %w", err)
	}
	return filepath.Join(dir, "Programs", appDirName), nil
}

func DefaultReceivedDir() (string, error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, 0)
	if err != nil {
		return "", fmt.Errorf("resolve downloads folder: %w", err)
	}
	return filepath.Join(dir, appDirName), nil
}

func FreeSpace(dir string) (uint64, error) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, fmt.Errorf("encode path %s: %w", dir, err)
	}
	var availableToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(path, &availableToCaller, &total, &totalFree); err != nil {
		return 0, fmt.Errorf("query free space of %s: %w", dir, err)
	}
	return availableToCaller, nil
}

func OpenURL(url string) error {
	if err := exec.CommandContext(context.Background(), "rundll32", "url.dll,FileProtocolHandler", url).Run(); err != nil {
		return fmt.Errorf("open url %s: %w", url, err)
	}
	return nil
}

func OpenFolder(path string) error {
	return startExplorer(path)
}

func RevealFile(path string) error {
	return startExplorer("/select," + path)
}

func startExplorer(arg string) error {
	cmd := exec.CommandContext(context.Background(), "explorer.exe", arg)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start explorer %s: %w", arg, err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release explorer process: %w", err)
	}
	return nil
}

func PickFiles(ctx context.Context, title string) ([]string, error) {
	out, err := runPowerShell(ctx, pickFilesScript, "FERRY_PICK_TITLE="+title)
	if err != nil {
		return nil, fmt.Errorf("pick files: %w", err)
	}
	var paths []string
	for line := range strings.Lines(string(out)) {
		if path := strings.TrimRight(line, "\r\n"); path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func PickFolder(ctx context.Context, title string) (string, error) {
	out, err := runPowerShell(ctx, pickFolderScript, "FERRY_PICK_TITLE="+title)
	if err != nil {
		return "", fmt.Errorf("pick folder: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func WriteShortcut(linkPath, target, args, iconPath string) error {
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o750); err != nil {
		return fmt.Errorf("create shortcut folder for %s: %w", linkPath, err)
	}
	_, err := runPowerShell(context.Background(), writeShortcutScript,
		"FERRY_LINK_PATH="+linkPath,
		"FERRY_LINK_TARGET="+target,
		"FERRY_LINK_ARGS="+args,
		"FERRY_LINK_ICON="+iconPath,
	)
	if err != nil {
		return fmt.Errorf("write shortcut %s: %w", linkPath, err)
	}
	return nil
}

func InstallSendTo(exePath string) error {
	sendToDir, err := windows.KnownFolderPath(windows.FOLDERID_SendTo, 0)
	if err != nil {
		return fmt.Errorf("resolve send to folder: %w", err)
	}
	return WriteShortcut(filepath.Join(sendToDir, SendToLinkName), exePath, "send", exePath)
}

func RemoveDirAfterExit(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("remove %s after exit: path is not absolute", dir)
	}
	cmd := exec.CommandContext(context.Background(), "cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       "cmd.exe /d /c " + removeDirScript,
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP,
	}
	cmd.Env = append(os.Environ(), "FERRY_REMOVE_DIR="+filepath.Clean(dir))
	cmd.Dir = filepath.Dir(filepath.Clean(dir))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start removal of %s: %w", dir, err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release removal process for %s: %w", dir, err)
	}
	return nil
}

func runPowerShell(ctx context.Context, script string, env ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run powershell: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func StartDetached(exePath string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), exePath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", exePath, err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release %s process: %w", exePath, err)
	}
	return nil
}

func IsConnectionRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED)
}
