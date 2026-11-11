//go:build linux || windows

package devnet

import (
	"context"
	"testing"
	"time"
)

// On this machine: the system's routes are read, and a machine with a
// default route has a primary interface that is no tunnel.
func TestDetectLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	routes, resolvers := osDetect(ctx)
	if len(routes) == 0 {
		t.Skip("no routes could be read here")
	}
	var e Env
	e.applyRoutes(routes)
	t.Logf("%d routes, %d resolvers, primary %q, VPNs %+v, default tunnels %v", len(routes), len(resolvers), e.PrimaryIface, e.VPNs, e.DefaultTunnels)
	hasDefault := false
	for _, r := range routes {
		if r.Iface == "" {
			t.Errorf("a route with no interface: %+v", r)
		}
		hasDefault = hasDefault || (r.Default && !isTunnel(r.Iface))
	}
	if hasDefault && (e.PrimaryIface == "" || isTunnel(e.PrimaryIface)) {
		t.Errorf("primary interface %q", e.PrimaryIface)
	}
	var d Detector
	env := d.Detect(ctx)
	if env.PrimaryIface != e.PrimaryIface {
		t.Errorf("Detect's primary interface %q", env.PrimaryIface)
	}
}
