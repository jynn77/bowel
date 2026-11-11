package sysproxy

// What Linux desktops' proxy settings look like, kept apart from the
// commands that read and write them (sysproxy_linux.go) so that it is
// tested anywhere.
//
// Linux has no system proxy of its own: each desktop keeps one, and the
// programs made for it follow it. Two cover nearly all of them. GNOME's
// (org.gnome.system.proxy in GSettings) is also what Cinnamon, MATE,
// Budgie, Unity and Pantheon use, and what Chromium and Firefox read on
// any of those; KDE's is the "Proxy Settings" group of kioslaverc.

import (
	"net/netip"
	"strconv"
	"strings"
)

const (
	gnomeName = "gnome"
	kdeName   = "kde"

	gnomeSchema = "org.gnome.system.proxy"
	kdeFile     = "kioslaverc"
	kdeGroup    = "Proxy Settings"
)

// ---- GNOME ----

// gnomeSettings is org.gnome.system.proxy and its children.
type gnomeSettings struct {
	mode   string // none, manual, auto
	pac    string // autoconfig-url
	ignore []string
	hosts  map[string]string // by child: http, https, socks
	ports  map[string]int
}

// gnomeKinds are the schema's children, in the order of Service.endpoints.
var gnomeKinds = []string{"http", "https", "socks"}

// parseGSettings reads `gsettings list-recursively org.gnome.system.proxy`:
//
//	org.gnome.system.proxy mode 'manual'
//	org.gnome.system.proxy ignore-hosts ['localhost', '127.0.0.0/8', '::1']
//	org.gnome.system.proxy.http host '127.0.0.1'
//	org.gnome.system.proxy.http port 7890
func parseGSettings(text string) gnomeSettings {
	gs := gnomeSettings{mode: "none", hosts: map[string]string{}, ports: map[string]int{}}
	for _, line := range strings.Split(text, "\n") {
		f := strings.SplitN(strings.TrimSpace(line), " ", 3)
		if len(f) < 3 || !strings.HasPrefix(f[0], gnomeSchema) {
			continue
		}
		child, key, val := strings.TrimPrefix(strings.TrimPrefix(f[0], gnomeSchema), "."), f[1], strings.TrimSpace(f[2])
		switch {
		case child == "" && key == "mode":
			gs.mode = gvString(val)
		case child == "" && key == "autoconfig-url":
			gs.pac = gvString(val)
		case child == "" && key == "ignore-hosts":
			gs.ignore = gvStrings(val)
		case key == "host":
			gs.hosts[child] = gvString(val)
		case key == "port":
			gs.ports[child], _ = strconv.Atoi(val)
		}
	}
	return gs
}

// gvString reads a GVariant string as gsettings prints it: 'text' or
// "text", with backslash escapes.
func gvString(v string) string {
	s, _ := gvTake(strings.TrimSpace(v))
	return s
}

