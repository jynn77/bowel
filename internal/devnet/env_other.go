//go:build !darwin && !linux && !windows

package devnet

import "context"

func osDetect(context.Context) ([]Route, []Resolver) { return nil, nil }
