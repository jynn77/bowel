package qr

import (
	"os/exec"
	"path/filepath"
)

// capture takes a picture of every display with screencapture, which
// writes one file per display, in the order given, and leaves the rest.
func capture(dir string) ([]string, error) {
	args := []string{"-x", "-t", "png"}
	for i := range maxDisplays {
		args = append(args, filepath.Join(dir, string(rune('a'+i))+".png"))
	}
	err := exec.Command("/usr/sbin/screencapture", args...).Run()
	return args[3:], err
}
