//go:build windows

package tun

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: `mihomo -v` is a console program, and
// the app is a GUI one, so it would otherwise flash a window of its own.
const createNoWindow = 0x08000000

// hideWindow keeps a command's console off the screen.
func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
