package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// attachConsole gives a command its terminal back. A release build is a
// GUI program (-H=windowsgui), which Windows starts without a console:
// run from cmd or PowerShell, its output would go nowhere. Output that
// was redirected (a pipe, a file, the MCP bridge's stdio) is left alone.
func attachConsole() {
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		return
	}
	const attachParent = ^uintptr(0) // ATTACH_PARENT_PROCESS
	if r, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole").Call(attachParent); r == 0 {
		return // not started from a console
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
		os.Stdin = f
	}
}
