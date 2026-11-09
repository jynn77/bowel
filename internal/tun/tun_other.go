//go:build !darwin && !windows

// Package tun installs the core so that it can open a TUN device.
package tun

import (
	"errors"

	"github.com/oboo/moho/internal/i18n"
)

// ErrCanceled is returned by Install and Uninstall when the user dismissed
// the administrator prompt.
var ErrCanceled error = canceled{}

// canceled says it in the interface's language when it is said.
type canceled struct{}

func (canceled) Error() string {
	return i18n.T("用户取消了授权", "The authorization was canceled")
}

var errUnsupported = errors.New("not supported")

// Installed is always false outside macOS.
func Installed(path string) bool { return false }

// Install is not supported outside macOS.
func Install(src, dst string) error { return errUnsupported }

// Uninstall is not supported outside macOS.
func Uninstall(dst string) error { return errUnsupported }

// Step is one shell command run as root.
type Step []string

// Prompt is what the administrator dialog says for TUN alone.
func Prompt() string {
	return i18n.T("bowel 需要授权以启用 TUN 模式", "bowel needs authorization to turn on TUN mode")
}

// InstallStep is not supported outside macOS.
func InstallStep(src, dst string) (Step, error) { return nil, errUnsupported }

// Admin is not supported outside macOS.
func Admin(prompt string, steps ...Step) error { return errUnsupported }

// Reset has nothing to do where TUN cannot run.
func Reset() error { return nil }
