package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oboo/moho/internal/locate"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/routes"
	"gopkg.in/yaml.v3"
)

const nodesYAML = `proxies:
  - {name: 东京 01, type: ss, server: 192.0.2.1, port: 8388, cipher: aes-128-gcm, password: x}
  - {name: 香港 02, type: ss, server: 192.0.2.2, port: 8388, cipher: aes-128-gcm, password: x}
`

func testBridge(t *testing.T) *bridge {
	t.Helper()
	dir := t.TempDir()
	store, err := provider.Open(filepath.Join(dir, "providers"))
	if err != nil {
		t.Fatal(err)
	}
	set, err := openSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	x := &bridge{dir: dir, store: store, set: set, loc: locate.New(""), sink: discard{}, vpn: "off", lastNode: map[string]string{}}
	file := filepath.Join(dir, "nodes.yaml")
	if err := os.WriteFile(file, []byte(nodesYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := x.addFile(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	return x
}

type discard struct{}

func (discard) Emit(string, string) {}

func group(t *testing.T, x *bridge, name string) groupData {
	t.Helper()
	for _, g := range x.routing().Groups {
		if g.Name == name {
			return g
		}
	}
	t.Fatalf("no group %q", name)
	return groupData{}
}

func config(t *testing.T, x *bridge) map[string]any {
	t.Helper()
	if err := x.prepareTunnel(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(x.dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// A group made on the phone reaches the core, with its range and rules.
func TestGroupReachesConfig(t *testing.T) {
	x := testBridge(t)
	ctx := context.Background()
	if err := x.routeOp(ctx, callArgs{Op: "newGroup", Name: "工作"}); err != nil {
		t.Fatal(err)
	}
	g := group(t, x, "工作")
	if err := x.routeOp(ctx, callArgs{Op: "setScope", ID: g.ID, Regions: []string{"JP"}}); err != nil {
		t.Fatal(err)
	}
	if err := x.routeOp(ctx, callArgs{Op: "addRule", Kind: routes.KindSuffix, Text: "https://work.example.com/a", Route: g.ID}); err != nil {
		t.Fatal(err)
	}
	cfg := config(t, x)
	raw, _ := json.Marshal(cfg)
	if !strings.Contains(string(raw), "DOMAIN-SUFFIX,work.example.com,"+g.ID) {
		t.Fatalf("rule missing from the config: %s", raw)
	}
	found := false
	for _, it := range cfg["proxy-groups"].([]any) {
		m := it.(map[string]any)
		if m["name"] != routes.Auto(routes.Route{Name: g.ID}) {
			continue
		}
		found = true
		list, _ := json.Marshal(m)
		if !strings.Contains(string(list), "东京") || strings.Contains(string(list), "香港") {
			t.Fatalf("range not applied: %s", list)
		}
	}
	if !found {
		t.Fatalf("no automatic group for %s: %s", g.ID, raw)
	}
	tun := cfg["tun"].(map[string]any)
	if tun["stack"] != "gvisor" || cfg["mixed-port"] != nil {
		t.Fatalf("phone overrides lost: %v", cfg)
	}
}

// The same rule twice is refused; in another service it offers to move.
func TestRuleConflict(t *testing.T) {
	x := testBridge(t)
	ctx := context.Background()
	add := callArgs{Op: "addRule", Kind: routes.KindSuffix, Text: "a.example.com", Route: routes.Default}
	if err := x.routeOp(ctx, add); err != nil {
		t.Fatal(err)
	}
	if err := x.routeOp(ctx, add); err == nil || strings.HasPrefix(err.Error(), "conflict:") {
		t.Fatalf("second add = %v, want already there", err)
	}
	other := add
	other.Route = "direct"
	var conflict *conflictError
	if err := x.routeOp(ctx, other); !errors.As(err, &conflict) {
		t.Fatalf("add elsewhere = %v, want a conflict", err)
	}
	other.Op = "moveRule"
	if err := x.routeOp(ctx, other); err != nil {
		t.Fatal(err)
	}
	if got := x.explain("a.example.com"); got.Route != "direct" {
		t.Fatalf("explain = %+v, want direct", got)
	}
}

// A rule on a built-in service stays in it, and the kind can be left to guess.
func TestRuleOnBuiltinServiceDetectsKind(t *testing.T) {
	x := testBridge(t)
	ai := group(t, x, "AI 服务")
	var sv serviceData
	for _, s := range ai.Services {
		if s.Builtin {
			sv = s
			break
		}
	}
	if sv.ID == "" {
		t.Fatal("no built-in service")
	}
	if err := x.routeOp(context.Background(), callArgs{Op: "addRule", Text: "https://new.example.com/x", Route: ai.ID, Service: sv.ID}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range group(t, x, "AI 服务").Services {
		for _, r := range s.Rules {
			if r.Value == "new.example.com" {
				found = s.ID == sv.ID && r.Kind == routes.KindSuffix
			}
		}
	}
	if !found {
		t.Fatal("rule did not land in the built-in service as a suffix")
	}
}

func TestParseRuleForPreview(t *testing.T) {
	if p := parseRule("", "https://a.example.com/x"); p.Error != "" || p.Value != "a.example.com" || p.KindName != routes.KindName(routes.KindSuffix) {
		t.Fatalf("link = %+v", p)
	}
	if p := parseRule("", "10.0.0.0/8"); p.Error != "" || p.KindName != routes.KindName(routes.KindIP) {
		t.Fatalf("range = %+v", p)
	}
	if p := parseRule(routes.KindIP, "example.com"); p.Error == "" {
		t.Fatalf("a domain passed as an IP: %+v", p)
	}
}

// Deleting a group takes its test sites along and hands on its services.
func TestDeleteGroupClearsTargets(t *testing.T) {
	x := testBridge(t)
	ctx := context.Background()
	if err := x.routeOp(ctx, callArgs{Op: "newGroup", Name: "工作"}); err != nil {
		t.Fatal(err)
	}
	g := group(t, x, "工作")
	if err := x.setTargets(ctx, g.ID, []routes.Target{{Name: "a", URL: "https://a.example.com/"}}); err != nil {
		t.Fatal(err)
	}
	if err := x.routeOp(ctx, callArgs{Op: "deleteGroup", ID: g.ID, To: routes.Default}); err != nil {
		t.Fatal(err)
	}
	if _, ok := x.set.Get().RouteTargets[g.ID]; ok {
		t.Fatal("the deleted group's test sites stayed")
	}
}

// 测试一个地址 and an activity row both lead to the service holding the rule.
func TestRuleLeadsToItsService(t *testing.T) {
	x := testBridge(t)
	ctx := context.Background()
	if err := x.routeOp(ctx, callArgs{Op: "newService", ID: routes.Default, Name: "工作站"}); err != nil {
		t.Fatal(err)
	}
	var id string
	for _, s := range group(t, x, "其他流量").Services {
		if s.Name == "工作站" {
			id = s.ID
		}
	}
	if err := x.routeOp(ctx, callArgs{Op: "addRule", Kind: routes.KindSuffix, Text: "work.example.com", Route: routes.Default, Service: id}); err != nil {
		t.Fatal(err)
	}
	if got := x.explain("work.example.com"); got.ServiceID != id {
		t.Fatalf("explain = %+v, want service %s", got, id)
	}
	if got := x.ruleHome("DomainSuffix", "work.example.com"); got.Group != routes.Default || got.Service != id {
		t.Fatalf("home = %+v", got)
	}
	if got := x.ruleHome("DomainSuffix", "nowhere.example.com"); got.Group != "" {
		t.Fatalf("home of an unknown rule = %+v", got)
	}
}

// Today's share counts the bytes each tick adds, by route.
func TestTodayShare(t *testing.T) {
	x := testBridge(t)
	def := routes.Group(routes.Route{ID: routes.Default})
	for _, r := range x.routes() {
		if r.ID == routes.Default {
			def = routes.Group(r)
		}
	}
	x.todayShare(map[string]int64{def: 100, "DIRECT": 100})
	got := x.todayShare(map[string]int64{def: 300, "DIRECT": 100})
	if got[routes.Default] != .75 || got["direct"] != .25 {
		t.Fatalf("share = %v", got)
	}
}

// Nodes carry the Provider id the 范围 chips filter on.
func TestPickDataHasProviderID(t *testing.T) {
	x := testBridge(t)
	id := x.store.List()[0].ID
	n := x.routing().Nodes
	if len(n) == 0 || n[0].ProviderID != id {
		t.Fatalf("nodes = %+v, want provider %s", n, id)
	}
}

// A strategy is a selector's position: it needs no reload, a range does.
func TestCoreSame(t *testing.T) {
	x := testBridge(t)
	old := x.set.Get()
	next := old.Clone()
	next.Routing.PickNode(routes.Default, "东京 01")
	if !coreSame(old, next) {
		t.Fatal("picking a node should not reload")
	}
	next = old.Clone()
	next.Routing.ChangePolicy(routes.Default, func(q *routes.Policy) { q.Regions = []string{"JP"} })
	if coreSame(old, next) {
		t.Fatal("changing the range must reload")
	}
}

func TestSetSettingsAndReset(t *testing.T) {
	x := testBridge(t)
	ctx := context.Background()
	if err := x.setSettings(ctx, json.RawMessage(`{"logLevel":"debug","tcpConcurrent":false}`)); err != nil {
		t.Fatal(err)
	}
	s := x.set.Get()
	if s.LogLevel != "debug" {
		t.Fatalf("level = %q", s.LogLevel)
	}
	if err := x.setSettings(ctx, json.RawMessage(`{"logLevel":"loud"}`)); err == nil {
		t.Fatal("a level that doesn't exist was accepted")
	}
	if err := x.resetCore(ctx); err != nil {
		t.Fatal(err)
	}
	if s := x.set.Get(); s.LogLevel == "debug" {
		t.Fatal("reset left the level")
	}
}

func TestDisabledProviderLeavesView(t *testing.T) {
	x := testBridge(t)
	ctx := context.Background()
	id := x.store.List()[0].ID
	if n := len(x.view().Nodes); n != 2 {
		t.Fatalf("nodes = %d", n)
	}
	if !strings.Contains(x.view().Nodes[0].Name, id) {
		t.Fatalf("node name %q lacks the provider", x.view().Nodes[0].Name)
	}
	if err := x.setProviderEnabled(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if v := x.view(); len(v.Nodes) != 0 || v.Providers[0].Enabled {
		t.Fatalf("disabled provider still listed: %+v", v.Providers)
	}
}

func TestOldRulesMoveIn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, oldRulesFile), []byte(`[{"route":"default","kind":"suffix","value":"old.example.com"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	set, err := openSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r := set.Get().Rules; len(r) != 1 || r[0].Value != "old.example.com" {
		t.Fatalf("rules = %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, oldRulesFile)); !os.IsNotExist(err) {
		t.Fatal("the old file is still there")
	}
}

// A reload that drops the descriptor makes the core close the TUN it can't reopen.
func TestReloadKeepsTunDescriptor(t *testing.T) {
	x := testBridge(t)
	if fd, ok := config(t, x)["tun"].(map[string]any)["file-descriptor"]; ok {
		t.Fatalf("no core has started, but the config names descriptor %v", fd)
	}
	if err := os.WriteFile(filepath.Join(x.dir, tunFDFile), []byte("42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fd := config(t, x)["tun"].(map[string]any)["file-descriptor"]; fmt.Sprint(fd) != "42" {
		t.Fatalf("descriptor = %v, want 42", fd)
	}
}
