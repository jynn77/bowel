//go:build !darwin && !linux && !windows

package tun

import (
	"errors"
	"path/filepath"
)

var errUnsupported = errors.New("not supported")

// Path is where the copy of the core that TUN runs on would be kept.
func Path(data, name string) string { return filepath.Join(data, "bin", name) }

// Installed is always false on systems bowel has no TUN for.
func Installed(path string) bool { return false }

// Uninstall has nothing to remove.
func Uninstall(dst string) error { return errUnsupported }

func stageDir(dst string) string               { return filepath.Dir(dst) }
func checkDir(string) error                    { return errUnsupported }
func installStep(string, string) (Step, error) { return nil, errUnsupported }
func runAdmin(string, []Step) error            { return errUnsupported }
