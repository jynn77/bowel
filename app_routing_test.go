package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/gateway"
	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
	"github.com/oboo/moho/internal/sysproxy"
)

// routingApp is an app on a real core with three nodes, on a port of its
// own and with the system proxy out of reach.
func routingApp(t *testing.T, prepare func(*settings.Settings)) (*App, map[string]NodeView) {
	t.Helper()
	if testing.Short() {
		t.Skip("starts the core")
	}
	saved := osProxy
	osProxy.enable = func(string, int, []string) error { t.Error("the test tried to enable the system proxy"); return nil }
	osProxy.disable = func() error { t.Error("the test tried to disable the system proxy"); return nil }
	osProxy.snapshot = func() (sysproxy.Saved, error) { return sysproxy.Saved{}, nil }
	osProxy.restore = func(context.Context, sysproxy.Saved) error {
		t.Error("the test tried to restore the system proxy")
		return nil
	}
	t.Cleanup(func() { osProxy = saved })
	savedGw := osGateway
	osGateway = func() gateway.System { return &gwFake{t: t} }
	t.Cleanup(func() { osGateway = savedGw })
	dir, err := os.MkdirTemp("", "mh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	if !exists(bundledCore()) {
		t.Skip("no core; run scripts/fetch-core.sh")
	}
	sub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "proxies:\n  - {name: \"🇭🇰 香港 01\", type: direct}\n  - {name: \"JP-Tokyo-02\", type: direct}\n  - {name: \"🇺🇸 US (IEPL) 03\", type: direct}\n")
	}))
	t.Cleanup(sub.Close)
	a, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	a.http = sub.Client()
	port, err := core.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.settings.Update(func(s *settings.Settings) {
		s.MixedPort = port
		if prepare != nil {
			prepare(s)
		}
	}); err != nil {
		t.Fatal(err)
	}
	a.copyGeo()
	if err := a.applyConfig(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(a.shutdown)
	if _, err := a.addProvider(context.Background(), sub.URL+"/s"); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	nodes := map[string]NodeView{}
	for _, n := range a.snapshot().Nodes {
		if n.Where.Place != nil {
			nodes[n.Where.Place.Country] = n
		}
	}
	if len(nodes) != 3 {
		t.Fatalf("nodes: %+v", a.snapshot().Nodes)
	}
	return a, nodes
}

