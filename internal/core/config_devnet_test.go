package core

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/oboo/moho/internal/devnet"
	"github.com/oboo/moho/internal/routes"
)

func devEnv() devnet.Env {
	return devnet.Env{
		Tailscale: devnet.Tailscale{
			Installed: true, State: "Running", Iface: "utun4", Suffix: "tail17b34.ts.net", MagicDNS: true,
			Peers: []devnet.Peer{{Name: "mybox", FQDN: "mybox.tail17b34.ts.net", IPs: []netip.Addr{netip.MustParseAddr("100.100.1.2")}}},
		},
		Resolvers: []devnet.Resolver{{Domain: "aws", Servers: []string{"127.0.0.1:5053"}}},
	}
}

func devInput(mode string, dev devnet.Overlay) Input {
	return Input{
		Routes: routes.Template(), Mode: mode, MixedPort: 7899, LogLevel: "info", TestURL: "http://x/", ProbePort: 1,
		UserRules: []routes.UserRule{
			{Kind: routes.KindSuffix, Value: "wiki.corp", Route: routes.Default},
			{Kind: routes.KindSuffix, Value: "example.org", Route: routes.Default},
		},
		Options: DefaultOptions.Clone(), Dev: dev,
	}
}

func build(t *testing.T, in Input) map[string]any {
	t.Helper()
	b, err := BuildConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func strs(v any) []string {
	l, _ := v.([]any)
	out := make([]string, len(l))
	for i, s := range l {
		out[i], _ = s.(string)
	}
	return out
}

func index(t *testing.T, rules []string, prefix string) int {
	t.Helper()
	i := slices.IndexFunc(rules, func(r string) bool { return strings.HasPrefix(r, prefix) })
	if i < 0 {
		t.Fatalf("no %s in %v", prefix, rules)
	}
	return i
}

func TestRuleOrderWithOverlay(t *testing.T) {
	dev := devnet.Compile(devnet.Options{}, devEnv())
	rules := strs(build(t, devInput("rule", dev))["rules"])
	mine, own, public := index(t, rules, "DOMAIN-SUFFIX,wiki.corp"), index(t, rules, "DOMAIN-SUFFIX,corp,DIRECT"), index(t, rules, "DOMAIN-SUFFIX,example.org")
	if mine >= own || own >= public {
		t.Errorf("order: %d %d %d", mine, own, public)
	}
	if !strings.HasPrefix(rules[len(rules)-1], "MATCH,") {
		t.Errorf("last: %s", rules[len(rules)-1])
	}
	if !slices.Contains(rules, "IP-CIDR,100.64.0.0/10,Tailscale-net,no-resolve") {
		t.Errorf("no-resolve goes last: %v", rules[:12])
	}
}

func TestGlobalKeepsOverlay(t *testing.T) {
	dev := devnet.Compile(devnet.Options{}, devEnv())
	for mode, last := range map[string]string{"global": "MATCH,GLOBAL", "direct": "MATCH,DIRECT"} {
		cfg := build(t, devInput(mode, dev))
		rules := strs(cfg["rules"])
		if cfg["mode"] != "rule" || rules[len(rules)-1] != last {
			t.Errorf("%s: mode %v, last %s", mode, cfg["mode"], rules[len(rules)-1])
		}
		if slices.ContainsFunc(rules, func(r string) bool { return strings.Contains(r, "example.org") }) {
			t.Errorf("%s keeps the routes' rules", mode)
		}
		index(t, rules, "DOMAIN-SUFFIX,ts.net,Tailscale-net")
	}
	if cfg := build(t, devInput("global", devnet.Overlay{})); cfg["mode"] != "global" {
		t.Errorf("off: mode %v", cfg["mode"])
	}
}

func TestDevNetworkDNSWithoutTun(t *testing.T) {
	in := devInput("rule", devnet.Compile(devnet.Options{}, devEnv()))
	dns, _ := build(t, in)["dns"].(map[string]any)
	if dns == nil || dns["enhanced-mode"] != "normal" || dns["listen"] != nil || !slices.Equal(strs(dns["nameserver"]), []string{"system"}) {
		t.Fatalf("%+v", dns)
	}
	policy, _ := dns["nameserver-policy"].(map[string]any)
	if got := strs(policy["+.aws"]); !slices.Equal(got, []string{"127.0.0.1:5053"}) {
		t.Errorf("policy %+v", policy)
	}
	// Nothing split: the resolver is still there — the nodes' own names need
	// one — but it carries no policy of the overlay's.
	alone, _ := build(t, devInput("rule", devnet.Compile(devnet.Options{}, devnet.Env{})))["dns"].(map[string]any)
	if alone == nil || alone["enable"] != true {
		t.Fatalf("no resolver with nothing split: %v", alone)
	}
	if _, ok := alone["nameserver-policy"]; ok {
		t.Errorf("a catalog alone brings a policy: %+v", alone)
	}
}

func TestDevNetworkTun(t *testing.T) {
	in := devInput("rule", devnet.Compile(devnet.Options{}, devEnv()))
	in.Tun = true
	in.NameserverPolicy = []string{"+.aws=9.9.9.9"}
	in.Hosts = []string{"mybox=1.2.3.4"}
	in.FakeIPFilter = []string{"*.mine"}
	in.Sniff = true
	cfg := build(t, in)
	tun, _ := cfg["tun"].(map[string]any)
	if !slices.Contains(strs(tun["route-exclude-address"]), "100.64.0.0/10") {
		t.Errorf("tun %+v", tun)
	}
	dns, _ := cfg["dns"].(map[string]any)
	policy, _ := dns["nameserver-policy"].(map[string]any)
	if got := strs(policy["+.aws"]); !slices.Equal(got, []string{"9.9.9.9"}) {
		t.Errorf("the user's policy wins: %v", got)
	}
	if got := strs(policy["+.lan"]); !slices.Equal(got, []string{"system"}) {
		t.Errorf("nas.lan is known only to the LAN's DNS: %v", got)
	}
	if got := strs(policy["+.ts.net"]); !slices.Equal(got, []string{"100.100.100.100#Tailscale-net"}) {
		t.Errorf("ts.net: %v", got)
	}
	filter := strs(dns["fake-ip-filter"])
	if filter[0] != "*.mine" || !slices.Contains(filter, "+.tail17b34.ts.net") || !slices.Contains(filter, "mybox") {
		t.Errorf("filter %v", filter)
	}
	hosts, _ := cfg["hosts"].(map[string]any)
	if hosts["mybox"] != "1.2.3.4" {
		t.Errorf("the user's hosts win: %v", hosts)
	}
	if !slices.Contains(strs(cfg["sniffer"].(map[string]any)["skip-dst-address"]), "100.64.0.0/10") {
		t.Error("sniffer")
	}
	px, _ := cfg["proxies"].([]any)
	if len(px) != 1 || px[0].(map[string]any)["interface-name"] != "utun4" {
		t.Errorf("proxies %+v", px)
	}
}

func TestDevNetworkOffLeavesConfigAlone(t *testing.T) {
	cfg := build(t, devInput("rule", devnet.Overlay{}))
	if _, ok := cfg["proxies"]; ok {
		t.Errorf("proxies is there with nothing to add")
	}
	// The only hosts are the resolver's own servers.
	hosts, _ := cfg["hosts"].(map[string]any)
	for k := range hosts {
		if _, ok := NameserverHosts[k]; !ok {
			t.Errorf("hosts has %s with nothing to add", k)
		}
	}
	// The resolver is the core's own, not the overlay's: it is there with
	// nothing to add, and carries none of the overlay.
	dns, _ := cfg["dns"].(map[string]any)
	if dns == nil || dns["enable"] != true {
		t.Fatalf("no resolver of the core's own: %v", dns)
	}
	if _, ok := dns["nameserver-policy"]; ok {
		t.Errorf("the overlay left a policy behind: %+v", dns)
	}
}

func TestDevOptionsValidate(t *testing.T) {
	o := DefaultOptions.Clone()
	o.DevExtra, o.DevOffScenarios = []string{"corp.example", "10.8.0.0/16"}, []string{"mesh"}
	if err := o.Validate(); err != nil {
		t.Error(err)
	}
	o.DevExtra = []string{"a b"}
	if o.Validate() == nil {
		t.Error("a b is no domain")
	}
	o.DevExtra, o.DevOffScenarios = nil, []string{"nope"}
	if o.Validate() == nil {
		t.Error("nope is no scenario")
	}
}
