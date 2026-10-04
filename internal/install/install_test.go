//go:build windows

package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/1etu/ferry/internal/platform"
)

type fixture struct {
	l          layout
	root       string
	downloaded string
	removals   *[]string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := longPath(t, t.TempDir())
	roaming := filepath.Join(root, "Roaming", "Microsoft", "Windows")
	keyPath := fmt.Sprintf(`Software\FerryTest\install-%d`, time.Now().UnixNano())
	t.Cleanup(func() { deleteTestKeys(t, keyPath) })
	var removals []string
	f := fixture{
		l: layout{
			installDir:    filepath.Join(root, "Local", "Programs", "Ferry"),
			startMenuLink: filepath.Join(roaming, "Start Menu", "Programs", startMenuLinkName),
			sendToLink:    filepath.Join(roaming, "SendTo", platform.SendToLinkName),
			registry:      platform.RegistryRoot(keyPath),
			removeDirAfterExit: func(dir string) error {
				removals = append(removals, dir)
				return nil
			},
		},
		root:       root,
		downloaded: filepath.Join(root, "Downloads", "Ferry.exe"),
		removals:   &removals,
	}
	writeFile(t, f.downloaded, "MZ downloaded build")
	return f
}

func TestInstallCopiesTheExeAndRegistersFerry(t *testing.T) {
	f := newFixture(t)
	exe, err := f.l.install(f.downloaded, "1.2.3")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if want := filepath.Join(f.l.installDir, "Ferry.exe"); exe != want {
		t.Fatalf("got installed exe %s, want %s", exe, want)
	}
	assertInstalled(t, f, exe, "1.2.3")
}

func TestInstallIsIdempotentEvenFromTheInstalledCopy(t *testing.T) {
	f := newFixture(t)
	for _, source := range []string{f.downloaded, f.downloaded, filepath.Join(f.l.installDir, "Ferry.exe")} {
		exe, err := f.l.install(source, "1.2.3")
		if err != nil {
			t.Fatalf("install from %s: %v", source, err)
		}
		assertInstalled(t, f, exe, "1.2.3")
	}
}

func TestInstallReplacesAnOlderCopyAndAStaleTemporary(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.l.installDir, "Ferry.exe"), "MZ old build")
	writeFile(t, filepath.Join(f.l.installDir, "Ferry.exe.tmp"), "MZ interrupted copy")
	exe, err := f.l.install(f.downloaded, "2.0.0")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	assertInstalled(t, f, exe, "2.0.0")
}

func TestIsInstalled(t *testing.T) {
	f := newFixture(t)
	installed := filepath.Join(f.l.installDir, "Ferry.exe")
	tests := []struct {
		name, exePath            string
		install, want, wantError bool
	}{
		{name: "the installed exe is installed", exePath: installed, install: true, want: true},
		{name: "letter case does not matter", exePath: strings.ToUpper(installed), install: true, want: true},
		{name: "the downloaded copy is not installed", exePath: f.downloaded, install: true},
		{name: "nothing installed yet", exePath: f.downloaded},
		{name: "a missing running exe is an error", exePath: filepath.Join(f.root, "gone.exe"), install: true, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.install {
				writeFile(t, installed, "MZ")
			} else if err := os.RemoveAll(f.l.installDir); err != nil {
				t.Fatalf("remove install dir: %v", err)
			}
			got, err := f.l.isInstalled(tt.exePath)
			if (err != nil) != tt.wantError || got != tt.want {
				t.Fatalf("got %v, %v, want %v and error %v", got, err, tt.want, tt.wantError)
			}
		})
	}
}

func TestUninstallRemovesEverythingButReceivedFiles(t *testing.T) {
	f := newFixture(t)
	if _, err := f.l.install(f.downloaded, "1.2.3"); err != nil {
		t.Fatalf("install: %v", err)
	}
	dataDir := filepath.Join(f.root, "Local", "Ferry")
	writeFile(t, filepath.Join(dataDir, "logs", "ferry.log"), "log")
	received := filepath.Join(f.root, "Downloads", "Ferry", "photo.heic")
	writeFile(t, received, "photo")
	quits := 0
	quit := func(context.Context) error { quits++; return nil }
	if err := f.l.uninstall(t.Context(), dataDir, quit); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if quits != 1 {
		t.Fatalf("got %d quit calls, want 1", quits)
	}
	for _, gone := range []string{f.l.startMenuLink, f.l.sendToLink, dataDir} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists: %v", gone, err)
		}
	}
	assertFile(t, received, "photo")
	assertUninstalledRegistry(t, f)
	if diff := cmp.Diff([]string{f.l.installDir}, *f.removals); diff != "" {
		t.Fatalf("scheduled removals (-want +got):\n%s", diff)
	}
}

