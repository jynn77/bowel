package core

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/oboo/moho/internal/devnet"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/routes"
)

// LocalDNSPort is where the core's resolver listens on this Mac, when it
// has one and devices on the LAN don't use it. A development build moves
// it, to run beside the released app.
var LocalDNSPort = 1053

// NodeSep separates a node's own name from the provider marker Moho appends
// (mihomo would otherwise see duplicate names across providers). Display
// names cut at it.
const NodeSep = "​‹"

// ProbeGroup is the selector the location probe points at a node.
const ProbeGroup = "MOHO-PROBE"

// ProviderRef is an enabled provider and its file.
type ProviderRef struct {
	ID   string
	Path string
}

// RuleSetRef is a rule set whose file is there to be read.
type RuleSetRef struct {
	ID       string
	Path     string
	Behavior string
}

// Input is everything the runtime config is made of.
type Input struct {
	Providers []ProviderRef
	RuleSets  []RuleSetRef // a set with no entry here has no rule: the core refuses a RULE-SET it can't find
	Routes    []routes.Route
	UserRules []routes.UserRule // sorted in with the routes' own, the more specific first
	Mode      string
	MixedPort int
	AllowLan  bool
	Tun       bool
	Gateway   bool // LAN devices use this Mac as gateway and DNS (needs Tun): the resolver answers them on port 53, with fake IPs
	LogLevel  string
	TestURL   string
	MainURLs  map[string]string // route id → what its automatic group tests; TestURL where absent
	// Scopes are the nodes a route's automatic group may pick from, by
	// route id; every node where absent. An empty list is no node at all:
	// a range is never widened on its own.
	Scopes map[string][]string
	// Orders are the nodes of a route's fallback group, first preferred,
	// by route id. Routes absent have no such group.
	Orders     map[string][]string
	ProbePort  int
	Controller map[string]any

	// Dev keeps the machine's own networks off the proxy; see devnet.
	Dev devnet.Overlay

	// IPv6Usable is what Options.IPv6 Auto found of the current network.
	IPv6Usable bool

	// Imported, when set, is the routing to use instead of Routes: a
	// provider's own nodes, groups and rules. UserRules and RuleSets, which
	// name route groups it doesn't have, are left out with the rest.
	Imported *Imported

	// Options are the 设置 window's; zero values fall back to Moho's.
	Options
}

func providerName(id string) string { return "p-" + id }

// providerOf is the provider a node's full name says it came from.
func providerOf(node string) string {
	if i := strings.LastIndex(node, NodeSep); i >= 0 {
		return node[i+len(NodeSep):]
	}
	return ""
}

// exactly is a group filter that keeps just these node names. mihomo
// splits a filter at backticks, so one in a name is matched by any
// character.
func exactly(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = strings.ReplaceAll(regexp.QuoteMeta(n), "`", ".")
	}
	return "^(?:" + strings.Join(quoted, "|") + ")$"
}

// withTarget puts the target before a rule's trailing options:
// "GEOIP,CN,no-resolve" + DIRECT → "GEOIP,CN,DIRECT,no-resolve".
func withTarget(rule, target string) string {
	if r, ok := strings.CutSuffix(rule, ",no-resolve"); ok {
		return r + "," + target + ",no-resolve"
	}
	return rule + "," + target
}

