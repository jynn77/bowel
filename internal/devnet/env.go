package devnet

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Env is what this machine's networks look like now.
type Env struct {
	Tailscale Tailscale  `json:"tailscale"`
	VPNs      []Tunnel   `json:"vpns,omitempty"`
	Resolvers []Resolver `json:"resolvers,omitempty"`
	// DefaultTunnels are the tunnels that hold the default route, Tailscale
	// aside: a proxy's TUN, this one's included while it is on.
	DefaultTunnels []string `json:"defaultTunnels,omitempty"`
}

// Tailscale is its state. Iface is "" unless it is up.
type Tailscale struct {
	Installed bool           `json:"installed"`
	State     string         `json:"state,omitempty"` // Running, Stopped, NeedsLogin…
	Iface     string         `json:"iface,omitempty"`
	Tailnet   string         `json:"tailnet,omitempty"`
	Suffix    string         `json:"suffix,omitempty"` // MagicDNS's, Headscale's own domain too
	MagicDNS  bool           `json:"magicDns,omitempty"`
	Self      []netip.Addr   `json:"self,omitempty"`
	Peers     []Peer         `json:"peers,omitempty"`
	ExitNode  string         `json:"exitNode,omitempty"` // the peer traffic leaves by
	Subnets   []netip.Prefix `json:"subnets,omitempty"`  // routes of other networks through it
	// Shadowed: the system sends the tailnet's addresses somewhere else,
	// as a proxy that excludes CGNAT by the physical gateway makes it do.
	Shadowed bool `json:"shadowed,omitempty"`
}

// Peer is another machine on the tailnet.
type Peer struct {
	Name string       `json:"name"` // the short one
	FQDN string       `json:"fqdn,omitempty"`
	IPs  []netip.Addr `json:"ips"`
}

// Tunnel is a VPN's interface and the networks routed through it.
type Tunnel struct {
	Iface    string         `json:"iface"`
	Kind     string         `json:"kind"` // zerotier, wireguard, ppp, vpn
	Prefixes []netip.Prefix `json:"prefixes"`
}

// Resolver is a domain the system asks its own servers about.
type Resolver struct {
	Domain  string   `json:"domain"`
	Servers []string `json:"servers"` // host or host:port
	Iface   string   `json:"iface,omitempty"`
}

// Detector reads the machine. Tailscale's CLI starts a whole app bundle, so
// it is asked only when a tailnet address comes or goes, or a while on.
type Detector struct {
	mu     sync.Mutex
	ts     Tailscale
	tunnel string
	at     time.Time
}

// tailscaleEvery is how old what the CLI said may be: peers come and go
// without any interface changing.
const tailscaleEvery = 2 * time.Minute

// Detect reads what it can. What cannot be read is left out: a machine it
// knows nothing about gets just the catalog.
func (d *Detector) Detect(ctx context.Context) Env {
	d.mu.Lock()
	defer d.mu.Unlock()
	tunnel, _ := cgnatTunnel()
	if d.at.IsZero() || tunnel != d.tunnel || time.Since(d.at) > tailscaleEvery {
		d.ts, d.tunnel, d.at = detectTailscale(ctx), tunnel, time.Now()
	}
	routes, resolvers := osDetect(ctx)
	e := Env{Tailscale: d.ts, Resolvers: resolvers}
	if e.Tailscale.Iface == "" {
		e.Tailscale = tailscaleByInterface(e.Tailscale, tunnel, resolvers)
	}
	e.applyRoutes(routes)
	return e
}

// Detected says whether anything beyond the catalog was found to act on.
func (e Env) Detected() bool {
	return e.Tailscale.Iface != "" || len(e.VPNs) > 0 || len(e.Resolvers) > 0
}

// ---- Tailscale ----

// tailscaleBins are where its CLI is, as an app opened from Finder has
// little of the shell's PATH.
var tailscaleBins = []string{
	"tailscale", "/usr/local/bin/tailscale", "/opt/homebrew/bin/tailscale",
	"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
	`C:\Program Files\Tailscale\tailscale.exe`,
}

