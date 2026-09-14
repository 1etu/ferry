//go:build windows

package update

import (
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

func launchDetached(exePath string, args ...string) (*os.Process, error) {
	return os.StartProcess(exePath, append([]string{exePath}, args...), &os.ProcAttr{
		Dir: filepath.Dir(exePath),
		Sys: &syscall.SysProcAttr{
			CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		},
	})
}
