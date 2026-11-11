package netif

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestTunnelType(t *testing.T) {
	for typ, want := range map[uint32]bool{
		ifTypePropVirtual: true, windows.IF_TYPE_TUNNEL: true, windows.IF_TYPE_PPP: true,
		ifTypeEthernet: false, ifTypeWiFi: false, windows.IF_TYPE_SOFTWARE_LOOPBACK: false,
	} {
		if tunnelType(typ) != want {
			t.Errorf("type %d: tunnel = %v", typ, !want)
		}
	}
}

// On this machine: adapters are listed with names, every route is on one
// of them, and whatever holds the default route is no tunnel.
func TestLive(t *testing.T) {
	as := Adapters()
	if len(as) == 0 {
		t.Fatal("no adapters")
	}
	names := map[string]bool{}
	for _, a := range as {
		if a.Name == "" {
			t.Errorf("an adapter without a name: %+v", a)
		}
		names[a.Name] = true
	}
	rs := Routes(t.Context())
	for _, r := range rs {
		if !names[r.Iface] {
			t.Errorf("a route on an unknown adapter: %+v", r)
		}
	}
	iface, gw := DefaultRoute()
	t.Logf("%d adapters, %d routes, default route %q via %q", len(as), len(rs), iface, gw)
	if iface != "" && (Tunnel(iface) || !names[iface]) {
		t.Errorf("the default route's adapter %q is a tunnel or unknown", iface)
	}
}
