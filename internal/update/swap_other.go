//go:build !windows

package update

import (
	"os"
	"path/filepath"
	"syscall"
)

func launchDetached(exePath string, args ...string) (*os.Process, error) {
	return os.StartProcess(exePath, append([]string{exePath}, args...), &os.ProcAttr{
		Dir: filepath.Dir(exePath),
		Sys: &syscall.SysProcAttr{Setsid: true},
	})
}
