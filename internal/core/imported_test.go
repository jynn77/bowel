package core

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/oboo/moho/internal/devnet"
	"github.com/oboo/moho/internal/routes"
)

// issue33 is the example of the issue: a group nested in a group.
const issue33 = `
proxies:
  - {name: JP-01, type: ss, server: 127.0.0.1, port: 18001, cipher: aes-128-gcm, password: x}
  - {name: JP-02, type: ss, server: 127.0.0.1, port: 18002, cipher: aes-128-gcm, password: x}
proxy-groups:
  - name: Japan
    type: url-test
    proxies: [JP-01, JP-02]
    url: https://www.gstatic.com/generate_204
    interval: 300
  - name: Streaming
    type: select
    proxies: [Japan, DIRECT]
rules:
  - DOMAIN-SUFFIX,netflix.com,Streaming
  - MATCH,Japan
mixed-port: 1
external-controller: 0.0.0.0:9090
secret: theirs
listeners:
  - {name: back-door, type: mixed, port: 7777}
script:
  shortcuts: {x: "true"}
dns: {enable: true, nameserver: [1.2.3.4]}
tun: {enable: true}
`

func mustParse(t *testing.T, src string) *Imported {
	t.Helper()
	im, err := ParseImported([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	if im == nil {
		t.Fatal("no routing found")
	}
	return im
}

func buildImported(t *testing.T, in Input) map[string]any {
	t.Helper()
	in.MixedPort, in.Mode, in.LogLevel, in.TestURL, in.ProbePort = 7899, "rule", "info", "http://t", 7900
	if in.Routes == nil {
		in.Routes = routes.Template()
	}
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

func groupNamed(t *testing.T, cfg map[string]any, name string) map[string]any {
	t.Helper()
	for _, g := range cfg["proxy-groups"].([]any) {
		if m := g.(map[string]any); m["name"] == name {
			return m
		}
	}
	t.Fatalf("no group %s", name)
	return nil
}

func TestImportedKeepsGroupsAndRulesAsWritten(t *testing.T) {
	cfg := buildImported(t, Input{Imported: mustParse(t, issue33), Providers: []ProviderRef{{ID: "abc", Path: "/x"}}})
	g := groupNamed(t, cfg, "Streaming")
	if g["type"] != "select" || !slices.Equal(stringsOf(g["proxies"]), []string{"Japan", "DIRECT"}) {
		t.Errorf("Streaming = %v", g)
	}
	j := groupNamed(t, cfg, "Japan")
	if j["type"] != "url-test" || j["interval"] != 300 || !slices.Equal(stringsOf(j["proxies"]), []string{"JP-01", "JP-02"}) {
		t.Errorf("Japan = %v", j)
	}
	if got := stringsOf(cfg["rules"]); !slices.Equal(got, []string{"DOMAIN-SUFFIX,netflix.com,Streaming", "MATCH,Japan"}) {
		t.Errorf("rules = %v", got)
	}
	var names []string
	for _, p := range cfg["proxies"].([]any) {
		names = append(names, p.(map[string]any)["name"].(string))
	}
	if !slices.Equal(names, []string{"JP-01", "JP-02"}) {
		t.Errorf("nodes must keep their names at the top level: %v", names)
	}
	if _, ok := cfg["proxy-providers"].(map[string]any)["p-abc"]; !ok {
		t.Error("the node list needs the provider still")
	}
}

// TestImportedTakesOnlyRouting: nothing but routing comes from the source.
func TestImportedTakesOnlyRouting(t *testing.T) {
	cfg := buildImported(t, Input{Imported: mustParse(t, issue33)})
	if cfg["mixed-port"] != 7899 || cfg["secret"] != nil || cfg["external-controller"] != nil || cfg["script"] != nil || cfg["tun"] != nil {
		t.Errorf("the source's settings got in: %v", cfg)
	}
	ls := cfg["listeners"].([]any)
	if len(ls) != 1 || ls[0].(map[string]any)["name"] != "moho-probe" {
		t.Errorf("listeners = %v", ls)
	}
	if ns := cfg["dns"].(map[string]any)["nameserver"]; slices.Contains(stringsOf(ns), "1.2.3.4") {
		t.Errorf("the source's DNS got in: %v", ns)
	}
	groupNamed(t, cfg, ProbeGroup)
}

// TestImportedPausesUserRules: the user's rules and rule sets name groups
// the source doesn't have.
func TestImportedPausesUserRules(t *testing.T) {
	cfg := buildImported(t, Input{
		Imported:  mustParse(t, issue33),
		UserRules: []routes.UserRule{{Kind: routes.KindDomain, Value: "mine.example", Route: "ai"}},
		RuleSets:  []RuleSetRef{{ID: "x", Path: "/x", Behavior: "classical"}},
	})
	if got := strings.Join(stringsOf(cfg["rules"]), "\n"); strings.Contains(got, "mine.example") {
		t.Errorf("rules = %s", got)
	}
	if cfg["rule-providers"] != nil {
		t.Errorf("rule-providers = %v", cfg["rule-providers"])
	}
}

func TestImportedDevNetworksComeFirst(t *testing.T) {
	dev := devnet.Overlay{Rules: []devnet.Rule{{Text: "IP-CIDR,10.0.0.0/8,no-resolve", Target: "DIRECT"}}}
	got := stringsOf(buildImported(t, Input{Imported: mustParse(t, issue33), Dev: dev})["rules"])
	if got[0] != "IP-CIDR,10.0.0.0/8,DIRECT,no-resolve" || got[len(got)-1] != "MATCH,Japan" {
		t.Errorf("rules = %v", got)
	}
	in := Input{Imported: mustParse(t, issue33), Dev: dev}
	in.MixedPort, in.Mode, in.LogLevel, in.TestURL, in.ProbePort = 7899, "global", "info", "http://t", 7900
	in.Routes = routes.Template()
	b, err := BuildConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "MATCH,GLOBAL") || strings.Contains(string(b), "netflix") {
		t.Errorf("global mode keeps only the networks' way out:\n%s", b)
	}
}

func TestImportedRefusesWhatCannotWork(t *testing.T) {
	head := "proxies:\n  - {name: A, type: direct}\n"
	for name, src := range map[string]string{
		"member":   head + "proxy-groups:\n  - {name: G, type: select, proxies: [Nope]}\nrules: [MATCH,G]\n",
		"target":   head + "proxy-groups:\n  - {name: G, type: select, proxies: [A]}\nrules: [MATCH,Nope]\n",
		"use":      head + "proxy-groups:\n  - {name: G, type: select, use: [nope]}\nrules: [MATCH,G]\n",
		"ruleset":  head + "proxy-groups:\n  - {name: G, type: select, proxies: [A]}\nrules: ['RULE-SET,nope,G', 'MATCH,G']\n",
		"norules":  head + "proxy-groups:\n  - {name: G, type: select, proxies: [A]}\n",
		"empty":    head + "proxy-groups:\n  - {name: G, type: select}\nrules: [MATCH,G]\n",
		"filter":   head + "proxy-groups:\n  - {name: G, type: select, proxies: [A], filter: '('}\nrules: [MATCH,G]\n",
		"reserved": head + "proxy-groups:\n  - {name: MOHO-PROBE, type: select, proxies: [A]}\nrules: [MATCH,MOHO-PROBE]\n",
		"filesets": head + "proxy-groups:\n  - {name: G, type: select, proxies: [A]}\nrule-providers:\n  r: {type: file, path: ./r.yaml, behavior: domain}\nrules: ['RULE-SET,r,G', 'MATCH,G']\n",
		"filenode": head + "proxy-providers:\n  n: {type: file, path: ./n.yaml}\nproxy-groups:\n  - {name: G, type: select, use: [n]}\nrules: [MATCH,G]\n",
	} {
		if im, err := ParseImported([]byte(src), nil); err == nil {
			t.Errorf("%s: accepted: %+v", name, im)
		}
	}
}

func TestImportedOfNodeListIsNone(t *testing.T) {
	for _, src := range []string{"proxies:\n  - {name: A, type: direct}\n", "vmess://abcd", "dmVzczovL2FiY2Q="} {
		if im, err := ParseImported([]byte(src), nil); im != nil || err != nil {
			t.Errorf("%q: %v, %v", src, im, err)
		}
	}
}

// TestImportedRemoteProviders: remote lists go along, to the core's own
// place; those nothing uses stay behind, even local ones.
func TestImportedRemoteProviders(t *testing.T) {
	im := mustParse(t, `
proxy-providers:
  air: {type: http, url: "https://a/x", path: /etc/passwd, interval: 3600}
  spare: {type: file, path: ./x.yaml}
rule-providers:
  ads: {type: http, url: "https://a/r", path: ../../evil, behavior: domain}
  unused: {type: file, path: ./r.yaml, behavior: domain}
  inl: {type: inline, behavior: domain, payload: [a.example]}
proxy-groups:
  - {name: G, type: select, use: [air]}
rules:
  - RULE-SET,ads,REJECT
  - RULE-SET,inl,G
  - MATCH,G
`)
	cfg := buildImported(t, Input{Imported: im, Providers: []ProviderRef{{ID: "abc", Path: "/x"}}})
	pp := cfg["proxy-providers"].(map[string]any)
	if pp["air"].(map[string]any)["path"] != nil || pp["spare"] != nil {
		t.Errorf("proxy-providers = %v", pp)
	}
	rp := cfg["rule-providers"].(map[string]any)
	if rp["ads"].(map[string]any)["path"] != nil || rp["unused"] != nil || rp["inl"] == nil {
		t.Errorf("rule-providers = %v", rp)
	}
	if use := stringsOf(groupNamed(t, cfg, ProbeGroup)["use"]); !slices.Contains(use, "air") {
		t.Errorf("the probe can't reach the source's nodes: %v", use)
	}
}

// TestImportedIncludeAll: only the source's own nodes and providers, not
// Moho's providers or the developer networks' outbounds.
func TestImportedIncludeAll(t *testing.T) {
	im := mustParse(t, `
proxies:
  - {name: JP-01, type: direct}
  - {name: US-01, type: direct}
proxy-providers:
  air: {type: http, url: "https://a/x"}
proxy-groups:
  - {name: All, type: select, include-all: true}
  - {name: JP, type: url-test, include-all-proxies: true, filter: "(?i)^(?!.*us).*jp", url: "http://t", interval: 60}
  - {name: Empty, type: select, include-all-proxies: true, filter: "nothing"}
  - {name: Pv, type: select, include-all-providers: true, proxies: [DIRECT]}
rules:
  - MATCH,All
`)
	dev := devnet.Overlay{Outbounds: []devnet.Outbound{{Name: "dev-tail", Iface: "utun9"}}}
	cfg := buildImported(t, Input{Imported: im, Dev: dev, Providers: []ProviderRef{{ID: "abc", Path: "/x"}}})
	all := groupNamed(t, cfg, "All")
	if all["include-all"] != nil || !slices.Equal(stringsOf(all["proxies"]), []string{"JP-01", "US-01"}) || !slices.Equal(stringsOf(all["use"]), []string{"air"}) {
		t.Errorf("All = %v", all)
	}
	if got := stringsOf(groupNamed(t, cfg, "JP")["proxies"]); !slices.Equal(got, []string{"JP-01"}) {
		t.Errorf("JP = %v", got)
	}
	if got := stringsOf(groupNamed(t, cfg, "Empty")["proxies"]); !slices.Equal(got, []string{"COMPATIBLE"}) {
		t.Errorf("Empty = %v", got)
	}
	pv := groupNamed(t, cfg, "Pv")
	if !slices.Equal(stringsOf(pv["proxies"]), []string{"DIRECT"}) || !slices.Equal(stringsOf(pv["use"]), []string{"air"}) {
		t.Errorf("Pv = %v", pv)
	}
}

func TestImportedTargets(t *testing.T) {
	im := mustParse(t, `
proxies:
  - {name: A, type: direct}
proxy-groups:
  - {name: G, type: select, proxies: [A]}
  - {name: H, type: select, proxies: [G]}
sub-rules:
  s:
    - DOMAIN,x.example,H
rules:
  - DOMAIN-REGEX,a{1,2}.example,G
  - AND,((DOMAIN,a.example),(NETWORK,UDP)),REJECT
  - SUB-RULE,(NETWORK,TCP),s
  - DOMAIN,z.example,G
  - MATCH,G
`)
	if got := im.Targets(); !slices.Equal(got, []string{"G", "REJECT", "H"}) {
		t.Errorf("targets = %v", got)
	}
	if im.Match() != "G" || im.RulesTo("G") != 3 || im.RulesTo("H") != 1 {
		t.Errorf("match %q, to G %d, to H %d", im.Match(), im.RulesTo("G"), im.RulesTo("H"))
	}
}

// TestImportedTheCoreAccepts runs the bundled core over the example, the way
// Apply does.
func TestImportedTheCoreAccepts(t *testing.T) {
	bin := filepath.Join("..", "..", "resources", runtime.GOOS+"-"+runtime.GOARCH, "bin", "mihomo")
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no bundled core")
	}
	bin, _ = filepath.Abs(bin)
	dir := t.TempDir()
	sub := filepath.Join(dir, "abc.txt")
	if err := os.WriteFile(sub, []byte(issue33), 0o600); err != nil {
		t.Fatal(err)
	}
	in := Input{Imported: mustParse(t, issue33), Providers: []ProviderRef{{ID: "abc", Path: sub}}, Routes: routes.Template(), MixedPort: 17899, Mode: "rule", LogLevel: "info", TestURL: "http://t", ProbePort: 17900}
	b, err := BuildConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if geo := filepath.Join("..", "..", "resources", "geo"); true {
		for _, f := range []string{"GeoIP.dat", "GeoSite.dat"} {
			if d, err := os.ReadFile(filepath.Join(geo, f)); err == nil {
				_ = os.WriteFile(filepath.Join(dir, f), d, 0o600)
			}
		}
	}
	cmd := exec.Command(bin, "-t", "-d", dir, "-f", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// TestImportedLocalFileProviders: a `type: file` provider is read from the
// file kept under its name, never from the path the config wrote.
func TestImportedLocalFileProviders(t *testing.T) {
	src := `
proxy-providers:
  nodes: {type: file, path: /etc/passwd, path-in-bundle: x, filter: JP, health-check: {enable: true, url: "http://t", interval: 300}}
  spare: {type: file, path: ./x.yaml}
proxy-groups:
  - {name: G, type: select, use: [nodes]}
rules: ['MATCH,G']
`
	var asked []string
	have := map[string][]string{"nodes": {"/store/abc.txt"}}
	local := func(name string) []string { asked = append(asked, name); return have[name] }
	im, err := ParseImported([]byte(src), local)
	if err != nil || im == nil {
		t.Fatal(im, err)
	}
	if !slices.Equal(asked, []string{"nodes"}) {
		t.Errorf("asked for %v: an unused provider needs no file", asked)
	}
	cfg := buildImported(t, Input{Imported: im})
	n := cfg["proxy-providers"].(map[string]any)["nodes"].(map[string]any)
	if n["type"] != "file" || n["path"] != "/store/abc.txt" || n["path-in-bundle"] != nil || n["filter"] != "JP" || n["health-check"] == nil {
		t.Errorf("nodes = %v", n)
	}
	if use := stringsOf(groupNamed(t, cfg, "G")["use"]); !slices.Equal(use, []string{"nodes"}) {
		t.Errorf("use = %v", use)
	}

	have = nil
	_, err = ParseImported([]byte(src), local)
	var missing *MissingFile
	if !errors.As(err, &missing) || missing.Name != "nodes" {
		t.Errorf("err = %v", err)
	}
	have = map[string][]string{"nodes": {"/a", "/b"}}
	if _, err = ParseImported([]byte(src), local); err == nil || errors.As(err, &missing) {
		t.Errorf("two files under one name: %v", err)
	}
	if _, err = ParseImported([]byte(src), nil); err == nil || errors.As(err, &missing) {
		t.Errorf("no store: %v", err)
	}
}
