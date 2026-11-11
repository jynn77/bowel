//go:build !darwin && !windows && !linux

package sysproxy

import (
	"context"
	"errors"

	"github.com/oboo/moho/internal/i18n"
)

// DefaultBypass is the bypass list a service gets back when Forget clears
// one that was all Moho's.
var DefaultBypass []string

func Enable(host string, port int, bypass []string) error {
	return errors.New(i18n.T("这个系统上还不能设置系统代理", "The system proxy can't be set on this system yet"))
}
func Disable() error { return nil }

// Snapshot and Restore have nothing to keep where Enable does nothing.
func Snapshot() (Saved, error)             { return Saved{}, nil }
func Restore(context.Context, Saved) error { return nil }
