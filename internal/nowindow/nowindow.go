// Package nowindow starts other programs without a console window.
//
// bowel is a GUI process on Windows and has no console of its own, so
// Windows opens a new, visible one for every console program it starts,
// for as long as that program runs: a window that flashes on screen each
// time something is probed. Every program bowel starts is started through
// this package, which asks Windows not to do that. On the other systems
// it is os/exec and nothing more.
//
// A test in this package reads the source tree and fails when a file that
// builds for Windows calls exec.Command or exec.CommandContext itself.
package nowindow

import (
	"context"
	"os/exec"
)

// Command is exec.Command for a program that opens no console window.
func Command(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	Hide(cmd)
	return cmd
}

// CommandContext is exec.CommandContext for a program that opens no
// console window.
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	Hide(cmd)
	return cmd
}
