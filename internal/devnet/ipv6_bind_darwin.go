package devnet

import (
	"net"
	"syscall"
)

// bindToIface makes a socket leave by iface, past the routes of a TUN.
func bindToIface(iface string) func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		i, err := net.InterfaceByName(iface)
		if err != nil {
			return err
		}
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_BOUND_IF, i.Index)
		}); err != nil {
			return err
		}
		return serr
	}
}
