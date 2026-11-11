//go:build !darwin && !linux && !windows

package qr

import "errors"

func capture(string) ([]string, error) { return nil, errors.ErrUnsupported }
