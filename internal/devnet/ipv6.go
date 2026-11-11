package devnet

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"
)

// IPv6State is what automatic IPv6 found of the current network.
type IPv6State struct {
	Usable bool   `json:"usable"`
	Iface  string `json:"iface,omitempty"`  // the interface it was tried on
	Reason string `json:"reason,omitempty"` // why not, when not usable
}

// Why IPv6 is not usable.
const (
	IPv6NoAddress   = "no-address"  // the interface has no public address
	IPv6Unreachable = "unreachable" // it has one, and nothing answers
)

// Known says whether the network has been read at all.
func (s IPv6State) Known() bool { return s.Usable || s.Reason != "" }

const (
	// ipv6ProbeEvery is how often an interface whose addresses stay the
	// same is tried again.
	ipv6ProbeEvery = time.Minute
	ipv6ProbeLimit = 1500 * time.Millisecond
	// ipv6Confirm is how many tries in a row must disagree with the state
	// to change it. A change makes the core reload, and under TUN reopen the
	// TUN, so one try that lost a packet or a Wi-Fi handover must not.
	ipv6Confirm = 2
)

// ipv6Public is the range of the internet's IPv6. ULA (Tailscale's fd7a::)
// and link-local addresses say nothing about reaching it.
var ipv6Public = netip.MustParsePrefix("2000::/3")

// ipv6Targets are public resolvers, tried together so that one blocked on
// this network doesn't count against it. Any that answers will do.
var ipv6Targets = []string{"[2400:3200::1]:53", "[2606:4700:4700::1111]:53", "[2001:4860:4860::8888]:53"}

// ipv6Iface is an interface with its public IPv6 addresses.
type ipv6Iface struct {
	Name  string
	Addrs []netip.Addr
}

// IPv6Watcher decides whether the network has IPv6: a public address on the
// interface traffic leaves by, and a connection from that interface reaching
// the internet. The connection is what tells a default route from one that
// leads nowhere, and being bound to the interface keeps bowel's own TUN from
// answering for it.
type IPv6Watcher struct {
	mu      sync.Mutex
	state   IPv6State
	read    bool
	key     string // the addresses last tried
	probed  time.Time
	against int // tries in a row that disagreed with state

	// Tests replace these.
	list func() []ipv6Iface
	dial func(ctx context.Context, iface, target string) bool
	now  func() time.Time
}

// Check reads the network. forced is the interface the user set for
// outbound traffic and primary the one holding the default route; either
// narrows it down to that one. Losing the address counts at once, as nothing
// is left to try; what a connection says has to be confirmed.
func (w *IPv6Watcher) Check(primary, forced string) IPv6State {
	w.mu.Lock()
	defer w.mu.Unlock()
	list, now := w.list, w.now
	if list == nil {
		list = publicIPv6Ifaces
	}
	if now == nil {
		now = time.Now
	}
	want := forced
	if want == "" {
		want = primary
	}
	ifaces := candidates(list(), want)
	if len(ifaces) == 0 {
		w.read, w.key, w.against = true, "", 0
		w.state = IPv6State{Reason: IPv6NoAddress}
		return w.state
	}
	key := ipv6Key(ifaces)
	t := now()
	if w.read && key == w.key && w.against == 0 && t.Sub(w.probed) < ipv6ProbeEvery {
		return w.state
	}
	w.key, w.probed = key, t
	found := w.probe(ifaces)
	if w.read && found.Usable != w.state.Usable {
		if w.against++; w.against < ipv6Confirm {
			return w.state
		}
	}
	w.read, w.against, w.state = true, 0, found
	return w.state
}

// Reset forgets the network, for when nothing asks about it for a while.
func (w *IPv6Watcher) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.read, w.key, w.against, w.state = false, "", 0, IPv6State{}
}

func candidates(all []ipv6Iface, want string) []ipv6Iface {
	return slices.DeleteFunc(slices.Clone(all), func(i ipv6Iface) bool {
		if want != "" {
			return i.Name != want
		}
		return isTunnel(i.Name)
	})
}

func ipv6Key(ifaces []ipv6Iface) string {
	var b strings.Builder
	for _, i := range ifaces {
		b.WriteString(i.Name)
		for _, a := range i.Addrs {
			b.WriteString(" " + a.String())
		}
		b.WriteString(";")
	}
	return b.String()
}

// probe tries every target from every interface at once and takes the
// first that connects, within ipv6ProbeLimit.
func (w *IPv6Watcher) probe(ifaces []ipv6Iface) IPv6State {
	dial := w.dial
	if dial == nil {
		dial = dialIPv6
	}
	ctx, cancel := context.WithTimeout(context.Background(), ipv6ProbeLimit)
	defer cancel()
	type result struct {
		iface string
		ok    bool
	}
	n := len(ifaces) * len(ipv6Targets)
	done := make(chan result, n)
	for _, i := range ifaces {
		for _, t := range ipv6Targets {
			go func() { done <- result{i.Name, dial(ctx, i.Name, t)} }()
		}
	}
	for range n {
		if r := <-done; r.ok {
			return IPv6State{Usable: true, Iface: r.iface}
		}
	}
	return IPv6State{Iface: ifaces[0].Name, Reason: IPv6Unreachable}
}

func dialIPv6(ctx context.Context, iface, target string) bool {
	d := net.Dialer{Control: bindToIface(iface)}
	c, err := d.DialContext(ctx, "tcp6", target)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// publicIPv6Ifaces are the interfaces that are up and have a public IPv6
// address. If they can't be read there is nothing to try: IPv4 is what
// works everywhere, so that counts as no IPv6.
func publicIPv6Ifaces() []ipv6Iface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []ipv6Iface
	for _, i := range all {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		if public := publicIPv6(addrs); len(public) > 0 {
			out = append(out, ipv6Iface{Name: i.Name, Addrs: public})
		}
	}
	return out
}

func publicIPv6(addrs []net.Addr) []netip.Addr {
	var out []netip.Addr
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ip, ok := netip.AddrFromSlice(n.IP); ok && ipv6Public.Contains(ip) {
			out = append(out, ip)
		}
	}
	slices.SortFunc(out, netip.Addr.Compare)
	return out
}
