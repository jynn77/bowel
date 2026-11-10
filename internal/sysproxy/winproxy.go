package sysproxy

// What Windows' proxy settings look like, kept apart from the calls that
// read and write them (sysproxy_windows.go) so that it is tested anywhere.

import (
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

// WinINet's INTERNET_PER_CONN_FLAGS bits.
const (
	winDirect     = 0x1 // PROXY_TYPE_DIRECT
	winProxy      = 0x2 // PROXY_TYPE_PROXY: the proxies in ProxyServer
	winPAC        = 0x4 // PROXY_TYPE_AUTO_PROXY_URL
	winAutoDetect = 0x8 // PROXY_TYPE_AUTO_DETECT (WPAD)
)

// winSettings are the LAN connection's proxy settings, as WinINet has them.
type winSettings struct {
	flags    uint32
	server   string // ProxyServer: "host:port" or "http=…;https=…;socks=…"
	override string // ProxyOverride: entries separated by ';'
	pac      string // AutoConfigURL
}

// winService reads ws as a Service.
func winService(ws winSettings) Service {
	sv := Service{Name: "LAN", PAC: ws.pac, PACOn: ws.flags&winPAC != 0, AutoDetect: ws.flags&winAutoDetect != 0}
	sv.Web, sv.Secure, sv.Socks = parseWinServer(ws.server)
	on := ws.flags&winProxy != 0
	for _, e := range sv.endpoints() {
		e.On = on && e.Host != ""
	}
	for _, b := range strings.Split(ws.override, ";") {
		if b = strings.TrimSpace(b); b != "" {
			sv.Bypass = append(sv.Bypass, b)
		}
	}
	return sv
}

// winSettings is sv as WinINet has it. The proxies are on if any of them
// is: WinINet has one switch for them all.
func (sv Service) winSettings() winSettings {
	ws := winSettings{flags: winDirect, server: winServer(sv.Web, sv.Secure, sv.Socks), override: strings.Join(sv.Bypass, ";"), pac: sv.PAC}
	for _, e := range sv.endpoints() {
		if e.On && e.Host != "" {
			ws.flags |= winProxy
		}
	}
	if sv.PACOn && sv.PAC != "" {
		ws.flags |= winPAC
	}
	if sv.AutoDetect {
		ws.flags |= winAutoDetect
	}
	return ws
}

// winStatus is Check's answer for ws. A PAC script comes before the
// proxies, so while one is on they are not Moho's.
func winStatus(ws winSettings, port int) Status {
	sv := winService(ws)
	ep := func(e Endpoint) endpoint { return endpoint{on: e.On, host: e.Host, port: e.Port} }
	s := status(ep(sv.Web), ep(sv.Secure), ep(sv.Socks), port)
	if sv.PACOn {
		s.Ours = false
	}
	return s
}

// parseWinServer reads ProxyServer. A bare "host:port" serves every
// protocol but SOCKS; "proto=host:port" entries name theirs. Entries may
// carry a scheme ("http://host:port").
func parseWinServer(s string) (web, secure, socks Endpoint) {
	var bare *Endpoint
	for _, entry := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ' ' || r == '\t' }) {
		proto, addr, named := strings.Cut(entry, "=")
		if !named {
			addr, proto = entry, ""
		}
		if _, rest, ok := strings.Cut(addr, "://"); ok {
			addr = rest
		}
		defPort := 80
		if proto = strings.ToLower(proto); proto == "socks" {
			defPort = 1080
		}
		e := hostPort(addr, defPort)
		switch proto {
		case "":
			bare = &e
		case "http":
			web = e
		case "https":
			secure = e
		case "socks":
			socks = e
		}
	}
	if bare != nil {
		if web.Host == "" {
			web = *bare
		}
		if secure.Host == "" {
			secure = *bare
		}
	}
	return web, secure, socks
}

func hostPort(addr string, defPort int) Endpoint {
	addr = strings.TrimSuffix(addr, "/")
	if h, p, err := net.SplitHostPort(addr); err == nil {
		n, _ := strconv.Atoi(p)
		return Endpoint{Host: h, Port: n}
	}
	return Endpoint{Host: strings.Trim(addr, "[]"), Port: defPort}
}

// winServer writes ProxyServer: the bare form when HTTP and HTTPS share a
// proxy and there is no SOCKS one, as Windows' own settings write it.
func winServer(web, secure, socks Endpoint) string {
	addr := func(e Endpoint) string { return net.JoinHostPort(e.Host, strconv.Itoa(e.Port)) }
	if web.Host != "" && web.Host == secure.Host && web.Port == secure.Port && socks.Host == "" {
		return addr(web)
	}
	var parts []string
	for _, p := range []struct {
		proto string
		e     Endpoint
	}{{"http", web}, {"https", secure}, {"socks", socks}} {
		if p.e.Host != "" {
			parts = append(parts, p.proto+"="+addr(p.e))
		}
	}
	return strings.Join(parts, ";")
}

// winBypass turns Moho's bypass list into ProxyOverride entries. WinINet
// matches wildcards, not networks, so an IPv4 network becomes the
// wildcards that cover it (10.0.0.0/8 → 10.*). IPv6 networks, which it
// cannot match, are left out, and so are IPv6 addresses: one of those in
// ProxyOverride makes InternetSetOption refuse the whole call with
// ERROR_INVALID_PARAMETER. <local>, the names without a dot, is added, as
// Windows' "Don't use the proxy server for local addresses" does.
func winBypass(list []string) string {
	var out []string
	add := func(s string) {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	for _, b := range list {
		if b = strings.TrimSpace(b); b == "" || isIPv6Entry(b) {
			continue
		}
		if !strings.Contains(b, "/") {
			add(b)
			continue
		}
		for _, w := range wildcards(b) {
			add(w)
		}
	}
	add("<local>")
	return strings.Join(out, ";")
}

// isIPv6Entry reports whether a bypass entry names an IPv6 address, bracketed
// or not, wildcards included (::1, [::1], fe80::*). One colon is a host and
// port (example.com:8080), which WinINet takes.
func isIPv6Entry(b string) bool {
	return strings.HasPrefix(b, "[") || strings.Count(b, ":") >= 2
}

// wildcards covers an IPv4 network with WinINet wildcards. It accepts
// macOS's short form too (169.254/16).
func wildcards(cidr string) []string {
	addr, bitsStr, _ := strings.Cut(cidr, "/")
	if !strings.Contains(addr, ":") {
		for strings.Count(addr, ".") < 3 {
			addr += ".0"
		}
	}
	p, err := netip.ParsePrefix(addr + "/" + bitsStr)
	if err != nil || !p.Addr().Is4() {
		return nil
	}
	p = p.Masked()
	ip, bits := p.Addr().As4(), p.Bits()
	if bits == 32 {
		return []string{p.Addr().String()}
	}
	whole := bits / 8 // octets fixed by the network
	prefix := ""
	for i := range whole {
		prefix += strconv.Itoa(int(ip[i])) + "."
	}
	if bits%8 == 0 {
		return []string{prefix + "*"}
	}
	n := 1 << (8 - bits%8) // values the partial octet takes
	var out []string
	for v := int(ip[whole]); v < int(ip[whole])+n; v++ {
		w := prefix + strconv.Itoa(v)
		if whole < 3 {
			w += ".*"
		}
		out = append(out, w)
	}
	return out
}
