//go:build !windows

package tun

import "os/exec"

// hideWindow does nothing here: only Windows gives a console child a window
// of its own.
func hideWindow(c *exec.Cmd) {}
