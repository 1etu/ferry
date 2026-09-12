package platform

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskFlagNoUI       = 0x00000400
	elevatedPollMillis    = 250
)

type shellExecuteInfo struct {
	size          uint32
	mask          uint32
	window        windows.Handle
	verb          *uint16
	file          *uint16
	parameters    *uint16
	directory     *uint16
	show          int32
	instApp       windows.Handle
	idList        uintptr
	class         *uint16
	classKey      windows.Handle
	hotKey        uint32
	iconOrMonitor windows.Handle
	process       windows.Handle
}

func FirewallState(ctx context.Context, exePath string) (Firewall, error) {
	return checkFirewall(ctx, exePath, runPowerShell)
}

func AddFirewallRule(ctx context.Context, exePath string) error {
	process, err := startElevated("netsh.exe", firewallRuleArgs(exePath))
	if err != nil {
		return fmt.Errorf("add firewall rule for %s: %w", exePath, err)
	}
	exitCode, err := waitProcess(ctx, process)
	if err = errors.Join(err, windows.CloseHandle(process)); err != nil {
		return fmt.Errorf("add firewall rule for %s: %w", exePath, err)
	}
	if exitCode != 0 {
		return fmt.Errorf("add firewall rule for %s: netsh exited with code %d", exePath, exitCode)
	}
	return nil
}

func startElevated(file, parameters string) (windows.Handle, error) {
	shellExecuteEx := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")
	if err := shellExecuteEx.Find(); err != nil {
		return 0, fmt.Errorf("find ShellExecuteExW: %w", err)
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return 0, fmt.Errorf("encode verb: %w", err)
	}
	filePtr, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return 0, fmt.Errorf("encode %s: %w", file, err)
	}
	parametersPtr, err := windows.UTF16PtrFromString(parameters)
	if err != nil {
		return 0, fmt.Errorf("encode parameters: %w", err)
	}
	info := shellExecuteInfo{
		mask:       seeMaskNoCloseProcess | seeMaskFlagNoUI,
		verb:       verb,
		file:       filePtr,
		parameters: parametersPtr,
		show:       windows.SW_HIDE,
	}
	info.size = uint32(unsafe.Sizeof(info))
	//nolint:gosec
	ok, _, callErr := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		//nolint:misspell
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return 0, ErrCanceled
		}
		return 0, fmt.Errorf("run %s elevated: %w", file, callErr)
	}
	if info.process == 0 {
		return 0, fmt.Errorf("run %s elevated: no process handle", file)
	}
	return info.process, nil
}

func waitProcess(ctx context.Context, process windows.Handle) (uint32, error) {
	for {
		event, err := windows.WaitForSingleObject(process, elevatedPollMillis)
		if err != nil {
			return 0, fmt.Errorf("wait for process: %w", err)
		}
		if event == windows.WAIT_OBJECT_0 {
			break
		}
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("wait for process: %w", err)
		}
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process, &exitCode); err != nil {
		return 0, fmt.Errorf("read exit code: %w", err)
	}
	return exitCode, nil
}
