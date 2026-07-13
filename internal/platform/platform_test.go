package platform

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestNormalizeHostname(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "windows computer name is lowercased", in: "DESKTOP-4F2K9QX", want: "desktop-4f2k9qx"},
		{name: "fully qualified name keeps the first label", in: "Studio.corp.example.com", want: "studio"},
		{name: "mdns name keeps the first label", in: "macbook.local", want: "macbook"},
		{name: "surrounding whitespace is dropped", in: " laptop \n", want: "laptop"},
		{name: "already normalized", in: "pc", want: "pc"},
		{name: "leading dot leaves nothing", in: ".local", want: ""},
		{name: "empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeHostname(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHostnameIsNormalized(t *testing.T) {
	t.Parallel()
	got, err := Hostname()
	if err != nil {
		t.Fatalf("hostname: %v", err)
	}
	if got != normalizeHostname(got) || got == "" {
		t.Fatalf("got %q, want a non-empty normalized label", got)
	}
}

func TestFreeSpaceOfTempDirIsPositive(t *testing.T) {
	t.Parallel()
	got, err := FreeSpace(t.TempDir())
	if err != nil {
		t.Fatalf("free space: %v", err)
	}
	if got == 0 {
		t.Fatal("got 0 free bytes, want more")
	}
}

func TestFreeSpaceOfMissingDirFails(t *testing.T) {
	t.Parallel()
	if _, err := FreeSpace(filepath.Join(t.TempDir(), "missing", "child")); err == nil {
		t.Fatal("got nil error, want one")
	}
}

func TestDefaultDirsEndInAppName(t *testing.T) {
	t.Parallel()
	for name, resolve := range map[string]func() (string, error){
		"data":     DataDir,
		"received": DefaultReceivedDir,
	} {
		dir, err := resolve()
		if err != nil {
			t.Fatalf("%s dir: %v", name, err)
		}
		if got := filepath.Base(dir); got != appDirName {
			t.Fatalf("%s dir %q ends in %q, want %q", name, dir, got, appDirName)
		}
	}
}

func TestIsConnectionRefused(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	closedAddr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	var dialer net.Dialer
	conn, refused := dialer.DialContext(t.Context(), "tcp", closedAddr)
	if refused == nil {
		closeErr := conn.Close()
		t.Fatalf("dial of a closed port succeeded, close: %v", closeErr)
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "dial of a closed port is refused", err: refused, want: true},
		{name: "a timeout is not refused", err: context.DeadlineExceeded, want: false},
		{name: "another error is not refused", err: errors.New("connection reset"), want: false},
		{name: "no error is not refused", err: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsConnectionRefused(tt.err); got != tt.want {
				t.Fatalf("got %v for %v, want %v", got, tt.err, tt.want)
			}
		})
	}
}

