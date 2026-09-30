package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

const utf8CodePage = 65001

var setConsoleOutputCP = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleOutputCP")

func isTerminal(f *os.File) bool {
	handle := windows.Handle(f.Fd())
	var mode uint32
	if windows.GetConsoleMode(handle, &mode) != nil {
		return false
	}
	if windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) != nil {
		return false
	}
	ok, _, err := setConsoleOutputCP.Call(utf8CodePage)
	return ok != 0 || errors.Is(err, windows.ERROR_SUCCESS)
}
