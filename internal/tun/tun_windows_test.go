//go:build windows

package tun

import (
	"net/netip"
	"testing"
)

// Only TUN's own default route is TUN's to take back: the LAN's default
// route and every narrower route stay, whatever gateway they use.
func TestLeftover(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dst     string
		nextHop string
		want    bool
	}{
		{"TUN's default route", "0.0.0.0/0", "198.18.0.2", true},
		{"TUN's half of it", "0.0.0.0/1", "198.18.0.2", true},
		{"TUN's other half", "128.0.0.0/1", "198.18.0.2", true},
		{"the LAN's default route", "0.0.0.0/0", "192.168.10.1", false},
		{"the LAN itself", "192.168.10.0/24", "198.18.0.2", false},
		{"TUN's own subnet", "198.18.0.0/30", "0.0.0.0", false},
		{"a public route", "1.1.1.1/32", "198.18.0.2", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := forward{dst: netip.MustParsePrefix(tc.dst), nextHop: netip.MustParseAddr(tc.nextHop)}
			if got := leftover(f); got != tc.want {
				t.Fatalf("leftover(%s via %s) = %v, want %v", tc.dst, tc.nextHop, got, tc.want)
			}
		})
	}
}
