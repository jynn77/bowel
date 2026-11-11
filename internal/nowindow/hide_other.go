//go:build !windows

package nowindow

import "os/exec"

// Hide keeps cmd from opening a console window on Windows. There is no
// such window on this system, and nothing to do.
func Hide(*exec.Cmd) {}