func detectTailscale(ctx context.Context) Tailscale {
	for _, bin := range tailscaleBins {
		path, err := exec.LookPath(bin)
		if err != nil {
			continue
		}
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		cmd := exec.CommandContext(c, path, "status", "--json")
		hideWindow(cmd)
		out, err := cmd.Output()
		cancel()
		if err != nil && len(out) == 0 {
			return Tailscale{Installed: true}
		}
		ts := parseStatus(out)
		ts.Installed = true
		if ts.State == "Running" {
			ts.Iface = ifaceOf(ts.Self)
		}
		return ts
	}
	return Tailscale{}
}

// parseStatus reads `tailscale status --json`.
func parseStatus(data []byte) Tailscale {
	type peer struct {
		DNSName      string
		HostName     string
		TailscaleIPs []netip.Addr
		ExitNode     bool
	}
	var st struct {
		BackendState   string
		TailscaleIPs   []netip.Addr
		MagicDNSSuffix string
		CurrentTailnet *struct {
			Name            string
			MagicDNSSuffix  string
			MagicDNSEnabled bool
		}
		Peer map[string]peer
	}
	var ts Tailscale
	if json.Unmarshal(data, &st) != nil {
		return ts
	}
	ts.State, ts.Self, ts.Suffix = st.BackendState, st.TailscaleIPs, st.MagicDNSSuffix
	if t := st.CurrentTailnet; t != nil {
		ts.Tailnet, ts.MagicDNS = t.Name, t.MagicDNSEnabled
		if t.MagicDNSSuffix != "" {
			ts.Suffix = t.MagicDNSSuffix
		}
	}
	ts.Suffix = strings.TrimSuffix(ts.Suffix, ".")
	if ts.State != "Running" {
		return ts // what it says of its peers is from before it stopped
	}
	for _, p := range st.Peer {
		fqdn := strings.TrimSuffix(p.DNSName, ".")
		name, _, _ := strings.Cut(fqdn, ".")
		if name == "" {
			name = strings.ToLower(p.HostName)
		}
		if p.ExitNode {
			ts.ExitNode = name
		}
		if name != "" && len(p.TailscaleIPs) > 0 {
			ts.Peers = append(ts.Peers, Peer{Name: name, FQDN: fqdn, IPs: p.TailscaleIPs})
		}
	}
	slices.SortFunc(ts.Peers, func(a, b Peer) int { return strings.Compare(a.FQDN+a.Name, b.FQDN+b.Name) })
	return ts
}

// ifaceOf is the interface that holds one of addrs.
func ifaceOf(addrs []netip.Addr) string {
	ifs, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifs {
		as, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range as {
			p, err := netip.ParsePrefix(a.String())
			if err == nil && slices.Contains(addrs, p.Addr().Unmap()) {
				return ifc.Name
			}
		}
	}
	return ""
}

// magicResolver is the address Tailscale answers DNS on.
const magicResolver = "100.100.100.100"

