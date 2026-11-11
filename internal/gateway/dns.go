package gateway

import (
	"encoding/binary"
	"net"
	"net/netip"
	"strings"
	"time"
)

// answersDNS sends one query for apple.com to addr over UDP and says
// whether a DNS answer to it came back within timeout.
func answersDNS(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	id := uint16(time.Now().UnixNano())
	if _, err := conn.Write(dnsQuery(id, "apple.com")); err != nil {
		return false
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	return err == nil && isAnswer(buf[:n], id)
}

// dnsQuery is a recursive query for name's A records.
func dnsQuery(id uint16, name string) []byte {
	b := make([]byte, 12, 12+len(name)+6)
	binary.BigEndian.PutUint16(b[0:], id)
	b[2] = 0x01                          // RD
	binary.BigEndian.PutUint16(b[4:], 1) // QDCOUNT
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	return append(b, 0, 0, 1, 0, 1) // root, QTYPE A, QCLASS IN
}

// isAnswer says whether b is a response to the query id.
func isAnswer(b []byte, id uint16) bool {
	return len(b) >= 12 && binary.BigEndian.Uint16(b) == id && b[2]&0x80 != 0
}

// scanLAN finds the Mac's LAN address without asking the system's
// configuration: the first private IPv4 of an interface that is up and
// broadcasts, which TUN's interfaces do not.
func scanLAN() Network {
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagBroadcast == 0 || !onLAN(ifc.Name) {
			continue
		}
		if ip := ifaceIPv4(&ifc); ip != "" {
			return Network{IP: ip, Iface: ifc.Name}
		}
	}
	return Network{}
}

// ifaceIPv4 is the interface's first private IPv4 address.
func ifaceIPv4(ifc *net.Interface) string {
	addrs, _ := ifc.Addrs()
	for _, ad := range addrs {
		n, ok := ad.(*net.IPNet)
		if !ok {
			continue
		}
		a, ok := netip.AddrFromSlice(n.IP)
		if a = a.Unmap(); ok && a.Is4() && a.IsPrivate() {
			return a.String()
		}
	}
	return ""
}

// physical says whether an interface by that name can be on the LAN:
// not loopback, a tunnel or a bridge.
func physical(name string) bool {
	if name == "" {
		return false
	}
	for _, p := range []string{"lo", "utun", "tun", "ppp", "ipsec", "bridge", "awdl", "llw", "gif", "stf", "anpi", "ap"} {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	return true
}
