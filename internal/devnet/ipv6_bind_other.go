//go:build !darwin && !linux && !windows

package devnet

import "syscall"

// bindToIface does nothing on systems bowel runs no TUN on: the system's
// route is the one traffic takes.
func bindToIface(string) func(network, address string, c syscall.RawConn) error { return nil }
