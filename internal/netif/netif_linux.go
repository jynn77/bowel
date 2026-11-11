package netif

import (
	"bufio"
	"context"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const sysNet = "/sys/class/net"

// Tunnel says whether the kernel has name as a tunnel: a device with no
// link layer (TUN, WireGuard), PPP, an IP-in-IP kind, or anything the
// tun driver made (TAP too, which ZeroTier and OpenVPN bridge with).
func Tunnel(name string) bool { return tunnelIn(sysNet, name) }

func tunnelIn(root, name string) bool {
	if name == "" || strings.ContainsAny(name, "/") {
		return false
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(filepath.Join(dir, "tun_flags")); err == nil {
		return true
	}
	b, err := os.ReadFile(filepath.Join(dir, "type"))
	if err != nil {
		return false
	}
	// ARPHRD_*: 512 PPP, 768 IPIP, 769 IP6 in IP6, 776 SIT, 778 GRE,
	// 823 GRE over IPv6, 65534 none.
	t, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return slices.Contains([]int{512, 768, 769, 776, 778, 823, 65534}, t)
}

// Physical says whether name is hardware (or a virtual machine's): the
// kernel gives those a device, and bridges, veth pairs and tunnels none.
func Physical(name string) bool { return physicalIn(sysNet, name) }

func physicalIn(root, name string) bool {
	if name == "" || strings.ContainsAny(name, "/") {
		return false
	}
	_, err := os.Stat(filepath.Join(root, name, "device"))
	return err == nil && !tunnelIn(root, name)
}

// WiFi says whether name is a wireless interface.
func WiFi(name string) bool {
	if name == "" || strings.ContainsAny(name, "/") {
		return false
	}
	for _, f := range []string{"wireless", "phy80211"} {
		if _, err := os.Stat(filepath.Join(sysNet, name, f)); err == nil {
			return true
		}
	}
	return false
}

// ipBins are where iproute2's ip is: an app started from a desktop may
// have a PATH without the sbin directories.
var ipBins = []string{"ip", "/usr/sbin/ip", "/sbin/ip", "/usr/bin/ip", "/bin/ip"}

// IP runs iproute2's ip and returns what it printed.
func IP(ctx context.Context, args ...string) (string, error) {
	var err error
	for _, bin := range ipBins {
		var path string
		if path, err = exec.LookPath(bin); err != nil {
			continue
		}
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		out, rerr := exec.CommandContext(c, path, args...).Output()
		cancel()
		return string(out), rerr
	}
	return "", err
}

// Routes are the routes of every table, IPv4 then IPv6. Policy routing
// (which table a packet is looked up in) is not read: what the tables
// hold is enough to tell whose networks are where.
func Routes(ctx context.Context) []Route {
	var out []Route
	for _, fam := range []string{"-4", "-6"} {
		if text, err := IP(ctx, fam, "route", "show", "table", "all"); err == nil {
			out = append(out, ParseRoutes(text)...)
		}
	}
	return out
}

// ParseRoutes reads `ip route show table all`:
//
//	default via 192.168.1.1 dev wlan0 proto dhcp metric 600
//	10.8.0.0/24 dev wg0 proto kernel scope link src 10.8.0.2
//	100.101.102.103 dev tailscale0 table 52
//	default dev Meta table 2022
//	local 127.0.0.0/8 dev lo table local proto kernel scope host
//
// Routes that deliver to this machine or to no one (local, broadcast,
// unreachable and the like) are left out.
func ParseRoutes(text string) []Route {
	var out []Route
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		switch f[0] {
		case "local", "broadcast", "anycast", "multicast", "unreachable", "blackhole", "prohibit", "throw", "nat":
			continue
		case "unicast":
			f = f[1:]
		}
		r := Route{Main: true}
		if f[0] == "default" {
			r.Default = true
		} else if p, err := netip.ParsePrefix(f[0]); err == nil {
			r.Prefix = p.Masked()
		} else if a, err := netip.ParseAddr(f[0]); err == nil {
			r.Prefix = netip.PrefixFrom(a, a.BitLen())
		} else {
			continue
		}
		for i := 1; i+1 < len(f); i += 2 {
			switch f[i] {
			case "dev":
				r.Iface = f[i+1]
			case "via":
				r.Gateway = f[i+1]
			case "metric":
				r.Metric, _ = strconv.Atoi(f[i+1])
			case "table":
				r.Main = f[i+1] == "main"
			case "onlink", "pervasive", "linkdown", "dead", "offload", "notify":
				i-- // a flag, not a pair
			}
		}
		if r.Iface == "" {
			continue
		}
		// ::/0 and 0.0.0.0/0 spelled out are defaults too.
		if !r.Default && r.Prefix.Bits() == 0 {
			r.Default, r.Prefix = true, netip.Prefix{}
		}
		out = append(out, r)
	}
	return out
}

// DefaultRoute is the interface and gateway of the IPv4 default route
// off any tunnel, "" when there is none.
func DefaultRoute() (iface, gateway string) {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", ""
	}
	return Default(parseProcRoute(string(b)), Tunnel)
}

// parseProcRoute reads /proc/net/route, the main IPv4 table: a line of
// headings, then per route the interface, and in hexadecimal as the
// machine stores them the destination, gateway, flags, …, metric, mask.
func parseProcRoute(text string) []Route {
	var out []Route
	for i, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 8 {
			continue
		}
		flags, err := strconv.ParseUint(f[3], 16, 32)
		if err != nil || flags&1 == 0 { // RTF_UP
			continue
		}
		dst, ok1 := hexAddr(f[1])
		mask, ok2 := hexAddr(f[7])
		gw, ok3 := hexAddr(f[2])
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		bits := 0
		for _, b := range mask.As4() {
			for ; b&0x80 != 0; b <<= 1 {
				bits++
			}
		}
		r := Route{Iface: f[0], Main: true}
		r.Metric, _ = strconv.Atoi(f[6])
		if flags&2 != 0 { // RTF_GATEWAY
			r.Gateway = gw.String()
		}
		if bits == 0 && dst.IsUnspecified() {
			r.Default = true
		} else {
			r.Prefix = netip.PrefixFrom(dst, bits)
		}
		out = append(out, r)
	}
	return out
}

// hexAddr reads an IPv4 address as /proc/net/route prints it: the
// address's four bytes read as a number of the machine's byte order.
func hexAddr(s string) (netip.Addr, bool) {
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || len(s) != 8 {
		return netip.Addr{}, false
	}
	b := [4]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
	if bigEndian {
		b = [4]byte{b[3], b[2], b[1], b[0]}
	}
	return netip.AddrFrom4(b), true
}

// Leased says whether name's IPv4 address has a lifetime, as one from
// DHCP has and one set by hand has not.
func Leased(ctx context.Context, name string) bool {
	out, err := IP(ctx, "-o", "-4", "addr", "show", "dev", name)
	return err == nil && strings.Contains(out, " dynamic ")
}