func TestCheckFirewall(t *testing.T) {
	t.Parallel()
	const exe = `C:\Users\me\AppData\Local\Programs\Ferry\Ferry.exe`
	tests := []struct {
		name   string
		output string
		want   Firewall
	}{
		{"allow covering the active private network", "category\tPrivate\nrule\tInbound\tAllow\tTrue\tPrivate, Public\n", Firewall{ProfilePrivate, true}},
		{"prompt rules that block public", "category\tPublic\r\nrule\tInbound\tBlock\tTrue\tPublic\r\nrule\tInbound\tBlock\tTrue\tPublic\r\n", Firewall{ProfilePublic, false}},
		{"public wins the profile when private is also active", "category\tPrivate\ncategory\tPublic\nrule\tInbound\tAllow\tTrue\tPrivate, Public\n", Firewall{ProfilePublic, true}},
		{"allow must cover every active profile", "category\tPrivate\ncategory\tPublic\nrule\tInbound\tAllow\tTrue\tPublic\n", Firewall{ProfilePublic, false}},
		{"any covers a domain network", "category\tDomainAuthenticated\nrule\tInbound\tAllow\tTrue\tAny\n", Firewall{ProfileUnknown, true}},
		{"domain network needs a domain rule", "category\tDomainAuthenticated\nrule\tInbound\tAllow\tTrue\tPrivate, Public\n", Firewall{ProfileUnknown, false}},
		{"disabled allow does not count", "category\tPrivate\nrule\tInbound\tAllow\tFalse\tAny\n", Firewall{ProfilePrivate, false}},
		{"outbound allow does not count", "category\tPrivate\nrule\tOutbound\tAllow\tTrue\tAny\n", Firewall{ProfilePrivate, false}},
		{"block on an active profile outranks allow", "category\tPrivate\nrule\tInbound\tAllow\tTrue\tAny\nrule\tInbound\tBlock\tTrue\tPrivate\n", Firewall{ProfilePrivate, false}},
		{"block on an inactive profile is ignored", "category\tPrivate\nrule\tInbound\tAllow\tTrue\tPrivate\nrule\tInbound\tBlock\tTrue\tPublic\n", Firewall{ProfilePrivate, true}},
		{"disabled block is ignored", "category\tPublic\nrule\tInbound\tAllow\tTrue\tPublic\nrule\tInbound\tBlock\tFalse\tPublic\n", Firewall{ProfilePublic, true}},
		{"no rules is blocked", "category\tPublic\n", Firewall{ProfilePublic, false}},
		{"no active network with an allow rule is allowed", "rule\tInbound\tAllow\tTrue\tPrivate\n", Firewall{ProfileUnknown, true}},
		{"empty output is unknown and blocked", "", Firewall{ProfileUnknown, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			run := func(_ context.Context, script string, env ...string) ([]byte, error) {
				if script != firewallScript || !slices.Equal(env, []string{"FERRY_FIREWALL_PROGRAM=" + exe}) {
					t.Errorf("ran an unexpected script or env %q", env)
				}
				return []byte(tt.output), nil
			}
			got, err := checkFirewall(t.Context(), exe, run)
			if err != nil {
				t.Fatalf("check firewall: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCheckFirewallFailuresAreUnknown(t *testing.T) {
	t.Parallel()
	errPowerShell := errors.New("powershell failed")
	tests := []struct {
		name   string
		output string
		err    error
	}{
		{name: "runner error is returned", err: errPowerShell},
		{name: "unknown line kind is rejected", output: "category\tPrivate\nwarning\tsomething\n"},
		{name: "rule with missing fields is rejected", output: "rule\tInbound\tAllow\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			run := func(context.Context, string, ...string) ([]byte, error) { return []byte(tt.output), tt.err }
			got, err := checkFirewall(t.Context(), `C:\Ferry.exe`, run)
			if err == nil || (tt.err != nil && !errors.Is(err, tt.err)) {
				t.Fatalf("got error %v, want one wrapping %v", err, tt.err)
			}
			if got != (Firewall{Profile: ProfileUnknown}) {
				t.Fatalf("got %+v, want unknown and not allowed", got)
			}
		})
	}
}

func TestFirewallRuleArgsCoverPrivateAndPublic(t *testing.T) {
	t.Parallel()
	got := firewallRuleArgs(`C:\Users\Ada Lovelace\Ferry.exe`)
	want := `advfirewall firewall add rule name="Ferry" dir=in action=allow program="C:\Users\Ada Lovelace\Ferry.exe" enable=yes profile=private,public`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestIsWebView2Version(t *testing.T) {
	t.Parallel()
	for pv, want := range map[string]bool{"141.0.3537.71": true, "0.0.0.0": false, "": false, "  ": false} {
		if got := isWebView2Version(pv); got != want {
			t.Fatalf("got %v for %q, want %v", got, pv, want)
		}
	}
}

func TestInstallDirIsProgramsFerry(t *testing.T) {
	t.Parallel()
	dir, err := InstallDir()
	if errors.Is(err, ErrUnsupported) {
		t.Skip("install dir is Windows only")
	}
	if err != nil {
		t.Fatalf("install dir: %v", err)
	}
	if got := filepath.Join(filepath.Base(filepath.Dir(dir)), filepath.Base(dir)); got != filepath.Join("Programs", appDirName) {
		t.Fatalf("install dir %q ends in %q", dir, got)
	}
}

func TestWriteShortcutCreatesFolderAndIsRepeatable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "Ferry.exe")
	if err := os.WriteFile(target, []byte("MZ"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "Start Menu", "Programs", "Ferry.lnk")
	for range 2 {
		err := WriteShortcut(link, target, "send", target)
		if errors.Is(err, ErrUnsupported) {
			t.Skip("shortcuts are Windows only")
		}
		if err != nil {
			t.Fatalf("write shortcut: %v", err)
		}
	}
	if _, err := os.Stat(link); err != nil {
		t.Fatalf("stat shortcut: %v", err)
	}
}

func TestRemoveDirAfterExitRejectsRelativePath(t *testing.T) {
	t.Parallel()
	if err := RemoveDirAfterExit(filepath.Join("relative", "Ferry")); err == nil {
		t.Fatal("got nil error, want one")
	}
}

func TestRemoveDirAfterExitDeletesTheTreeAfterADelay(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "Tools & 100%", "Ferry")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "Ferry.exe"), []byte("MZ"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := RemoveDirAfterExit(dir)
	if errors.Is(err, ErrUnsupported) {
		t.Skip("deferred removal is Windows only")
	}
	if err != nil {
		t.Fatalf("remove after exit: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("dir is gone before the delay: %v", err)
	}
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			return
		}
	}
	t.Fatalf("%s still exists after 20 s", dir)
}