// BuildConfig writes the mihomo config: one file proxy-provider per enabled
// provider, a selector per route with the groups it can point at (an
// automatic url-test one, and a fallback one if it has an order), every
// rule, the more specific first, and a listener bound to the probe group.
func BuildConfig(in Input) ([]byte, error) {
	o := in.Options.Clone()
	o.Fill()
	// False also takes IPv6 off the TUN: the core then gives it no address,
	// and without one it doesn't route 2000::/3 or the rest of ::/0.
	ipv6 := o.IPv6.Enabled(in.IPv6Usable)
	dev := in.Dev
	// The core ignores rules outside rule mode, and with them the way to
	// this machine's own networks; see rulesFor.
	mode := in.Mode
	if !dev.Empty() {
		mode = "rule"
	}
	cfg := map[string]any{
		"mixed-port":          in.MixedPort,
		"allow-lan":           in.AllowLan,
		"mode":                mode,
		"log-level":           in.LogLevel,
		"ipv6":                ipv6,
		"unified-delay":       o.UnifiedDelay,
		"tcp-concurrent":      o.TCPConcurrent,
		"find-process-mode":   o.FindProcess,
		"keep-alive-interval": o.KeepAliveInterval,
		"disable-keep-alive":  o.DisableKeepAlive,
		"profile":             map[string]any{"store-selected": true},
		"geodata-mode":        true,
		"geo-auto-update":     o.GeoAutoUpdate,
		"geo-update-interval": o.GeoUpdateInterval,
		"geox-url":            map[string]any{"geoip": o.GeoIPURL, "geosite": o.GeoSiteURL, "mmdb": o.MMDBURL},
	}
	if o.HTTPPort > 0 {
		cfg["port"] = o.HTTPPort
	}
	if o.SocksPort > 0 {
		cfg["socks-port"] = o.SocksPort
	}
	if in.AllowLan {
		cfg["lan-allowed-ips"] = o.LanAllowedIPs
	}
	if len(o.Authentication) > 0 {
		cfg["authentication"] = o.Authentication
		// This Mac (the system proxy, Moho's own probes) needs no password.
		cfg["skip-auth-prefixes"] = []string{"127.0.0.1/8", "::1/128"}
	}
	if o.Interface != "" {
		cfg["interface-name"] = o.Interface
	}
	if o.Fingerprint != "" {
		cfg["global-client-fingerprint"] = o.Fingerprint
	}
	for k, v := range in.Controller {
		if v != "" {
			cfg[k] = v
		}
	}
	// The dashboards' controller, unless Moho's own already took TCP.
	if own, _ := in.Controller["external-controller"].(string); own == "" && o.ExtController != "" {
		cfg["external-controller"] = o.ExtController
		cfg["secret"] = o.ExtSecret
	}
	hosts := map[string]any{}
	// The DNS servers in use that Moho knows the addresses of; the lines
	// below, the user's among them, replace these.
	servers := slices.Concat(o.Nameservers, o.Fallback, o.ProxyNameservers)
	if len(o.Nameservers) == 0 {
		servers = append(servers, DefaultNameservers...)
	}
	for _, list := range dnsPolicy(o.NameserverPolicy, dev.Policy) {
		servers = append(servers, list...)
	}
	for _, s := range servers {
		if host := nameserverHost(s); len(NameserverHosts[host]) > 0 {
			hosts[host] = NameserverHosts[host]
		}
	}
	for k, v := range dev.Hosts {
		hosts[k] = v
	}
	for _, h := range o.Hosts {
		if k, v, ok := Pair(h); ok {
			hosts[k] = v
		}
	}
	if len(hosts) > 0 {
		cfg["hosts"] = hosts
	}
	if len(dev.Outbounds) > 0 {
		var out []any
		for _, b := range dev.Outbounds {
			out = append(out, map[string]any{"name": b.Name, "type": "direct", "interface-name": b.Iface})
		}
		cfg["proxies"] = out
	}
	if o.Sniff {
		sniffer := map[string]any{
			"enable": true, "override-destination": o.SniffOverride,
			"force-dns-mapping": o.SniffForceDNSMapping, "parse-pure-ip": o.SniffParsePureIP,
			"sniff": map[string]any{
				"HTTP": map[string]any{"ports": o.SniffHTTP},
				"TLS":  map[string]any{"ports": o.SniffTLS},
				"QUIC": map[string]any{"ports": o.SniffQUIC},
			},
			"skip-domain": o.SniffSkip,
		}
		if len(dev.SniffSkip) > 0 {
			sniffer["skip-dst-address"] = dev.SniffSkip
		}
		cfg["sniffer"] = sniffer
	}

	use := []string{}
	providers := map[string]any{}
	for _, p := range in.Providers {
		name := providerName(p.ID)
		use = append(use, name)
		providers[name] = map[string]any{
			"type":         "file",
			"path":         p.Path,
			"health-check": map[string]any{"enable": true, "url": in.TestURL, "interval": o.HealthInterval, "lazy": true},
			"override":     map[string]any{"additional-suffix": NodeSep + p.ID},
		}
	}
	if len(providers) > 0 {
		cfg["proxy-providers"] = providers
	}

	withNodes := func(g map[string]any, extra ...string) map[string]any {
		if len(use) > 0 {
			g["use"] = use
		}
		if len(extra) > 0 || len(use) == 0 {
			if len(extra) == 0 {
				extra = []string{"DIRECT"}
			}
			g["proxies"] = extra
		}
		return g
	}
	var groups []any
	var rules []string
	var err error
	if im := in.Imported; im != nil {
		if groups, err = im.runtimeGroups(); err != nil {
			return nil, err
		}
		rules = importedRules(in.Mode, dev, im.Rules)
		if len(im.Proxies) > 0 {
			out, _ := cfg["proxies"].([]any)
			for _, p := range im.Proxies {
				out = append(out, p)
			}
			cfg["proxies"] = out
		}
		for name, p := range im.ProxyProviders {
			if _, taken := providers[name]; taken {
				return nil, fmt.Errorf(i18n.T("「%s」这个名字是 bowel 自己用的", "The name %s is bowel's own"), name)
			}
			providers[name] = p
			use = append(use, name) // the probe has to reach these nodes too
		}
		if len(providers) > 0 {
			cfg["proxy-providers"] = providers
		}
		if len(im.RuleProviders) > 0 {
			cfg["rule-providers"] = im.RuleProviders
		}
		if len(im.SubRules) > 0 {
			cfg["sub-rules"] = im.SubRules
		}
	} else if groups, rules, err = templateRouting(in, o, dev, cfg, use, withNodes); err != nil {
		return nil, err
	}
	groups = append(groups, withNodes(map[string]any{"name": ProbeGroup, "type": "select"}, "DIRECT"))
	cfg["proxy-groups"] = groups
	cfg["rules"] = rules
	listeners := []any{map[string]any{
		"name": "moho-probe", "type": "mixed", "listen": "127.0.0.1", "port": in.ProbePort, "proxy": ProbeGroup,
	}}
	// A provider's own routing has none of the route groups these name.
	for _, e := range o.ExtraPorts {
		if exit, ok := exitOf(in.Routes, e.Route); ok && in.Imported == nil {
			listeners = append(listeners, map[string]any{
				"name": ExtraPortListener(e.Port), "type": "mixed", "listen": "127.0.0.1", "port": e.Port, "proxy": exit,
			})
		}
	}
	cfg["listeners"] = listeners

	if in.Tun {
		tun := map[string]any{
			"enable": true, "stack": o.TunStack, "auto-route": true, "auto-detect-interface": o.AutoDetectInterface,
			"strict-route": o.StrictRoute, "dns-hijack": o.DNSHijack, "mtu": o.TunMTU,
			"endpoint-independent-nat": o.EndpointIndependentNAT,
		}
		if o.TunDevice != "" {
			tun["device"] = o.TunDevice
		}
		if len(dev.TunExclude) > 0 {
			tun["route-exclude-address"] = dev.TunExclude
		}
		cfg["tun"] = tun
	}
	gateway := in.Tun && in.Gateway
	if gateway {
		o.DNSMode = "fake-ip"
	}
	policy := dnsPolicy(o.NameserverPolicy, dev.Policy)
	switch {
	case in.Tun || o.DNS:
		dns := map[string]any{
			"enable": true, "ipv6": ipv6, "enhanced-mode": o.DNSMode,
			"default-nameserver": o.DefaultNameservers,
			"nameserver":         o.Nameservers,
			"use-hosts":          o.UseHosts,
			"use-system-hosts":   o.UseSystemHosts,
			"prefer-h3":          o.PreferH3,
			"respect-rules":      o.RespectRules,
		}
		if o.DNSMode == "fake-ip" {
			dns["fake-ip-range"] = o.FakeIPRange
			dns["fake-ip-filter"] = devnet.Merge(o.FakeIPFilter, dev.FakeIPFilter)
		}
		if len(o.Fallback) > 0 {
			dns["fallback"] = o.Fallback
		}
		if len(o.ProxyNameservers) > 0 {
			dns["proxy-server-nameserver"] = o.ProxyNameservers
		}
		for _, pattern := range dev.SystemDNS {
			if _, ok := policy[pattern]; !ok {
				policy[pattern] = []string{"system"}
			}
		}
		if len(policy) > 0 {
			dns["nameserver-policy"] = policy
		}
		switch {
		case gateway:
			dns["listen"] = "0.0.0.0:53"
		case in.Tun:
			dns["listen"] = fmt.Sprintf("127.0.0.1:%d", LocalDNSPort)
		}
		cfg["dns"] = dns
	case len(dev.Policy) > 0:
		// Without a resolver of its own the core asks the system's servers
		// directly, which knows nothing of split DNS: this resolver is the
		// system's too, but for the domains that have servers of their own.
		cfg["dns"] = map[string]any{
			"enable": true, "ipv6": ipv6, "enhanced-mode": "normal", "nameserver": []string{"system"},
			"use-hosts": o.UseHosts, "use-system-hosts": o.UseSystemHosts, "nameserver-policy": policy,
		}
	default:
		// Neither TUN nor the built-in switch nor developer networks: the core
		// used to be given no resolver at all here. It needs one for the
		// nodes' own names, not only for the traffic's — a node whose server
		// is a domain cannot be dialled without it, and ECH asks DNS for its
		// config before the TLS handshake starts — so exactly those nodes
		// failed to test while every other provider was fine. Normal mode
		// answers with real addresses: with no tunnel there is nothing to map
		// fake ones back from.
		ns := o.Nameservers
		if len(ns) == 0 {
			ns = DefaultNameservers
		}
		dns := map[string]any{
			"enable": true, "ipv6": ipv6, "enhanced-mode": "normal", "nameserver": ns,
			"use-hosts": o.UseHosts, "use-system-hosts": o.UseSystemHosts,
		}
		if len(policy) > 0 {
			dns["nameserver-policy"] = policy
		}
		cfg["dns"] = dns
	}
	return yaml.Marshal(cfg)
}

