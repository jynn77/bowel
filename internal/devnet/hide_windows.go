//go:build windows

package devnet

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW. The app is a GUI program, so the
// Tailscale CLI — read again every two minutes — would otherwise be given a
// console window of its own, one that flashes and goes.
const createNoWindow = 0x08000000

// hideWindow keeps a command's console off the screen.
func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
