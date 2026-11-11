package devnet

import (
	"net"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const (
	ipUnicastIf   = 31 // IP_UNICAST_IF
	ipv6UnicastIf = 31 // IPV6_UNICAST_IF
)

// bindToIface makes a socket leave by iface, past the routes of a TUN:
// IPV6_UNICAST_IF takes the interface's index as it is, IP_UNICAST_IF
// in network byte order.
func bindToIface(iface string) func(network, address string, c syscall.RawConn) error {
	return func(network, _ string, c syscall.RawConn) error {
		i, err := net.InterfaceByName(iface)
		if err != nil {
			return err
		}
		var serr error
		if err := c.Control(func(fd uintptr) {
			if strings.HasSuffix(network, "4") {
				x := uint32(i.Index)
				serr = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, ipUnicastIf,
					int(x>>24|x>>8&0xff00|x<<8&0xff0000|x<<24))
			} else {
				serr = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, ipv6UnicastIf, i.Index)
			}
		}); err != nil {
			return err
		}
		return serr
	}
}
