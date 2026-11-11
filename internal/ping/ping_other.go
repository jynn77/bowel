//go:build !darwin && !linux && !windows

package ping

import (
	"context"
	"errors"
	"time"
)

// ICMP is not available on systems bowel has no ping command for.
func ICMP(context.Context, string, string, time.Duration) (int, error) {
	return 0, errors.New("ping: not supported")
}
