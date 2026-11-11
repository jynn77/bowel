package gateway

import (
	"testing"

	"github.com/oboo/moho/internal/tun"
)

// Windows reads the network and changes nothing: turning the gateway on
// says it cannot, before anything is asked of the user.
func TestWindowsReadsOnly(t *testing.T) {
	sys := OS()
	if Supported() || sys.Ready() {
		t.Error("the gateway reads as supported")
	}
	if err := sys.Prepare("p", tun.Step{"echo ", "x"}); err == nil {
		t.Error("Prepare ran")
	}
	if err := sys.SetForwarding(true); err == nil {
		t.Error("SetForwarding ran")
	}
	if _, err := sys.Forwarding(); err != nil {
		t.Errorf("Forwarding: %v", err)
	}
	n := sys.Network()
	t.Logf("network %+v, primary %q", n, PrimaryInterface())
	if n.Iface != "" && n.IP == "" {
		t.Errorf("an adapter with no address: %+v", n)
	}
}
