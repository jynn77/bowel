package main

import (
	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/tun"
)

// spawnTun starts the core the way TUN's installation needs where that is
// not as a child of the app: on Windows, through the scheduled task that
// runs the protected copy elevated. For any other core, and on the other
// systems always, it starts nothing and the manager does.
func spawnTun(bin string) (core.Process, error) {
	t, err := tun.Spawn(bin)
	if t == nil {
		return nil, err
	}
	return t, err
}
