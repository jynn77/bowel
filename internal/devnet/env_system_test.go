package devnet

import (
	"net/netip"
	"slices"
	"testing"

	"github.com/oboo/moho/internal/netif"
)

// What Linux and Windows read of the system, as every system's tests can
// read it: the parsing has nothing of theirs in it.

const resolvedDNS = `Global:
Link 2 (enp3s0): 192.168.1.1 fe80::1%2
Link 3 (tailscale0): 100.100.100.100
Link 4 (wg0): 10.8.0.1 10.8.0.2#dns.corp.example
                    10.8.0.3
Link 5 (docker0):
Link 6 (tun0): 198.18.0.2
`

const resolvedDomains = `Global:
Link 2 (enp3s0): lan ~intranet.example
Link 3 (tailscale0): tail17b34.ts.net ~0.e.1.a.c.5.1.1.a.7.d.f.ip6.arpa
                     ~100.100.in-addr.arpa
Link 4 (wg0): ~corp.example ~.
Link 5 (docker0):
Link 6 (tun0): ~proxy.example
`

func TestParseResolved(t *testing.T) {
	rs := parseResolved(resolvedDNS, resolvedDomains, proxyDNS, tunnelName)
	got := map[string]Resolver{}
	for _, r := range rs {
		got[r.Domain] = r
	}
	if len(rs) != 3 {
		t.Fatalf("resolvers: %+v", rs)
	}
	// A routing domain counts on any link; the LAN's own search domain
	// does not, its servers being the default ones.
	if r := got["intranet.example"]; r.Iface != "enp3s0" || !slices.Equal(r.Servers, []string{"192.168.1.1", "fe80::1"}) {
		t.Errorf("intranet.example: %+v", r)
	}
	if _, ok := got["lan"]; ok {
		t.Error("the LAN's search domain is split")
	}
	// A tunnel's search domain counts; reverse zones do not.
	if r := got["tail17b34.ts.net"]; r.Iface != "tailscale0" || !slices.Equal(r.Servers, []string{magicResolver}) {
		t.Errorf("the tailnet's domain: %+v", r)
	}
	// A list that goes on over the next line, a server with its TLS name,
	// and "~." (everything), which is no domain.
	if r := got["corp.example"]; r.Iface != "wg0" || !slices.Equal(r.Servers, []string{"10.8.0.1", "10.8.0.2", "10.8.0.3"}) {
		t.Errorf("corp.example: %+v", r)
	}
	// A proxy's TUN answering with its fake resolver is nobody's network.
	if _, ok := got["proxy.example"]; ok {
		t.Error("a TUN's own resolver is taken for a network's")
	}
	if rs := parseResolved("", "", nil, tunnelName); len(rs) != 0 {
		t.Errorf("nothing read: %+v", rs)
	}
	// Recognised without Tailscale's CLI, as on macOS.
	ts := tailscaleByInterface(Tailscale{}, "tailscale0", nil)
	if ts.Iface != "" {
		t.Errorf("recognised from nothing: %+v", ts)
	}
	if ts := tailscaleByInterface(Tailscale{}, "tailscale0", parseResolved(resolvedDNS, resolvedDomains, proxyDNS, tunnelName)); ts.Iface != "tailscale0" || !ts.MagicDNS {
		t.Errorf("Tailscale by its resolver: %+v", ts)
	}
}

func TestSplitResolvers(t *testing.T) {
	rs := splitResolvers([]string{".ts.net", ".corp.example", ".", "64.100.in-addr.arpa"}, "100.100.100.100; 10.0.0.53", "", proxyDNS)
	if len(rs) != 2 || rs[0].Domain != "ts.net" || rs[1].Domain != "corp.example" || !slices.Equal(rs[0].Servers, []string{"100.100.100.100", "10.0.0.53"}) {
		t.Errorf("a policy rule: %+v", rs)
	}
	if rs := splitResolvers([]string{"vpn.example"}, "198.18.0.2", "Meta", proxyDNS); rs != nil {
		t.Errorf("a TUN's own resolver: %+v", rs)
	}
	if rs := splitResolvers([]string{"vpn.example"}, "10.1.0.1", "OpenVPN", proxyDNS); len(rs) != 1 || rs[0].Iface != "OpenVPN" {
		t.Errorf("an adapter's suffix: %+v", rs)
	}
}

// The system's routes as Linux has them with Tailscale, a WireGuard VPN
// and a proxy's TUN up: the same reading comes out as on macOS.
func TestFromSystem(t *testing.T) {
	p := netip.MustParsePrefix
	sys := []netif.Route{
		{Prefix: p("10.8.0.0/24"), Iface: "wg0", Main: true},
		{Default: true, Iface: "wlan0", Gateway: "192.168.1.1", Metric: 600, Main: true},
		{Default: true, Iface: "tun0", Main: false}, // the TUN's, in its own table
		{Default: true, Iface: "enp3s0", Gateway: "10.0.0.1", Metric: 100, Main: true},
		{Default: true, Prefix: p("::/0"), Iface: "wlan0", Metric: 1, Main: true},
		{Prefix: p("192.168.1.0/24"), Iface: "wlan0", Main: true},
		{Prefix: p("100.101.102.103/32"), Iface: "tailscale0"},
		{Prefix: p("10.20.0.0/16"), Iface: "tailscale0"},
		{Prefix: p("fd7a:115c:a1e0::/48"), Iface: "tailscale0"},
	}
	rs := fromSystem(sys)
	if len(rs) != len(sys)-1 {
		t.Fatalf("%d routes: %+v", len(rs), rs)
	}
	if !rs[0].Default || rs[0].Iface != "enp3s0" || !rs[1].Default || rs[1].Iface != "wlan0" || rs[2].Iface != "tun0" {
		t.Errorf("defaults, the one traffic takes first: %+v", rs[:3])
	}
	e := Env{Tailscale: Tailscale{Installed: true, State: "Running", Iface: "tailscale0"}}
	e.applyRoutes(rs)
	if e.PrimaryIface != "enp3s0" {
		t.Errorf("primary interface %q", e.PrimaryIface)
	}
	if !slices.Equal(e.DefaultTunnels, []string{"tun0"}) {
		t.Errorf("tunnels holding the default route: %v", e.DefaultTunnels)
	}
	if len(e.VPNs) != 1 || e.VPNs[0].Iface != "wg0" || e.VPNs[0].Kind != "wireguard" || !slices.Equal(e.VPNs[0].Prefixes, []netip.Prefix{p("10.8.0.0/24")}) {
		t.Errorf("VPNs: %+v", e.VPNs)
	}
	if !slices.Equal(e.Tailscale.Subnets, []netip.Prefix{p("10.20.0.0/16")}) {
		t.Errorf("Tailscale's subnets: %v", e.Tailscale.Subnets)
	}
	if e.Tailscale.Shadowed {
		t.Error("the tailnet reads as shadowed")
	}
}