func TestUninstallWithNothingInstalledSucceeds(t *testing.T) {
	f := newFixture(t)
	if err := f.l.uninstall(t.Context(), filepath.Join(f.root, "Local", "Ferry"), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	assertUninstalledRegistry(t, f)
	if err := f.l.uninstall(t.Context(), "Ferry", func(context.Context) error { return nil }); err == nil {
		t.Fatal("got nil error for a relative data dir, want one")
	}
}

func TestUninstallStopsWhenTheRunningInstanceDoesNotQuit(t *testing.T) {
	f := newFixture(t)
	if _, err := f.l.install(f.downloaded, "1.2.3"); err != nil {
		t.Fatalf("install: %v", err)
	}
	dataDir := filepath.Join(f.root, "Local", "Ferry")
	writeFile(t, filepath.Join(dataDir, "config.json"), "{}")
	errStillRunning := errors.New("port still open")
	err := f.l.uninstall(t.Context(), dataDir, func(context.Context) error { return errStillRunning })
	if !errors.Is(err, errStillRunning) {
		t.Fatalf("got %v, want %v", err, errStillRunning)
	}
	assertFile(t, filepath.Join(dataDir, "config.json"), "{}")
	if on, err := f.l.registry.RunAtLogin(); err != nil || !on {
		t.Fatalf("got run at login %v, %v, want it kept", on, err)
	}
	if len(*f.removals) != 0 {
		t.Fatalf("got removals %v, want none", *f.removals)
	}
}

func assertInstalled(t *testing.T, f fixture, exe, version string) {
	t.Helper()
	assertFile(t, exe, "MZ downloaded build")
	if _, err := os.Stat(exe + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary copy left behind: %v", err)
	}
	if diff := cmp.Diff([]string{exe + "|", exe + "|send"}, readShortcuts(t, f.l.startMenuLink, f.l.sendToLink)); diff != "" {
		t.Fatalf("shortcuts (-want +got):\n%s", diff)
	}
	wantRun := map[string]any{"Ferry": `"` + exe + `" --hidden`}
	if diff := cmp.Diff(wantRun, readValues(t, string(f.l.registry)+`\Run`)); diff != "" {
		t.Fatalf("run key (-want +got):\n%s", diff)
	}
	wantEntry := map[string]any{
		"DisplayName":     "Ferry",
		"DisplayVersion":  version,
		"DisplayIcon":     exe + ",0",
		"Publisher":       "Ferry",
		"InstallLocation": f.l.installDir,
		"UninstallString": `"` + exe + `" uninstall`,
		"NoModify":        uint64(1),
		"NoRepair":        uint64(1),
		"EstimatedSize":   uint64(1),
	}
	if diff := cmp.Diff(wantEntry, readValues(t, string(f.l.registry)+`\Uninstall\Ferry`)); diff != "" {
		t.Fatalf("uninstall entry (-want +got):\n%s", diff)
	}
}

func assertUninstalledRegistry(t *testing.T, f fixture) {
	t.Helper()
	if on, err := f.l.registry.RunAtLogin(); err != nil || on {
		t.Fatalf("got run at login %v, %v, want off", on, err)
	}
	_, err := registry.OpenKey(registry.CURRENT_USER, string(f.l.registry)+`\Uninstall\Ferry`, registry.QUERY_VALUE)
	if !errors.Is(err, registry.ErrNotExist) {
		t.Fatalf("uninstall entry still present: %v", err)
	}
}

func readValues(t *testing.T, path string) map[string]any {
	t.Helper()
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer key.Close()
	names, err := key.ReadValueNames(-1)
	if err != nil {
		t.Fatalf("list values of %s: %v", path, err)
	}
	values := make(map[string]any, len(names))
	for _, name := range names {
		if text, _, err := key.GetStringValue(name); err == nil {
			values[name] = text
			continue
		}
		number, _, err := key.GetIntegerValue(name)
		if err != nil {
			t.Fatalf("read %s value %s: %v", path, name, err)
		}
		values[name] = number
	}
	return values
}

func readShortcuts(t *testing.T, paths ...string) []string {
	t.Helper()
	const script = `$shell = New-Object -ComObject WScript.Shell
foreach ($path in $env:FERRY_LINKS.Split('|')) { $link = $shell.CreateShortcut($path); $link.TargetPath + '|' + $link.Arguments }`
	cmd := exec.CommandContext(t.Context(), "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "FERRY_LINKS="+strings.Join(paths, "|"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("read shortcuts: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\r\n")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("got %q, %v from %s, want %q", got, err, path, want)
	}
}

func deleteTestKeys(t *testing.T, keyPath string) {
	t.Helper()
	for _, path := range []string{keyPath + `\Uninstall\Ferry`, keyPath + `\Uninstall`, keyPath + `\Run`, keyPath} {
		if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil && !errors.Is(err, registry.ErrNotExist) {
			t.Errorf("delete test key %s: %v", path, err)
		}
	}
	parent, err := registry.OpenKey(registry.CURRENT_USER, filepath.Dir(keyPath), registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return
	}
	if err != nil {
		t.Errorf("open test key parent: %v", err)
		return
	}
	info, err := parent.Stat()
	if err = errors.Join(err, parent.Close()); err != nil {
		t.Errorf("stat test key parent: %v", err)
		return
	}
	if info.SubKeyCount == 0 && info.ValueCount == 0 {
		if err := registry.DeleteKey(registry.CURRENT_USER, filepath.Dir(keyPath)); err != nil {
			t.Errorf("delete test key parent: %v", err)
		}
	}
}

func longPath(t *testing.T, path string) string {
	t.Helper()
	short, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetLongPathName(short, &buf[0], uint32(len(buf)))
	if err != nil {
		t.Fatalf("resolve long path of %s: %v", path, err)
	}
	return windows.UTF16ToString(buf[:n])
}
