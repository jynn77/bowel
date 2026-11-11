// Package netif asks the system what its network interfaces are, where a
// name does not say: Linux and Windows let an interface be called
// anything, so whether one is a tunnel, hardware or Wi-Fi is read from
// the system. On macOS names do say (utun4, en0), nothing here knows
// more than they do, and the packages that ask keep going by the name.
package netif

import (
	"net/netip"
)

// Route is a line of the system's routing tables.
type Route struct {
	Prefix  netip.Prefix // zero for a default route
	Default bool
	Iface   string
	Gateway string // "" when the network is on the link
	Metric  int
	// Main: the route is in the table the system looks in when no rule
	// says otherwise. A proxy's TUN keeps its own in another, on Linux.
	Main bool
}

// Default is the IPv4 default route traffic takes when no tunnel has
// it: the lowest metric among those of the main table that are not a
// tunnel's. tunnel says which interfaces are.
func Default(routes []Route, tunnel func(string) bool) (iface, gateway string) {
	best := -1
	for i, r := range routes {
		if !r.Default || !r.Main || r.Iface == "" || tunnel(r.Iface) {
			continue
		}
		if r.Prefix.IsValid() && !r.Prefix.Addr().Is4() {
			continue
		}
		if best < 0 || r.Metric < routes[best].Metric {
			best = i
		}
	}
	if best < 0 {
		return "", ""
	}
	return routes[best].Iface, routes[best].Gateway
}