// tailscaleByInterface recognises Tailscale without its CLI: a tunnel with
// a CGNAT address, and the system asking its magic address.
func tailscaleByInterface(ts Tailscale, tunnel string, rs []Resolver) Tailscale {
	if tunnel == "" || (ts.State != "" && ts.State != "Running") {
		return ts // there is none, or it says it's off
	}
	if slices.ContainsFunc(rs, func(r Resolver) bool { return slices.Contains(r.Servers, magicResolver) }) {
		ts.Installed, ts.State, ts.Iface, ts.MagicDNS = true, "Running", tunnel, true
	}
	return ts
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func cgnatTunnel() (string, bool) {
	ifs, err := net.Interfaces()
	if err != nil {
		return "", false
	}
	for _, ifc := range ifs {
		if !isTunnel(ifc.Name) {
			continue
		}
		as, _ := ifc.Addrs()
		for _, a := range as {
			if p, err := netip.ParsePrefix(a.String()); err == nil && cgnat.Contains(p.Addr().Unmap()) {
				return ifc.Name, true
			}
		}
	}
	return "", false
}

// ---- Routes ----

// Route is a line of the routing table.
type Route struct {
	Prefix  netip.Prefix // zero for default
	Default bool
	Iface   string
}

// isTunnel says whether an interface's name is a VPN's. bridge and en
// interfaces are not: the core's DIRECT already reaches their networks.
func isTunnel(name string) bool {
	for _, p := range []string{"utun", "ppp", "ipsec", "tun", "tap", "wg", "zt", "feth", "gpd", "nordlynx"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func tunnelKind(name string) string {
	switch {
	case strings.HasPrefix(name, "zt"), strings.HasPrefix(name, "feth"):
		return "zerotier"
	case strings.HasPrefix(name, "wg"):
		return "wireguard"
	case strings.HasPrefix(name, "ppp"), strings.HasPrefix(name, "ipsec"):
		return "ppp"
	}
	return "vpn"
}

// parseRoutes reads `netstat -rn`.
func parseRoutes(text string) []Route {
	var out []Route
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[0] == "Destination" {
			continue
		}
		iface := f[3]
		// The I flag scopes a route to one interface: every utun has such
		// a default, and none of them takes anyone's traffic.
		if strings.Contains(f[3], "#") || !isName(iface) || strings.Contains(f[2], "I") {
			continue
		}
		if f[0] == "default" {
			out = append(out, Route{Default: true, Iface: iface})
			continue
		}
		if p, ok := parseDest(f[0]); ok {
			out = append(out, Route{Prefix: p, Iface: iface})
		}
	}
	return out
}

func isName(s string) bool {
	return s != "" && s[0] >= 'a' && s[0] <= 'z'
}

// parseDest reads a destination as netstat writes it: "10", "100.64/10",
// "192.168.50", "1.1.1.1", "fd7a::/48", "fe80::%utun0/64".
func parseDest(s string) (netip.Prefix, bool) {
	addr, bitsText, hasBits := strings.Cut(s, "/")
	if i := strings.Index(addr, "%"); i >= 0 {
		addr = addr[:i]
	}
	if strings.Contains(addr, ":") {
		ip, err := netip.ParseAddr(addr)
		if err != nil {
			return netip.Prefix{}, false
		}
		bits := 128
		if hasBits {
			if bits, err = strconv.Atoi(bitsText); err != nil {
				return netip.Prefix{}, false
			}
		}
		p, err := ip.Prefix(bits)
		return p, err == nil
	}
	parts := strings.Split(addr, ".")
	if len(parts) > 4 {
		return netip.Prefix{}, false
	}
	var b [4]byte
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return netip.Prefix{}, false
		}
		b[i] = byte(n)
	}
	bits := 8 * len(parts)
	if hasBits {
		n, err := strconv.Atoi(bitsText)
		if err != nil || n < 0 || n > 32 {
			return netip.Prefix{}, false
		}
		bits = n
	}
	p, err := netip.AddrFrom4(b).Prefix(bits)
	return p, err == nil
}

// tunnels groups routes by tunnel, leaving what is no network of a VPN's:
// the addresses of the hosts in one, link-local and multicast ones.
func tunnels(routes []Route, tailscale string) (vpn []Tunnel, subnets []netip.Prefix, holders []string) {
	byName := map[string][]netip.Prefix{}
	claimed := map[string]bool{}
	for _, r := range routes {
		if !isTunnel(r.Iface) {
			continue
		}
		if r.Default || isHalf(r.Prefix) {
			if r.Iface != tailscale {
				claimed[r.Iface] = true
			}
			continue
		}
		if !worthRouting(r.Prefix) {
			continue
		}
		byName[r.Iface] = append(byName[r.Iface], r.Prefix)
	}
	for name := range claimed {
		holders = append(holders, name)
	}
	slices.Sort(holders)
	for name, ps := range byName {
		slices.SortFunc(ps, netip.Prefix.Compare)
		ps = slices.Compact(ps)
		if name == tailscale {
			for _, p := range ps {
				if !inMesh(p) {
					subnets = append(subnets, p)
				}
			}
			continue
		}
		if claimed[name] {
			continue
		}
		vpn = append(vpn, Tunnel{Iface: name, Kind: tunnelKind(name), Prefixes: ps})
	}
	slices.SortFunc(vpn, func(a, b Tunnel) int { return strings.Compare(a.Iface, b.Iface) })
	return vpn, subnets, holders
}

// isHalf is a piece of the default route, as a proxy's TUN adds them: 128/1,
// 64/2 and so on down to 1/8. A VPN's own routes are narrower than that.
func isHalf(p netip.Prefix) bool {
	a := p.Addr()
	if a.IsMulticast() || a.IsPrivate() {
		return false
	}
	return (p.Bits() >= 1 && p.Bits() <= 7) || p == netip.PrefixFrom(netip.AddrFrom4([4]byte{1}), 8)
}

