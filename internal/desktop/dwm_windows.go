//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	captionColor = 0x001E1E1E
	defaultDPI   = 96
	win32True    = 1
)

var errForegroundRefused = errors.New("foreground refused by the system")

type user32 struct {
	isIconic            *windows.LazyProc
	showWindow          *windows.LazyProc
	setForegroundWindow *windows.LazyProc
	destroyWindow       *windows.LazyProc
	getDpiForSystem     *windows.LazyProc
}

func loadUser32() user32 {
	dll := windows.NewLazySystemDLL("user32.dll")
	return user32{
		isIconic:            dll.NewProc("IsIconic"),
		showWindow:          dll.NewProc("ShowWindow"),
		setForegroundWindow: dll.NewProc("SetForegroundWindow"),
		destroyWindow:       dll.NewProc("DestroyWindow"),
		getDpiForSystem:     dll.NewProc("GetDpiForSystem"),
	}
}

func callWithoutLastError(proc *windows.LazyProc, args ...uintptr) uintptr {
	//nolint:errcheck
	result, _, _ := proc.Call(args...)
	return result
}

func (u user32) systemDPI() uint {
	if u.getDpiForSystem.Find() != nil {
		return defaultDPI
	}
	dpi := callWithoutLastError(u.getDpiForSystem)
	if dpi == 0 {
		return defaultDPI
	}
	return uint(dpi)
}

func scaleToDPI(length, dpi uint) uint {
	return (length*dpi + defaultDPI/2) / defaultDPI
}

func (u user32) bringToFront(hwnd windows.HWND) error {
	if callWithoutLastError(u.isIconic, uintptr(hwnd)) != 0 {
		callWithoutLastError(u.showWindow, uintptr(hwnd), windows.SW_RESTORE)
	}
	if callWithoutLastError(u.setForegroundWindow, uintptr(hwnd)) == 0 {
		return errForegroundRefused
	}
	return nil
}

func (u user32) destroy(hwnd windows.HWND) error {
	if !windows.IsWindow(hwnd) {
		return nil
	}
	if destroyed, _, err := u.destroyWindow.Call(uintptr(hwnd)); destroyed == 0 {
		return fmt.Errorf("destroy window: %w", err)
	}
	return nil
}

func setDWMAttribute(hwnd windows.HWND, attribute, value uint32) error {
	//nolint:gosec
	return windows.DwmSetWindowAttribute(hwnd, attribute, unsafe.Pointer(&value), uint32(unsafe.Sizeof(value)))
}

func useDarkTitleBar(hwnd windows.HWND) error {
	if err := setDWMAttribute(hwnd, windows.DWMWA_USE_IMMERSIVE_DARK_MODE, win32True); err != nil {
		return fmt.Errorf("set immersive dark mode: %w", err)
	}
	return nil
}

func paintCaption(hwnd windows.HWND) error {
	if err := setDWMAttribute(hwnd, windows.DWMWA_CAPTION_COLOR, captionColor); err != nil {
		return fmt.Errorf("set caption color: %w", err)
	}
	return nil
}