// templateRouting is the groups and rules of Moho's own route groups.
func templateRouting(in Input, o Options, dev devnet.Overlay, cfg map[string]any, use []string, withNodes func(map[string]any, ...string) map[string]any) ([]any, []string, error) {
	var groups []any
	for _, r := range in.Routes {
		if r.OwnExit() {
			groups = append(groups, map[string]any{"name": routes.Group(r), "type": "select", "proxies": []string{"DIRECT"}})
		}
		if !r.NodeGroups() {
			continue
		}
		mainURL := in.MainURLs[r.ID]
		if mainURL == "" {
			mainURL = in.TestURL
		}
		auto := map[string]any{"name": routes.Auto(r), "type": "url-test", "url": mainURL, "interval": o.URLTestInterval, "tolerance": o.URLTestTolerance, "lazy": true}
		if names, scoped := in.Scopes[r.ID]; !scoped {
			auto = withNodes(auto)
		} else if len(names) == 0 {
			auto["proxies"] = []string{"REJECT"} // nothing in range: no way out, not another way out
		} else {
			auto["use"], auto["filter"] = use, exactly(names)
		}
		groups = append(groups, auto)
		members := []string{routes.Auto(r)}
		if order, ok := in.Orders[r.ID]; ok {
			backup := map[string]any{"name": routes.Backup(r), "type": "fallback", "url": mainURL, "interval": o.URLTestInterval, "lazy": true}
			var steps []string
			for i, n := range order {
				step := routes.BackupStep(r, i)
				steps = append(steps, step)
				groups = append(groups, map[string]any{"name": step, "type": "select", "use": []string{providerName(providerOf(n))}, "filter": exactly([]string{n})})
			}
			if len(steps) == 0 {
				steps = []string{"REJECT"}
			}
			backup["proxies"] = steps
			groups = append(groups, backup)
			members = append(members, routes.Backup(r))
		}
		groups = append(groups, withNodes(map[string]any{"name": routes.Group(r), "type": "select"}, members...))
	}
	sets := map[string]any{}
	for _, s := range in.RuleSets {
		sets[routes.RuleSetProvider(s.ID)] = map[string]any{"type": "file", "behavior": s.Behavior, "format": "yaml", "path": s.Path}
	}
	if len(sets) > 0 {
		cfg["rule-providers"] = sets
	}
	var mine, rest []string
	for _, l := range routes.Lines(in.Routes, in.UserRules) {
		if _, ok := sets[routes.RuleSetProvider(l.Set)]; l.Set != "" && !ok {
			continue
		}
		target, ok := exitOf(in.Routes, l.Route)
		if !ok {
			continue
		}
		if l.Mine && dev.Covers(l.Text) {
			mine = append(mine, withTarget(l.Text, target))
		} else {
			rest = append(rest, withTarget(l.Text, target))
		}
	}
	var def routes.Route
	for _, r := range in.Routes {
		if r.ID == routes.Default {
			def = r
		}
	}
	if def.Name == "" {
		return nil, nil, errors.New(i18n.T("模板里没有默认路由", "The template has no default route"))
	}
	match := routes.Group(def)
	if def.Direct {
		match = "DIRECT"
	}
	return groups, rulesFor(in.Mode, dev, mine, rest, match), nil
}