func worthRouting(p netip.Prefix) bool {
	a := p.Addr()
	if p.IsSingleIP() || a.IsLinkLocalUnicast() || a.IsMulticast() || a.IsLoopback() {
		return false
	}
	return !a.IsLinkLocalMulticast()
}

func inMesh(p netip.Prefix) bool {
	for _, c := range meshCIDRs {
		m := netip.MustParsePrefix(c)
		if m.Bits() <= p.Bits() && m.Contains(p.Addr()) {
			return true
		}
	}
	return false
}

func (e *Env) applyRoutes(routes []Route) {
	vpn, subnets, holders := tunnels(routes, e.Tailscale.Iface)
	e.VPNs, e.DefaultTunnels = vpn, holders
	e.Tailscale.Subnets = subnets
	e.Tailscale.Shadowed = shadowed(routes, e.Tailscale.Iface)
}

// shadowed says whether the route that takes the tailnet's addresses is
// another interface's. Host routes say nothing about the rest of the range.
func shadowed(routes []Route, iface string) bool {
	if iface == "" {
		return false
	}
	bits, on := -1, ""
	for _, r := range routes {
		if r.Default || r.Prefix.IsSingleIP() || !r.Prefix.Addr().Is4() || !r.Prefix.Overlaps(cgnat) {
			continue
		}
		if r.Prefix.Bits() > bits || (r.Prefix.Bits() == bits && r.Iface == iface) {
			bits, on = r.Prefix.Bits(), r.Iface
		}
	}
	return bits >= 0 && on != iface
}

// ---- Resolvers ----

var (
	scutilServer = regexp.MustCompile(`^nameserver\[\d+\]\s*:\s*(\S+)`)
	scutilIface  = regexp.MustCompile(`^if_index\s*:\s*\d+\s*\((\S+)\)`)
)

// parseResolvers reads `scutil --dns`, up to its scoped section: the
// domains the system sends somewhere of their own.
func parseResolvers(text string, own func(server string) bool) []Resolver {
	var out []Resolver
	var cur *Resolver
	port, mdns := "", false
	flush := func() {
		defer func() { cur, port, mdns = nil, "", false }()
		if cur == nil || mdns || cur.Domain == "" || len(cur.Servers) == 0 ||
			strings.HasSuffix(cur.Domain, ".arpa") || cur.Domain == "local" {
			return
		}
		var servers []string
		for _, s := range cur.Servers {
			if own != nil && own(s) {
				continue
			}
			if port != "" && port != "53" {
				s = net.JoinHostPort(s, port)
			}
			servers = append(servers, s)
		}
		if len(servers) == 0 {
			return
		}
		cur.Servers = servers
		out = append(out, *cur)
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "DNS configuration (for scoped"):
			flush()
			return dedupeResolvers(out)
		case strings.HasPrefix(line, "resolver #"):
			flush()
			cur = &Resolver{}
		case cur == nil:
		case strings.HasPrefix(line, "domain"):
			if _, v, ok := strings.Cut(line, ":"); ok {
				cur.Domain = cleanDomain(v)
			}
		case strings.HasPrefix(line, "port"):
			if _, v, ok := strings.Cut(line, ":"); ok {
				port = strings.TrimSpace(v)
			}
		case strings.HasPrefix(line, "options") && strings.Contains(line, "mdns"):
			mdns = true
		default:
			if m := scutilServer.FindStringSubmatch(line); m != nil {
				cur.Servers = append(cur.Servers, m[1])
			} else if m := scutilIface.FindStringSubmatch(line); m != nil {
				cur.Iface = m[1]
			}
		}
	}
	flush()
	return dedupeResolvers(out)
}

func dedupeResolvers(rs []Resolver) []Resolver {
	seen := map[string]bool{}
	var out []Resolver
	for _, r := range rs {
		if !seen[r.Domain] {
			seen[r.Domain] = true
			out = append(out, r)
		}
	}
	return out
}

// proxyDNS says whether server is a TUN's own fake resolver.
func proxyDNS(server string) bool {
	ip, err := netip.ParseAddr(server)
	return err == nil && netip.MustParsePrefix("198.18.0.0/15").Contains(ip)
}
