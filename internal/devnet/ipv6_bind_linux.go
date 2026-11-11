package devnet

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// bindToIface makes a socket leave by iface, past the routes and rules
// of a TUN (SO_BINDTODEVICE, which since Linux 5.7 any process may set
// on a socket of its own).
func bindToIface(iface string) func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = unix.BindToDevice(int(fd), iface)
		}); err != nil {
			return err
		}
		return serr
	}
}
