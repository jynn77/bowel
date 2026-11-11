package netif

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

const routesAll = `default via 192.168.1.1 dev wlan0 proto dhcp src 192.168.1.23 metric 600
default via 10.0.0.1 dev enp3s0 proto dhcp metric 100
10.8.0.0/24 dev wg0 proto kernel scope link src 10.8.0.2
172.17.0.0/16 dev docker0 proto kernel scope link src 172.17.0.1 linkdown
192.168.1.0/24 dev wlan0 proto kernel scope link src 192.168.1.23 metric 600
default dev Meta table 2022
100.100.100.100 dev tailscale0 table 52
100.101.102.103 dev tailscale0 table 52
10.20.0.0/16 dev tailscale0 table 52
local 127.0.0.0/8 dev lo table local proto kernel scope host src 127.0.0.1
broadcast 192.168.1.255 dev wlan0 table local proto kernel scope link src 192.168.1.23
unreachable 10.99.0.0/16 dev lo metric 1024
192.168.9.0/24 via 192.168.1.9 dev wlan0 onlink metric 5
fe80::/64 dev wlan0 proto kernel metric 1024 pref medium
default via fe80::1 dev wlan0 proto ra metric 600 pref medium
fd7a:115c:a1e0::/48 dev tailscale0 table 52 metric 1024 pref medium
`

func TestParseRoutes(t *testing.T) {
	rs := ParseRoutes(routesAll)
	find := func(iface, prefix string) *Route {
		for i, r := range rs {
			if r.Iface == iface && ((prefix == "default" && r.Default) || (!r.Default && r.Prefix.String() == prefix)) {
				return &rs[i]
			}
		}
		return nil
	}
	if len(rs) != 13 {
		t.Errorf("%d routes: %+v", len(rs), rs)
	}
	if r := find("wlan0", "default"); r == nil || r.Gateway != "192.168.1.1" || r.Metric != 600 || !r.Main {
		t.Errorf("wlan0's default: %+v", r)
	}
	if r := find("Meta", "default"); r == nil || r.Main {
		t.Errorf("the TUN's default, of its own table: %+v", r)
	}
	if r := find("tailscale0", "100.101.102.103/32"); r == nil || r.Main || !r.Prefix.IsSingleIP() {
		t.Errorf("a peer's route: %+v", r)
	}
	if find("tailscale0", "10.20.0.0/16") == nil || find("tailscale0", "fd7a:115c:a1e0::/48") == nil || find("wg0", "10.8.0.0/24") == nil {
		t.Errorf("networks of the tunnels: %+v", rs)
	}
	if r := find("wlan0", "192.168.9.0/24"); r == nil || r.Metric != 5 || r.Gateway != "192.168.1.9" {
		t.Errorf("a route with a flag before its metric: %+v", r)
	}
	if find("lo", "127.0.0.0/8") != nil || find("lo", "10.99.0.0/16") != nil || find("wlan0", "192.168.1.255/32") != nil {
		t.Errorf("routes to this machine or to no one are in: %+v", rs)
	}
	// The cable wins on its metric; with it a tunnel, Wi-Fi does.
	tun := func(n string) bool { return n == "Meta" || n == "tailscale0" || n == "wg0" }
	if iface, gw := Default(rs, tun); iface != "enp3s0" || gw != "10.0.0.1" {
		t.Errorf("Default = %s, %s", iface, gw)
	}
	if iface, _ := Default(rs, func(n string) bool { return n != "wlan0" }); iface != "wlan0" {
		t.Errorf("Default off the cable = %s", iface)
	}
	if iface, _ := Default(rs, func(string) bool { return true }); iface != "" {
		t.Errorf("Default with only tunnels = %s", iface)
	}
}

func TestParseProcRoute(t *testing.T) {
	if bigEndian {
		t.Skip("the sample is a little-endian machine's")
	}
	rs := parseProcRoute(`Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
eth0	00000000	0205A8C0	0003	0	0	200	00000000	0	0	0
wlan0	00000000	0101A8C0	0003	0	0	600	00000000	0	0	0
eth0	0005A8C0	00000000	0001	0	0	200	00FFFFFF	0	0	0
down0	00000000	0909A8C0	0002	0	0	1	00000000	0	0	0
`)
	if len(rs) != 3 {
		t.Fatalf("%d routes: %+v", len(rs), rs)
	}
	if !rs[0].Default || rs[0].Gateway != "192.168.5.2" || rs[0].Metric != 200 {
		t.Errorf("default: %+v", rs[0])
	}
	if rs[2].Prefix != netip.MustParsePrefix("192.168.5.0/24") || rs[2].Gateway != "" {
		t.Errorf("the link's network: %+v", rs[2])
	}
	if iface, gw := Default(rs, func(string) bool { return false }); iface != "eth0" || gw != "192.168.5.2" {
		t.Errorf("Default = %s, %s", iface, gw)
	}
}

// The kernel's own account of interfaces: by type and by what made them.
func TestKinds(t *testing.T) {
	root := t.TempDir()
	mk := func(name, typ string, files ...string) {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(dir, "type"), []byte(typ+"\n"), 0o644)
		for _, f := range files {
			_ = os.WriteFile(filepath.Join(dir, f), nil, 0o644)
		}
	}
	mk("eth0", "1", "device")
	mk("docker0", "1")
	mk("Meta", "65534", "tun_flags")
	mk("wg0", "65534")
	mk("ppp0", "512")
	mk("ztabc", "1", "tun_flags") // a TAP
	mk("lo", "772")
	for name, want := range map[string]bool{"eth0": false, "docker0": false, "Meta": true, "wg0": true, "ppp0": true, "ztabc": true, "lo": false, "none": false, "../eth0": false, "": false} {
		if got := tunnelIn(root, name); got != want {
			t.Errorf("tunnel %q = %v", name, got)
		}
	}
	for name, want := range map[string]bool{"eth0": true, "docker0": false, "Meta": false, "lo": false, "none": false} {
		if got := physicalIn(root, name); got != want {
			t.Errorf("physical %q = %v", name, got)
		}
	}
}

// On this machine: the loopback is no tunnel, and whatever holds the
// default route is an interface that exists.
func TestLive(t *testing.T) {
	if Tunnel("lo") || Physical("lo") || WiFi("lo") {
		t.Error("the loopback is taken for something else")
	}
	iface, gw := DefaultRoute()
	t.Logf("default route: %q via %q", iface, gw)
	if iface != "" {
		if _, err := os.Stat(filepath.Join(sysNet, iface)); err != nil {
			t.Errorf("the default route's interface: %v", err)
		}
	}
	rs := Routes(t.Context())
	t.Logf("%d routes", len(rs))
	if iface != "" && len(rs) > 0 {
		if got, _ := Default(rs, Tunnel); got != iface {
			t.Errorf("ip says %q holds the default route, /proc says %q", got, iface)
		}
	}
}
