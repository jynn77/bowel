//go:build windows

package devtools

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW. The app is a GUI program: a console
// child — git, ssh, a shell — would open a console window of its own, one
// that flashes and goes, and the tool list is read again every few minutes.
const createNoWindow = 0x08000000

// hideWindow keeps a command's console off the screen.
func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
