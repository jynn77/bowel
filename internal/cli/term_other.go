//go:build !darwin && !linux && !windows

package cli

import (
	"errors"
	"os"
)

func termRaw(in, out *os.File) (func(), error) {
	return nil, errors.New("the terminal interface is not supported on this system")
}

func termSize(out *os.File) (w, h int) { return 80, 24 }
