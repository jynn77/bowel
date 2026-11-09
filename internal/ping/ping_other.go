//go:build !darwin && !windows

package ping

import (
	"context"
	"errors"
	"time"
)

// ICMP is not available outside macOS.
func ICMP(context.Context, string, string, time.Duration) (int, error) {
	return 0, errors.New("ping: not supported")
}
