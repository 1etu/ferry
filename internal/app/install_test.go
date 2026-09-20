package app

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/1etu/ferry/internal/config"
)

func TestSelfInstallOnlyForReleaseBuildsOnWindows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		env     config.Env
		version string
		goos    string
		want    bool
	}{
		{"release on windows", config.Env{}, "1.0.0", "windows", true},
		{"dev build", config.Env{}, "dev", "windows", false},
		{"headless", config.Env{Headless: true}, "1.0.0", "windows", false},
		{"dev mode", config.Env{Dev: true}, "1.0.0", "windows", false},
		{"linux", config.Env{}, "1.0.0", "linux", false},
	}
	for _, tc := range tests {
		if got := needsSelfInstall(tc.env, tc.version, tc.goos); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

type installCalls struct {
	steps []string
}

func (c *installCalls) installer(isInstalled, isRunning bool, installErr error) installer {
	return installer{
		isInstalled: func(string) (bool, error) { c.steps = append(c.steps, "check"); return isInstalled, nil },
		install: func(exe, _ string) (string, error) {
			c.steps = append(c.steps, "install "+exe)
			return `C:\Programs\Ferry\Ferry.exe`, installErr
		},
		start: func(exe string, _ ...string) error { c.steps = append(c.steps, "start "+exe); return nil },
		isRunning: func(context.Context) bool {
			c.steps = append(c.steps, "probe")
			return isRunning
		},
		show: func(context.Context) error { c.steps = append(c.steps, "show"); return nil },
	}
}

func TestSelfInstall(t *testing.T) {
	t.Parallel()
	const downloaded = `C:\Users\me\Downloads\Ferry.exe`
	tests := []struct {
		name        string
		isInstalled bool
		isRunning   bool
		installErr  error
		code        int
		isDone      bool
		steps       []string
	}{
		{"installed copy keeps running", true, false, nil, exitOK, false, []string{"check"}},
		{"running instance shows its window", false, true, nil, exitOK, true, []string{"check", "probe", "show"}},
		{"first run installs and starts the copy", false, false, nil, exitOK, true, []string{"check", "probe", "install " + downloaded, `start C:\Programs\Ferry\Ferry.exe`}},
		{"failed install starts nothing", false, false, errors.New("disk full"), exitFailure, true, []string{"check", "probe", "install " + downloaded}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &machine{exePath: downloaded, log: slog.New(slog.DiscardHandler)}
			calls := &installCalls{}
			code, isDone := m.selfInstall(t.Context(), calls.installer(tc.isInstalled, tc.isRunning, tc.installErr))
			if code != tc.code || isDone != tc.isDone {
				t.Fatalf("got %d %v, want %d %v", code, isDone, tc.code, tc.isDone)
			}
			if diff := cmp.Diff(tc.steps, calls.steps); diff != "" {
				t.Fatalf("steps (-want +got):\n%s", diff)
			}
		})
	}
}