func coreProxies(t *testing.T, a *App) map[string]mihomo.Proxy {
	t.Helper()
	var px map[string]mihomo.Proxy
	if err := a.coreCall(5*time.Second, func(ctx context.Context, c *mihomo.Client) (err error) {
		px, err = c.Proxies(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return px
}

func routeOf(t *testing.T, a *App, id string) routes.Route {
	t.Helper()
	for _, r := range a.routes() {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no route %q", id)
	return routes.Route{}
}

func changeRouting(t *testing.T, a *App, fn func(*routes.Plan)) {
	t.Helper()
	if err := a.updateSettings(func(s *settings.Settings) { fn(&s.Routing) }); err != nil {
		t.Fatal(err)
	}
	a.refresh()
}

// TestStrategiesOnCore: each strategy on a real core, and what the page
// says about it.
func TestStrategiesOnCore(t *testing.T) {
	a, n := routingApp(t, nil)
	hk, jp, us := n["HK"].Name, n["JP"].Name, n["US"].Name
	ai := routeOf(t, a, "ai")
	pid := a.core.Status().PID

	// A fresh app is 自动最快 over everything.
	r := routeIn(a.snapshot(), "ai")
	if r.Policy.Mode != routes.ModeAuto || !r.Auto || r.Broken != "" {
		t.Fatalf("fresh: %+v", r)
	}

	// A range narrows what 自动 may pick.
	changeRouting(t, a, func(p *routes.Plan) { p.ChangePolicy("ai", func(q *routes.Policy) { q.Regions = []string{"HK"} }) })
	if got := coreProxies(t, a)[routes.Auto(ai)].All; !slices.Equal(got, []string{hk}) {
		t.Errorf("range = %q", got)
	}
	if r = routeIn(a.snapshot(), "ai"); r.Node != hk || r.Broken != "" {
		t.Errorf("in range: %+v", r)
	}

	// A range with nothing in it is red, and doesn't widen.
	changeRouting(t, a, func(p *routes.Plan) { p.ChangePolicy("ai", func(q *routes.Policy) { q.Regions = []string{"ZZ"} }) })
	if got := coreProxies(t, a)[routes.Auto(ai)].All; !slices.Equal(got, []string{"REJECT"}) {
		t.Errorf("empty range = %q", got)
	}
	if r = routeIn(a.snapshot(), "ai"); r.Broken == "" || r.Node != "" {
		t.Errorf("empty range: %+v", r)
	}
	if is := a.brokenIssue(r, a.snapshot().Nodes, 0); is == nil || is.Action != "fix:ai" {
		t.Errorf("issue for it: %+v", is)
	}

	// 顺序备用 is a position of the selector; the list itself is config.
	changeRouting(t, a, func(p *routes.Plan) {
		p.ChangePolicy("ai", func(q *routes.Policy) { q.Regions, q.Order = nil, []string{jp, us} })
		p.SetStrategy("ai", routes.ModeOrder, "")
	})
	px := coreProxies(t, a)
	if px[routes.Group(ai)].Now != routes.Backup(ai) || px[routes.BackupStep(ai, 1)].All[0] != us {
		t.Errorf("order: selector %+v, step %+v", px[routes.Group(ai)], px[routes.BackupStep(ai, 1)])
	}
	if r = routeIn(a.snapshot(), "ai"); r.Node != jp {
		t.Errorf("order: node %q, want %q (%+v)", r.Node, jp, r)
	}

	// 固定节点, and the choice of strategy reloads nothing.
	changeRouting(t, a, func(p *routes.Plan) { p.SetStrategy("ai", routes.ModeFixed, us) })
	if now := coreProxies(t, a)[routes.Group(ai)].Now; now != us {
		t.Errorf("fixed: selector on %q", now)
	}
	if r = routeIn(a.snapshot(), "ai"); r.Node != us || r.Auto {
		t.Errorf("fixed: %+v", r)
	}

	// Picking a node under 自动最快 fixes on it, and that lasts.
	changeRouting(t, a, func(p *routes.Plan) { p.SetStrategy("ai", routes.ModeAuto, "") })
	if err := a.selectNode("ai", jp); err != nil {
		t.Fatal(err)
	}
	if q := a.settings.Get().Routing.Policy("ai"); q.Mode != routes.ModeFixed || q.Fixed != jp || len(q.Order) != 2 {
		t.Errorf("picked: %+v", q)
	}
	if a.core.Status().PID != pid {
		t.Errorf("the core restarted: %d → %d", pid, a.core.Status().PID)
	}
}

// TestRoutingStructureOnCore: a custom group gets its own selector, its
// rules reach the core's config, and deleting it hands them on.
func TestRoutingStructureOnCore(t *testing.T) {
	a, _ := routingApp(t, nil)
	var gid, sid string
	if err := a.updateSettings(func(s *settings.Settings) {
		probe := s.Routing.Clone()
		gid = probe.AddGroup(s.Rules, "工作")
		sid = probe.AddService(s.Rules, gid, "内网")
		s.Routing = probe
		s.Rules = append(s.Rules, routes.UserRule{Kind: routes.KindSuffix, Value: "corp.example", Route: gid, Service: sid})
	}); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	r := routeIn(a.snapshot(), gid)
	if r.Name != "工作" || r.Node == "" {
		t.Fatalf("custom group: %+v", r)
	}
	if _, ok := coreProxies(t, a)[gid]; !ok {
		t.Errorf("no selector named %q", gid)
	}
	if cfg := coreConfig(t, a); !strings.Contains(cfg, "DOMAIN-SUFFIX,corp.example,"+gid) {
		t.Errorf("rule not at the core:\n%s", cfg)
	}
	if err := a.updateSettings(func(s *settings.Settings) { s.Routing.DeleteGroup(s.Rules, gid, "media") }); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	if _, ok := coreProxies(t, a)[gid]; ok {
		t.Error("the deleted group's selector stayed")
	}
	if cfg := coreConfig(t, a); !strings.Contains(cfg, "DOMAIN-SUFFIX,corp.example,"+routes.Group(routeOf(t, a, "media"))) {
		t.Errorf("rule not handed on:\n%s", cfg)
	}
}

func coreConfig(t *testing.T, a *App) string {
	t.Helper()
	b, err := os.ReadFile(a.core.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestAdoptSelections: settings from before strategies existed follow where
// the selector stands: a node means 固定节点, 自动 means 自动最快. The rules
// made before services stay in the group's 我添加的.
func TestAdoptSelections(t *testing.T) {
	old := routes.UserRule{Kind: routes.KindSuffix, Value: "old.example", Route: "ai"}
	a, n := routingApp(t, func(s *settings.Settings) { s.Rules = []routes.UserRule{old} })
	all := a.routes()
	sv, ok := routes.FindRule(all, old.Mihomo())
	if !ok || !sv.Mine || sv.Group != "ai" {
		t.Errorf("old rule landed in %+v", sv)
	}
	ai := routeOf(t, a, "ai")
	if _, err := a.settings.Update(func(s *settings.Settings) { s.Routing = routes.Plan{} }); err != nil {
		t.Fatal(err)
	}
	if err := a.coreCall(5*time.Second, func(ctx context.Context, c *mihomo.Client) error {
		return c.Select(ctx, routes.Group(ai), n["JP"].Name)
	}); err != nil {
		t.Fatal(err)
	}
	a.adoptSelections()
	got := a.settings.Get().Routing
	if q := got.Policy("ai"); q.Mode != routes.ModeFixed || q.Fixed != n["JP"].Name {
		t.Errorf("ai adopted %+v", q)
	}
	if q := got.Policy(routes.Default); q.Mode != routes.ModeAuto {
		t.Errorf("default adopted %+v", q)
	}
}

// TestDefaultDirectInTheApp: going direct changes the core's config, what
// no rule matched counts as the default route, and it shows no exit node;
// in global mode the default route goes by its nodes again.
func TestDefaultDirectInTheApp(t *testing.T) {
	var off, on routes.Plan
	on.ChangePolicy(routes.Default, func(q *routes.Policy) { q.Direct = true })
	off.ChangePolicy(routes.Default, func(*routes.Policy) {})
	if routingCoreSame(off, on) {
		t.Error("going direct would not reach the core")
	}
	a, _ := routingApp(t, nil)
	changeRouting(t, a, func(p *routes.Plan) {
		p.ChangePolicy(routes.Default, func(q *routes.Policy) { q.Direct = true })
	})
	if cfg := coreConfig(t, a); !strings.Contains(cfg, "- MATCH,DIRECT") {
		t.Errorf("the core's rules:\n%s", cfg)
	}
	if got := a.routeOfGroup("DIRECT", "Match"); got != routes.Default {
		t.Errorf("unmatched traffic is on route %q", got)
	}
	if got := a.routeOfGroup("DIRECT", "GeoIP"); got != "direct" {
		t.Errorf("traffic a rule sent direct is on route %q", got)
	}
	snap := a.snapshot()
	if last := snap.Routes[len(snap.Routes)-1]; last.ID != routes.Default || !last.Direct || last.Node != "" {
		t.Errorf("last route: %+v", last)
	}
	if snap.Exit != nil {
		t.Errorf("an exit while going direct: %+v", snap.Exit)
	}
	if err := a.setMode("global"); err != nil {
		t.Fatal(err)
	}
	if r := routeIn(a.snapshot(), routes.Default); r.Direct || r.Node == "" {
		t.Errorf("global mode: %+v", r)
	}
	if now := coreProxies(t, a)["GLOBAL"].Now; now != "默认" {
		t.Errorf("global mode is on %q", now)
	}
}

// TestGroupDirectInTheApp: a route group that goes direct reaches the
// core, shows no node, and still counts its own traffic, by its group's
// name; what other rules send direct stays 直连's. Back on a node, it is on
// the one it was fixed to.
func TestGroupDirectInTheApp(t *testing.T) {
	var off, on routes.Plan
	on.ChangePolicy("ai", func(q *routes.Policy) { q.Direct = true })
	off.ChangePolicy("ai", func(*routes.Policy) {})
	if routingCoreSame(off, on) {
		t.Error("going direct would not reach the core")
	}
	a, n := routingApp(t, nil)
	changeRouting(t, a, func(p *routes.Plan) { p.PickNode("ai", n["JP"].Name) })
	if now := coreProxies(t, a)["AI 服务"].Now; now != n["JP"].Name {
		t.Fatalf("ai is on %q", now)
	}
	changeRouting(t, a, func(p *routes.Plan) {
		p.ChangePolicy("ai", func(q *routes.Policy) { q.Direct = true })
	})
	cfg := coreConfig(t, a)
	if !strings.Contains(cfg, "- DOMAIN-SUFFIX,openai.com,AI 服务") || strings.Contains(cfg, "AI 服务·自动") {
		t.Errorf("the core's config:\n%s", cfg)
	}
	if g := coreProxies(t, a)["AI 服务"]; g.Now != "DIRECT" || len(g.All) != 1 {
		t.Errorf("the group in the core: %+v", g)
	}
	if got := a.routeOfGroup("AI 服务", "DomainSuffix"); got != "ai" {
		t.Errorf("the group's traffic is on route %q", got)
	}
	if got := a.routeOfGroup("DIRECT", "GeoIP"); got != "direct" {
		t.Errorf("traffic a rule sent direct is on route %q", got)
	}
	if r := routeIn(a.snapshot(), "ai"); !r.Direct || r.Node != "" {
		t.Errorf("ai: %+v", r)
	}
	if err := a.selectNode("ai", n["JP"].Name); err == nil {
		t.Error("a node chosen for a group that goes direct")
	}
	changeRouting(t, a, func(p *routes.Plan) {
		p.ChangePolicy("ai", func(q *routes.Policy) { q.Direct = false })
	})
	if r := routeIn(a.snapshot(), "ai"); r.Direct || r.Node != n["JP"].Name {
		t.Errorf("ai back on a node: %+v", r)
	}
	if now := coreProxies(t, a)["AI 服务"].Now; now != n["JP"].Name {
		t.Errorf("back on a node, ai is on %q", now)
	}
}