// exitOf is what the core sends route id's traffic to: the route's group,
// or DIRECT or REJECT for a route that has none. False if there is no such
// route.
func exitOf(all []routes.Route, id string) (string, bool) {
	for _, r := range all {
		if r.ID != id {
			continue
		}
		switch {
		case r.Reject:
			return "REJECT", true
		case r.OwnExit(): // DIRECT, by its own group
		case r.Direct:
			return "DIRECT", true
		}
		return routes.Group(r), true
	}
	return "", false
}

// ExtraPortListener is the name of the listener of an extra port.
func ExtraPortListener(port int) string { return fmt.Sprintf("bowel-port-%d", port) }

// rulesFor is the rule list. Outside rule mode the core would ignore the
// list and send everything one way, so the mode becomes the list's last
// line and the routes' rules are left out: the way to this machine's own
// networks stays, and all else goes as the mode says.
func rulesFor(mode string, dev devnet.Overlay, mine, rest []string, def string) []string {
	var own []string
	for _, r := range dev.Rules {
		own = append(own, withTarget(r.Text, r.Target))
	}
	switch {
	case dev.Empty() || mode == "rule":
		return slices.Concat(mine, own, rest, []string{"MATCH," + def})
	case mode == "global":
		return append(own, "MATCH,GLOBAL")
	}
	return append(own, "MATCH,DIRECT")
}

// dnsPolicy is the user's "pattern=server" entries, and those of dev for
// the patterns the user says nothing about.
func dnsPolicy(user []string, dev []devnet.Policy) map[string][]string {
	policy := map[string][]string{}
	for _, p := range user {
		if k, v, ok := Pair(p); ok {
			policy[k] = append(policy[k], v)
		}
	}
	mine := maps.Clone(policy)
	for _, p := range dev {
		if _, ok := mine[p.Pattern]; !ok {
			policy[p.Pattern] = append(policy[p.Pattern], p.Server)
		}
	}
	return policy
}