// gvTake reads the string v starts with, and returns what follows it.
func gvTake(v string) (s, rest string) {
	if v == "" || (v[0] != '\'' && v[0] != '"') {
		return "", ""
	}
	q := v[0]
	var b strings.Builder
	for i := 1; i < len(v); i++ {
		switch c := v[i]; {
		case c == '\\' && i+1 < len(v):
			i++
			b.WriteByte(v[i])
		case c == q:
			return b.String(), v[i+1:]
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), ""
}

// gvStrings reads a GVariant array of strings: ['a', 'b'], or @as [] when
// it is empty.
func gvStrings(v string) []string {
	i := strings.Index(v, "[")
	if i < 0 {
		return nil
	}
	var out []string
	for rest := strings.TrimLeft(v[i+1:], " ,"); rest != "" && rest[0] != ']'; rest = strings.TrimLeft(rest, " ,") {
		var s string
		if s, rest = gvTake(rest); rest == "" && s == "" {
			break
		}
		out = append(out, s)
	}
	return out
}

// gvQuote writes s as a GVariant string.
func gvQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func gvList(list []string) string {
	if len(list) == 0 {
		return "@as []"
	}
	q := make([]string, len(list))
	for i, s := range list {
		q[i] = gvQuote(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// service reads gs as a Service. GNOME has one switch for its proxies
// (mode), and keeps their addresses while it is off.
func (gs gnomeSettings) service() Service {
	sv := Service{Name: gnomeName, PAC: gs.pac, PACOn: gs.mode == "auto", Bypass: gs.ignore}
	for i, e := range sv.endpoints() {
		k := gnomeKinds[i]
		*e = Endpoint{Host: gs.hosts[k], Port: gs.ports[k]}
		e.On = gs.mode == "manual" && e.Host != "" && e.Port != 0
		if e.Host == "" {
			e.Port = 0 // the schema's default ports mean nothing with no host
		}
	}
	return sv
}

// gnomeSets are the `gsettings set` arguments that make GNOME's proxy
// what sv says, the switch last: programs watching it then read addresses
// that are already there.
func gnomeSets(sv Service) [][]string {
	var out [][]string
	mode := "none"
	for i, e := range sv.endpoints() {
		schema := gnomeSchema + "." + gnomeKinds[i]
		out = append(out, []string{schema, "host", gvQuote(e.Host)}, []string{schema, "port", strconv.Itoa(e.Port)})
		if e.On && e.Host != "" {
			mode = "manual"
		}
	}
	if mode == "none" && sv.PACOn && sv.PAC != "" {
		mode = "auto"
	}
	return append(out,
		[]string{gnomeSchema, "ignore-hosts", gvList(sv.Bypass)},
		[]string{gnomeSchema, "autoconfig-url", gvQuote(sv.PAC)},
		[]string{gnomeSchema, "mode", gvQuote(mode)})
}

// ---- KDE ----

// kdeSettings is kioslaverc's "Proxy Settings" group.
type kdeSettings struct {
	kind    int // ProxyType: 0 none, 1 manual, 2 a script, 3 WPAD, 4 the environment
	proxies [3]string
	noProxy string
	pac     string // Proxy Config Script
}

// kdeKeys are the group's proxies, in the order of Service.endpoints,
// and the scheme each address is written with.
var (
	kdeKeys    = [3]string{"httpProxy", "httpsProxy", "socksProxy"}
	kdeSchemes = [3]string{"http://", "http://", "socks://"}
)

// parseKioslaverc reads the file: an INI, whose group this is.
func parseKioslaverc(text string) kdeSettings {
	var ks kdeSettings
	in := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			in = line == "["+kdeGroup+"]"
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !in || !ok {
			continue
		}
		// A key may carry flags: "httpProxy[$e]".
		if i := strings.Index(k, "["); i >= 0 {
			k = k[:i]
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "ProxyType":
			ks.kind, _ = strconv.Atoi(v)
		case "NoProxyFor":
			ks.noProxy = v
		case "Proxy Config Script":
			ks.pac = v
		default:
			for i, key := range kdeKeys {
				if k == key {
					ks.proxies[i] = v
				}
			}
		}
	}
	return ks
}

// kdeEndpoint reads a proxy as KDE writes it, "http://host port", or as
// others do, "http://host:port".
func kdeEndpoint(v string) Endpoint {
	if i := strings.Index(v, "://"); i >= 0 {
		v = v[i+3:]
	}
	v = strings.TrimSuffix(strings.TrimSpace(v), "/")
	host, port, ok := strings.Cut(v, " ")
	if !ok {
		e := hostPort(v, 0)
		return Endpoint{Host: e.Host, Port: e.Port}
	}
	p, _ := strconv.Atoi(strings.TrimSpace(port))
	return Endpoint{Host: strings.Trim(host, "[]"), Port: p}
}

func (ks kdeSettings) service() Service {
	sv := Service{Name: kdeName, PAC: ks.pac, PACOn: ks.kind == 2, AutoDetect: ks.kind == 3}
	for i, e := range sv.endpoints() {
		*e = kdeEndpoint(ks.proxies[i])
		e.On = ks.kind == 1 && e.Host != "" && e.Port != 0
	}
	for _, b := range strings.Split(ks.noProxy, ",") {
		if b = strings.TrimSpace(b); b != "" {
			sv.Bypass = append(sv.Bypass, b)
		}
	}
	return sv
}

// kdeSets are the key and value pairs that make KDE's proxy what sv
// says, the switch last.
func kdeSets(sv Service) [][2]string {
	var out [][2]string
	kind := 0
	for i, e := range sv.endpoints() {
		v := ""
		if e.Host != "" {
			host := e.Host
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			v = kdeSchemes[i] + host + " " + strconv.Itoa(e.Port)
			if e.On {
				kind = 1
			}
		}
		out = append(out, [2]string{kdeKeys[i], v})
	}
	switch {
	case kind == 1:
	case sv.PACOn && sv.PAC != "":
		kind = 2
	case sv.AutoDetect:
		kind = 3
	}
	return append(out,
		[2]string{"NoProxyFor", strings.Join(sv.Bypass, ",")},
		[2]string{"Proxy Config Script", sv.PAC},
		[2]string{"ProxyType", strconv.Itoa(kind)})
}

// ---- Both ----

// linuxBypass turns bowel's bypass list into what both desktops take:
// names, "*.suffix", addresses and networks. macOS's short form of a
// network (169.254/16) is written out.
func linuxBypass(list []string) []string {
	var out []string
	for _, b := range list {
		if b = strings.TrimSpace(b); b == "" {
			continue
		}
		if addr, bits, ok := strings.Cut(b, "/"); ok && !strings.Contains(addr, ":") {
			for strings.Count(addr, ".") < 3 {
				addr += ".0"
			}
			if p, err := netip.ParsePrefix(addr + "/" + bits); err == nil {
				b = p.Masked().String()
			}
		}
		out = append(out, b)
	}
	return out
}

// pointed is sv with its three proxies on at host:port and bypass as its
// list; any script is kept, and off while they are on.
func pointed(sv Service, host string, port int, bypass []string) Service {
	for _, e := range sv.endpoints() {
		*e = Endpoint{On: true, Host: host, Port: port}
	}
	sv.Bypass, sv.PACOn, sv.AutoDetect = linuxBypass(bypass), false, false
	return sv
}

// off is sv with its proxies off, their addresses kept.
func off(sv Service) Service {
	for _, e := range sv.endpoints() {
		e.On = false
	}
	return sv
}

// serviceStatus is Check's answer for one desktop's settings. A script
// comes before the proxies, so while one is on they are not bowel's.
func serviceStatus(sv Service, port int) Status {
	ep := func(e Endpoint) endpoint { return endpoint{on: e.On, host: e.Host, port: e.Port} }
	s := status(ep(sv.Web), ep(sv.Secure), ep(sv.Socks), port)
	if sv.PACOn || sv.AutoDetect {
		s.Ours = false
	}
	return s
}
