//go:build windows

package sysproxy

import "testing"

// The registry holds the same setting as the LAN blob, so a write that
// WinINet refused still leaves Check reading what was asked for. This is
// what makes the switch work in an elevated process on the machines where
// the per-connection call fails.
func TestRegistryFallback(t *testing.T) {
	before, err := read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	t.Cleanup(func() { _ = apply(before) }) // whatever the machine had

	ws := winSettings{flags: winDirect | winProxy, server: "127.0.0.1:7899", override: "<local>"}
	if err := writeRegistry(ws); err != nil {
		t.Fatalf("writeRegistry: %v", err)
	}
	st, err := Check(7899)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !st.Ours || !st.On {
		t.Fatalf("after writeRegistry: %+v", st)
	}

	// And turning it off the same way is seen too.
	if err := writeRegistry(winSettings{flags: winDirect, server: "127.0.0.1:7899", override: "<local>"}); err != nil {
		t.Fatalf("writeRegistry off: %v", err)
	}
	if st, err := Check(7899); err != nil || st.On || st.Ours {
		t.Fatalf("after writeRegistry off: %+v %v", st, err)
	}
}

// The two readings of the proxy agree. They can differ on the script: the
// LAN blob keeps one Windows no longer lists in the registry, which is
// exactly why Enable clears it.
func TestRegistryAgreesWithWinINet(t *testing.T) {
	ws, err := readWinINet()
	if err != nil {
		t.Skipf("no WinINet answer here: %v", err)
	}
	reg, err := readRegistry()
	if err != nil {
		t.Fatalf("readRegistry: %v", err)
	}
	if reg.server != ws.server || reg.override != ws.override {
		t.Fatalf("registry %+v, WinINet %+v", reg, ws)
	}
	if on := ws.flags&winProxy != 0; on != (reg.flags&winProxy != 0) {
		t.Fatalf("registry says proxy %v, WinINet %v", reg.flags&winProxy != 0, on)
	}
}

// Enabling leaves no automatic configuration behind: a script outranks the
// fixed proxy, and one left in the blob is what made the app call its own
// takeover "changed by another program".
func TestEnableClearsTheScript(t *testing.T) {
	before, err := read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	t.Cleanup(func() { _ = apply(before) })

	if err := Enable("127.0.0.1", 7899, []string{"localhost"}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	ws, err := readWinINet()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ws.pac != "" {
		t.Fatalf("the script is still there: %q", ws.pac)
	}
	st, err := Check(7899)
	if err != nil || !st.Ours {
		t.Fatalf("check: %+v %v", st, err)
	}
}
