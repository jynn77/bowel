package devnet

import (
	"context"
	"net"
	"net/netip"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// fakeIPv6 is a network whose interfaces and upstream a test sets.
type fakeIPv6 struct {
	w      *IPv6Watcher
	ifaces []ipv6Iface
	up     atomic.Bool
	dials  atomic.Int32
	dialed atomic.Value
	at     time.Time
}

func newFakeIPv6(names ...string) *fakeIPv6 {
	f := &fakeIPv6{at: time.Unix(1000, 0)}
	for i, n := range names {
		f.ifaces = append(f.ifaces, ipv6Iface{n, []netip.Addr{netip.AddrFrom16([16]byte{0x24, 15: byte(i + 1)})}})
	}
	f.w = &IPv6Watcher{
		list: func() []ipv6Iface { return f.ifaces },
		dial: func(_ context.Context, iface, _ string) bool {
			f.dials.Add(1)
			f.dialed.Store(iface)
			return f.up.Load()
		},
		now: func() time.Time { return f.at },
	}
	return f
}

func (f *fakeIPv6) tick() IPv6State {
	f.at = f.at.Add(20 * time.Second)
	return f.w.Check("", "")
}

func TestIPv6FirstReadCountsAtOnce(t *testing.T) {
	f := newFakeIPv6("en0")
	f.up.Store(true)
	if s := f.tick(); !s.Usable || s.Iface != "en0" {
		t.Errorf("%+v", s)
	}
	g := newFakeIPv6("en0")
	if s := g.tick(); s.Usable || s.Reason != IPv6Unreachable {
		t.Errorf("%+v", s)
	}
}

func TestIPv6NoAddressMakesNoConnection(t *testing.T) {
	f := newFakeIPv6()
	if s := f.tick(); s.Usable || s.Reason != IPv6NoAddress || !s.Known() {
		t.Errorf("%+v", s)
	}
	if f.dials.Load() != 0 {
		t.Errorf("%d dials on a network with nothing to try", f.dials.Load())
	}
}

// Losing the address needs no confirming; gaining it, and what a connection
// says, do.
func TestIPv6ChangesAreConfirmed(t *testing.T) {
	f := newFakeIPv6("en0")
	f.up.Store(true)
	f.tick()

	f.up.Store(false)
	f.at = f.at.Add(ipv6ProbeEvery)
	if s := f.tick(); !s.Usable {
		t.Fatalf("one failure was enough: %+v", s)
	}
	if s := f.tick(); s.Usable || s.Reason != IPv6Unreachable {
		t.Fatalf("two failures weren't: %+v", s)
	}

	f.up.Store(true)
	f.at = f.at.Add(ipv6ProbeEvery)
	if s := f.tick(); s.Usable {
		t.Fatalf("one success was enough: %+v", s)
	}
	if s := f.tick(); !s.Usable {
		t.Fatalf("two successes weren't: %+v", s)
	}

	addrs := f.ifaces
	f.ifaces = nil
	if s := f.tick(); s.Usable || s.Reason != IPv6NoAddress {
		t.Fatalf("the address went: %+v", s)
	}

	// Back on a new network: confirmed again, tried at once and not after a minute.
	f.ifaces = addrs
	if s := f.tick(); s.Usable {
		t.Fatalf("one success was enough: %+v", s)
	}
	if s := f.tick(); !s.Usable {
		t.Fatalf("two successes weren't: %+v", s)
	}
}

func TestIPv6OneBadTryIsForgiven(t *testing.T) {
	f := newFakeIPv6("en0")
	f.up.Store(true)
	f.tick()
	f.up.Store(false)
	f.at = f.at.Add(ipv6ProbeEvery)
	f.tick()
	f.up.Store(true)
	f.tick()
	f.up.Store(false)
	f.at = f.at.Add(ipv6ProbeEvery)
	if s := f.tick(); !s.Usable {
		t.Errorf("two failures apart counted as two in a row: %+v", s)
	}
}

func TestIPv6ProbesSparingly(t *testing.T) {
	f := newFakeIPv6("en0")
	f.up.Store(true)
	f.tick()
	first := f.dials.Load()
	f.tick()
	f.tick()
	if got := f.dials.Load(); got != first {
		t.Errorf("tried again within the minute: %d dials, %d before", got, first)
	}
	f.tick()
	if got := f.dials.Load(); got == first {
		t.Error("not tried again after a minute")
	}
	// New addresses are a new network, to try at once.
	before := f.dials.Load()
	f.ifaces = []ipv6Iface{{"en0", []netip.Addr{netip.MustParseAddr("2400::9")}}}
	f.w.Check("", "")
	if f.dials.Load() == before {
		t.Error("new addresses weren't tried")
	}
}

func TestIPv6PicksTheInterface(t *testing.T) {
	f := newFakeIPv6("en0", "en7", "utun3")
	f.up.Store(true)

	if s := f.w.Check("en7", ""); s.Iface != "en7" || !s.Usable {
		t.Errorf("primary: %+v", s)
	}
	f.w.Reset()
	if s := f.w.Check("en7", "en0"); s.Iface != "en0" {
		t.Errorf("the user's interface comes first: %+v", s)
	}
	f.w.Reset()
	if s := f.w.Check("en9", ""); s.Usable || s.Reason != IPv6NoAddress {
		t.Errorf("the interface in use has no address, whatever others have: %+v", s)
	}
	f.w.Reset()
	if got := candidates(f.ifaces, ""); len(got) != 2 || got[0].Name != "en0" || got[1].Name != "en7" {
		t.Errorf("a tunnel counted: %+v", got)
	}
}

func TestIPv6ResetForgets(t *testing.T) {
	f := newFakeIPv6("en0")
	f.up.Store(true)
	f.tick()
	f.w.Reset()
	f.up.Store(false)
	if s := f.tick(); s.Usable || s.Reason != IPv6Unreachable {
		t.Errorf("not a first read after Reset: %+v", s)
	}
}

func TestPublicIPv6(t *testing.T) {
	var addrs []net.Addr
	for _, s := range []string{"192.168.1.2/24", "fe80::1/64", "fd7a:115c:a1e0::1/48", "2400:db8::5/64", "2001:db8::9/64"} {
		ip, n, err := net.ParseCIDR(s)
		if err != nil {
			t.Fatal(err)
		}
		addrs = append(addrs, &net.IPNet{IP: ip, Mask: n.Mask})
	}
	want := []netip.Addr{netip.MustParseAddr("2001:db8::9"), netip.MustParseAddr("2400:db8::5")}
	if got := publicIPv6(addrs); !reflect.DeepEqual(got, want) {
		t.Errorf("%v", got)
	}
}

func TestPrimaryIface(t *testing.T) {
	var e Env
	e.applyRoutes([]Route{{Default: true, Iface: "utun4"}, {Default: true, Iface: "en1"}, {Default: true, Iface: "en0"}})
	if e.PrimaryIface != "en1" {
		t.Errorf("%q", e.PrimaryIface)
	}
}

func TestProbeTakesFirstSuccess(t *testing.T) {
	var calls atomic.Int32
	w := &IPv6Watcher{dial: func(_ context.Context, iface, target string) bool {
		calls.Add(1)
		return target == ipv6Targets[1]
	}}
	if s := w.probe([]ipv6Iface{{Name: "en0"}}); !s.Usable {
		t.Errorf("%+v", s)
	}
}
