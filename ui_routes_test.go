package main

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

func routesFixture() *UI {
	u := fixtureUI()
	u.snap.Settings = settings.Defaults.Clone()
	u.page = "routes"
	return u
}

func typeInto(t *testing.T, tt *ui.Tester, label, text string) {
	t.Helper()
	f, ok := tt.AccessByRole(ui.RoleTextField, label)
	if !ok {
		t.Fatalf("no field %q: %q", label, tt.Texts())
	}
	tt.ClickAt(f.Bounds.X+f.Bounds.W/2, f.Bounds.Y+f.Bounds.H/2)
	tt.Frame()
	tt.Type(text)
	tt.Frame()
}

// TestRoutesTab: the tab lists the groups, shows one's services, and opens
// a service's rules.
func TestRoutesTab(t *testing.T) {
	for _, dark := range []bool{false, true} {
		u := routesFixture()
		tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{Dark: dark})
		tt.Advance(time.Second)
		if err := tt.Click("AI 服务"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(time.Second)
		for _, want := range []string{"测试一个地址", "节点策略", "Claude", "OpenAI", "＋ 新建路由组", "＋ 新建服务", "删除路由组"} {
			if !hasTextPart(tt, want) {
				t.Errorf("tab lacks %q: %q", want, tt.Texts())
			}
		}
		shot(t, tt, map[bool]string{false: "routes-group", true: "routes-group-dark"}[dark])
		if err := tt.Click("Claude"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(time.Second)
		if !hasTextPart(tt, "claude.ai") {
			t.Errorf("open service: %q", tt.Texts())
		}
		shot(t, tt, map[bool]string{false: "routes-service", true: "routes-service-dark"}[dark])
	}
}

// TestRoutesRejectGroup: the group that refuses says so, takes services and
// rules, and has no node strategy and no way to delete it.
func TestRoutesRejectGroup(t *testing.T) {
	u := routesFixture()
	tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(time.Second)
	if err := tt.Click("拦截"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	for _, want := range []string{"已拦截", "命中这里规则的连接会被拒绝", "＋ 新建服务"} {
		if !hasTextPart(tt, want) {
			t.Errorf("group lacks %q: %q", want, tt.Texts())
		}
	}
	for _, not := range []string{"删除路由组", "今日"} {
		if hasTextPart(tt, not) {
			t.Errorf("group has %q: %q", not, tt.Texts())
		}
	}
	shot(t, tt, "routes-reject")
}

// TestRoutesAddRule: a typed rule says what it will do, joins the open
// service, and one that's in another service asks first.
func TestRoutesAddRule(t *testing.T) {
	u := routesFixture()
	u.selectGroup("ai")
	u.routes.svc = "claude"
	tt := ui.NewTester(u.view, 1060, 860) // room for the exit choice above the services
	tt.Advance(time.Second)

	typeInto(t, tt, "新规则", "https://chat.example.com/x")
	if !hasTextPart(tt, "添加后") {
		t.Errorf("no preview: %q", tt.Texts())
	}
	shot(t, tt, "routes-typing")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Frame()
	got := u.shownSettings().Rules
	want := routes.UserRule{Kind: routes.KindSuffix, Value: "chat.example.com", Route: "ai", Service: "claude"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("rules after Enter: %+v", got)
	}

	tt.Type("netflix.com")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Advance(100 * time.Millisecond)
	if !hasTextPart(tt, "一条规则只能属于一个服务") {
		t.Fatalf("no conflict: %q", tt.Texts())
	}
	shot(t, tt, "routes-conflict")
	if err := tt.Click("移过来"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(100 * time.Millisecond)
	s := u.shownSettings()
	sv, ok := routes.FindRule(routes.Build(s.Routing, s.Rules), "DOMAIN-SUFFIX,netflix.com")
	if !ok || sv.ID != "claude" {
		t.Errorf("after moving: %+v %v", sv, ok)
	}
}

// TestRoutesNewAndDelete: a new group opens ready to be named, and deleting
// one asks, then hands its services over.
func TestRoutesNewAndDelete(t *testing.T) {
	u := routesFixture()
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if err := tt.Click("＋ 新建路由组"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(100 * time.Millisecond)
	if u.routes.group != "g1" {
		t.Fatalf("selected %q", u.routes.group)
	}
	shot(t, tt, "routes-new-group")
	if err := tt.Click("删除路由组"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(100 * time.Millisecond)
	if !tt.HasText("服务交给") {
		t.Errorf("delete doesn't ask: %q", tt.Texts())
	}
	if err := tt.Click("确定删除"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(100 * time.Millisecond)
	if s := u.shownSettings(); slices.ContainsFunc(s.Routing.Groups, func(g routes.GroupConf) bool { return g.ID == "g1" }) {
		t.Errorf("still there: %+v", s.Routing.Groups)
	}
	if u.routes.group != routes.Default {
		t.Errorf("left on %q", u.routes.group)
	}
}

// TestRoutesTester: an address shows group, service and rule.
func TestRoutesTester(t *testing.T) {
	u := routesFixture()
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	typeInto(t, tt, "测试地址", "api.anthropic.com")
	for _, want := range []string{"Claude", "DOMAIN-SUFFIX,anthropic.com", "查看"} {
		if !hasTextPart(tt, want) {
			t.Errorf("lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "routes-tester")
}

// TestStrategyPicker: each strategy shows its own controls, in the card and
// in 代理's expansion.
func TestStrategyPicker(t *testing.T) {
	u := routesFixture()
	u.selectGroup("ai")
	u.routes.pol = true
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if !hasTextPart(tt, "地区") || !hasTextPart(tt, "来源") || !hasTextPart(tt, "定时测速") {
		t.Errorf("auto: %q", tt.Texts())
	}
	shot(t, tt, "routes-auto")
	if err := tt.Click("顺序备用"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(100 * time.Millisecond)
	if !hasTextPart(tt, "备用顺序 · 1") || !hasTextPart(tt, "点击加入") {
		t.Errorf("order: %q", tt.Texts())
	}
	if err := tt.Click(u.snap.Nodes[0].Display); err != nil {
		t.Fatal(err)
	}
	tt.Advance(100 * time.Millisecond)
	if q := u.policy("ai"); q.Mode != routes.ModeOrder || len(q.Order) != 2 {
		t.Errorf("after adding: %+v", q)
	}
	shot(t, tt, "routes-order")
	if err := tt.Click("固定节点"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(100 * time.Millisecond)
	shot(t, tt, "routes-fixed")
	if q := u.policy("ai"); q.Mode != routes.ModeFixed || len(q.Order) != 2 {
		t.Errorf("after fixed: %+v", q)
	}
}

// scopeFixture is a group on 自动最快, among more regions than fit a line.
func scopeFixture() (*UI, []string) {
	u := routesFixture()
	u.selectGroup("ai")
	u.routes.pol = true
	proto := u.snap.Nodes[0]
	var regions []string
	for _, cc := range []string{"AE", "AR", "AU", "BR", "CA", "CH", "DE", "ES", "FR", "GB", "ID", "IN", "IT", "KR", "MO", "MX", "MY", "NL", "PH", "RU", "SE", "TH", "TR", "TW", "UA", "VN", "ZA"} {
		n := proto
		pl := *proto.Where.Place
		pl.Country = cc
		n.Name, n.Display, n.Where.Place = cc+"-1", cc+"-1", &pl
		u.snap.Nodes = append(u.snap.Nodes, n)
		regions = append(regions, cc)
	}
	return u, regions
}

// TestScopeChipsTakeEveryClick: a chip of 自动最快's range chooses and
// un-chooses from its top to its bottom edge, at any window width: nothing
// over it takes the press, and choosing moves no chip.
func TestScopeChipsTakeEveryClick(t *testing.T) {
	for _, width := range []int{1060, 760} {
		u, regions := scopeFixture()
		u.home.regionsMore = true // every region out, as the most crowded row is
		tt := ui.NewTester(u.view, width, 760)
		tt.Advance(time.Second)

		bounds := func(label string) ui.Rect {
			t.Helper()
			a, ok := tt.AccessByRole(ui.RoleButton, label)
			if !ok {
				t.Fatalf("width %d: no chip %q: %q", width, label, tt.Texts())
			}
			return a.Bounds
		}
		var providers, provLabels, regLabels []string
		for _, pv := range u.snap.Providers {
			if !pv.Enabled {
				continue
			}
			n := 0
			for _, v := range u.snap.Nodes {
				if v.Provider == pv.ID {
					n++
				}
			}
			providers = append(providers, pv.ID)
			provLabels = append(provLabels, fmt.Sprintf("%s %d", pv.Name, n))
		}
		for _, r := range regions {
			regLabels = append(regLabels, r+" 1")
		}

		lines := map[float32]bool{}
		var regionsEnd float32
		for _, l := range regLabels {
			b := bounds(l)
			if b.X < 0 || b.X+b.W > float32(width) {
				t.Errorf("width %d: %q cut off: %+v", width, l, b)
			}
			lines[b.Y] = true
			regionsEnd = max(regionsEnd, b.Y+b.H)
		}
		if len(lines) < 2 {
			t.Errorf("width %d: regions did not wrap, lines at %v", width, lines)
		}
		for _, l := range provLabels {
			if b := bounds(l); b.Y < regionsEnd {
				t.Errorf("width %d: %q at %v overlaps the regions, which end at %v", width, l, b.Y, regionsEnd)
			}
		}

		// click presses a chip at dy down it, the pointer having come to
		// rest over it first, as a person's does.
		click := func(b ui.Rect, dy float32) {
			x, y := b.X+b.W/2, b.Y+dy
			tt.Move(x, y)
			tt.Advance(50 * time.Millisecond)
			tt.Press(x, y)
			tt.Release(x, y)
			tt.Advance(50 * time.Millisecond)
		}
		// Clicks on and off at each height, the watched chips kept in place.
		exercise := func(label string, chosen func() bool, watch []string) {
			t.Helper()
			was := map[string]ui.Rect{}
			for _, l := range watch {
				was[l] = bounds(l)
			}
			b := bounds(label)
			for _, dy := range []float32{1, b.H / 2, b.H - 1} {
				for _, want := range []bool{true, false} {
					click(b, dy)
					if chosen() != want {
						t.Errorf("width %d: click %q at %v down: chosen %v, want %v", width, label, dy, !want, want)
					}
					for _, l := range watch {
						if now := bounds(l); now != was[l] {
							t.Errorf("width %d: click %q moved %q: %+v to %+v", width, label, l, was[l], now)
						}
					}
				}
			}
		}
		// A chosen region narrows the other row's counts and a chosen source
		// this row's, so only the row clicked keeps its labels throughout.
		for i, l := range regLabels {
			exercise(l, func() bool { return slices.Contains(u.policy("ai").Regions, regions[i]) }, regLabels)
		}
		for i, l := range provLabels {
			exercise(l, func() bool { return slices.Contains(u.policy("ai").Providers, providers[i]) }, provLabels)
		}
		shot(t, tt, fmt.Sprintf("routes-auto-many-%d", width))
	}
}

// TestRoutesFixAction: a broken group's notice opens its strategy, in
// place on this tab and in 代理's expansion elsewhere.
func TestRoutesFixAction(t *testing.T) {
	u := routesFixture()
	u.issueAction("fix:ai")
	if u.routes.group != "ai" || !u.routes.pol {
		t.Errorf("on the tab: %+v", u.routes.group)
	}
	u.page = "home"
	u.issueAction("fix:media")
	if u.home.x != "routes" || u.home.sub != "media" {
		t.Errorf("elsewhere: %q %q", u.home.x, u.home.sub)
	}
	u.issueAction("fix:default")
	if u.home.x != "hero" {
		t.Errorf("default: %q", u.home.x)
	}
}

func ruleSetFixture() (*UI, string) {
	u := routesFixture()
	var id string
	u.snap.Settings.Routing = routes.Plan{}
	id = u.snap.Settings.Routing.AddRuleSet(nil, "ai", "global", "https://raw.example/rules/global.yaml")
	u.snap.RuleSets = map[string]RuleSetView{id: {Count: 349, Skipped: 2, UpdatedAt: time.Now().Add(-2 * time.Hour),
		Preview: []routes.Rule{{Text: "DOMAIN-SUFFIX,anthropic.com", Kind: routes.KindSuffix, Value: "anthropic.com"}}}}
	return u, id
}

// TestRuleSetRow: a rule set reads as a service, says how it stands, and
// has none of the controls for rules one writes by hand.
func TestRuleSetRow(t *testing.T) {
	u, id := ruleSetFixture()
	u.selectGroup("ai")
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	for _, want := range []string{"global", "349 条规则", "2 小时前更新", "raw.example", "规则集"} {
		if !hasTextPart(tt, want) {
			t.Errorf("row lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "ruleset-row")

	if err := tt.Click("global"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(100 * time.Millisecond)
	for _, want := range []string{"https://raw.example/rules/global.yaml", "349 条规则，跳过 2 条不支持的", "每天自动更新", "*.anthropic.com", "还有 348 条", "更新", "停用", "删除", "移到…"} {
		if !hasTextPart(tt, want) {
			t.Errorf("editor lacks %q: %q", want, tt.Texts())
		}
	}
	if _, ok := tt.AccessByRole(ui.RoleTextField, "新规则"); ok {
		t.Error("a list someone else keeps has an input for new rules")
	}
	shot(t, tt, "ruleset-open")

	v := u.snap.RuleSets[id]
	v.Error = "HTTP 404"
	u.snap.RuleSets[id] = v
	tt = ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if !hasTextPart(tt, "更新失败，继续使用上次的 349 条规则：HTTP 404") {
		t.Errorf("no failure: %q", tt.Texts())
	}
	delete(u.snap.RuleSets, id)
	tt = ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if !hasTextPart(tt, "本地副本不见了") {
		t.Errorf("no word on the lost file: %q", tt.Texts())
	}
}

// TestRuleSetImportPanel: the panel opens from the group, knows a source it
// already has, and goes away.
func TestRuleSetImportPanel(t *testing.T) {
	u, _ := ruleSetFixture()
	u.selectGroup("ai")
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if err := tt.Click("＋ 导入规则集"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(100 * time.Millisecond)
	if !hasTextPart(tt, "粘贴规则集链接") {
		t.Fatalf("no panel: %q", tt.Texts())
	}
	shot(t, tt, "ruleset-import")
	typeInto(t, tt, "规则集来源", "https://raw.example/rules/global.yaml")
	if !hasTextPart(tt, "已经导入到") {
		t.Errorf("no word on the duplicate: %q", tt.Texts())
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(100 * time.Millisecond)
	if hasTextPart(tt, "已经导入到") || u.routes.rs.open {
		t.Error("the panel stayed")
	}
}

// TestDefaultExitChoice: 其他流量 comes last, chooses its exit before its
// node strategy, and has no strategy while it goes direct.
func TestDefaultExitChoice(t *testing.T) {
	for _, direct := range []bool{false, true} {
		u := routesFixture()
		for i := range u.snap.Routes {
			if u.snap.Routes[i].ID == routes.Default {
				u.snap.Routes[i].Direct = direct
			}
		}
		u.snap.Settings.Routing.ChangePolicy(routes.Default, func(q *routes.Policy) { q.Direct = direct })
		tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
		tt.Advance(time.Second)
		if err := tt.Click("其他流量"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(time.Second)
		for _, want := range []string{"走节点", "直接连接", "不经过节点，用本机网络", "以上规则都没命中"} {
			if !hasTextPart(tt, want) {
				t.Errorf("direct=%v lacks %q: %q", direct, want, tt.Texts())
			}
		}
		if got := hasTextPart(tt, "节点策略"); got == direct {
			t.Errorf("direct=%v: node strategy shown is %v", direct, got)
		}
		if got := hasTextPart(tt, "当前网络打不开的网站"); got != direct {
			t.Errorf("direct=%v: warning shown is %v", direct, got)
		}
		shot(t, tt, map[bool]string{false: "routes-other", true: "routes-other-direct"}[direct])

		u = fixtureUI()
		for i := range u.snap.Routes {
			if u.snap.Routes[i].ID == routes.Default {
				u.snap.Routes[i].Direct = direct
			}
		}
		u.home.x = "routes"
		tt = ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
		tt.Advance(2 * time.Second)
		if !hasTextPart(tt, "以上规则都没命中") || !hasTextPart(tt, "其他流量") {
			t.Errorf("home list: %q", tt.Texts())
		}
		shot(t, tt, map[bool]string{false: "home-routes", true: "home-routes-direct"}[direct])
	}
}

// TestGroupExitChoice: a route group other than 直连 and 拦截 chooses its
// exit as 其他流量 does. Going direct hides its node strategy and keeps it;
// it stays a group that can be renamed and deleted.
func TestGroupExitChoice(t *testing.T) {
	u := routesFixture()
	u.snap.Settings.Routing.SetStrategy("ai", routes.ModeFixed, "hk")
	tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(time.Second)
	if err := tt.Click("AI 服务"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	for _, want := range []string{"走节点", "不经过节点，用本机网络", "节点策略"} {
		if !hasTextPart(tt, want) {
			t.Errorf("on a node, lacks %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("不经过节点，用本机网络"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if q := u.shownSettings().Routing.Policy("ai"); !q.Direct || q.Mode != routes.ModeFixed || q.Fixed != "hk" {
		t.Errorf("the policy after going direct: %+v", q)
	}
	if slices.Contains(tt.Texts(), "节点策略") || !hasTextPart(tt, "节点策略会保留") || hasTextPart(tt, "当前网络打不开的网站") {
		t.Errorf("direct: %q", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleTextField, "路由组名字"); !ok {
		t.Error("a group that goes direct can't be renamed")
	}
	if !hasTextPart(tt, "删除路由组") {
		t.Errorf("a group that goes direct can't be deleted: %q", tt.Texts())
	}
	shot(t, tt, "routes-group-direct")
	if err := tt.Click("走节点"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if q := u.shownSettings().Routing.Policy("ai"); q.Direct || q.Fixed != "hk" {
		t.Errorf("the policy back on a node: %+v", q)
	}
	for _, name := range []string{"直连", "拦截"} {
		if err := tt.Click(name); err != nil {
			t.Fatal(err)
		}
		tt.Advance(time.Second)
		if hasTextPart(tt, "走节点") || hasTextPart(tt, "不经过节点，用本机网络") {
			t.Errorf("%s chooses an exit: %q", name, tt.Texts())
		}
	}
}

// dragOnto drags the row showing from onto the row showing to.
func dragOnto(t *testing.T, tt *ui.Tester, from, to string) {
	t.Helper()
	a, ok := tt.Find(from)
	if !ok {
		t.Fatalf("no %q: %q", from, tt.Texts())
	}
	b, ok := tt.Find(to)
	if !ok {
		t.Fatalf("no %q: %q", to, tt.Texts())
	}
	ax, ay := a.X+a.W/2, a.Y+a.H/2
	bx, by := b.X+b.W/2, b.Y+b.H/2
	tt.Press(ax, ay)
	tt.Frame()
	for i := 1; i <= 4; i++ {
		tt.Move(ax+(bx-ax)*float32(i)/4, ay+(by-ay)*float32(i)/4)
		tt.Frame()
	}
	tt.Release(bx, by)
	tt.Advance(200 * time.Millisecond)
}

// TestDragToReorder: a route group dragged onto another takes its place,
// and so does a rule set within its group; the fixed groups neither move
// nor take one. The list says what the order means.
func TestDragToReorder(t *testing.T) {
	u := routesFixture()
	plan := &u.snap.Settings.Routing
	g := plan.AddGroup(nil, "OpenAI")
	first := plan.AddRuleSet(nil, "ai", "宽的规则集", "https://x.example/a.yaml")
	second := plan.AddRuleSet(nil, "ai", "窄的规则集", "https://x.example/b.yaml")
	tt := ui.NewTesterWith(u.view, 1060, 860, ui.TesterOptions{})
	tt.Advance(time.Second)
	if !hasTextPart(tt, "规则集从上到下匹配") {
		t.Errorf("no word on what the order means: %q", tt.Texts())
	}
	order := func() []string {
		var out []string
		for _, c := range u.shownSettings().Routing.Groups {
			out = append(out, c.ID)
		}
		return out
	}
	dragOnto(t, tt, "OpenAI", "AI 服务")
	if got, want := order(), []string{routes.Default, g, "ai", "media", "dev", "direct", routes.Reject}; !slices.Equal(got, want) {
		t.Errorf("after dragging up: %v, want %v", got, want)
	}
	shot(t, tt, "routes-reordered")
	dragOnto(t, tt, "OpenAI", "开发")
	if got, want := order(), []string{routes.Default, "ai", "media", "dev", g, "direct", routes.Reject}; !slices.Equal(got, want) {
		t.Errorf("after dragging down: %v, want %v", got, want)
	}
	before := order()
	dragOnto(t, tt, "OpenAI", "直连")
	dragOnto(t, tt, "拦截", "流媒体")
	dragOnto(t, tt, "其他流量", "流媒体")
	if got := order(); !slices.Equal(got, before) {
		t.Errorf("a fixed group moved or gave up its place: %v", got)
	}

	if err := tt.Click("AI 服务"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	sets := func() []string {
		var out []string
		for _, l := range routes.Lines(routes.Build(u.shownSettings().Routing, nil), nil) {
			if l.Set != "" {
				out = append(out, l.Set)
			}
		}
		return out
	}
	if got := sets(); !slices.Equal(got, []string{first, second}) {
		t.Fatalf("sets before: %v", got)
	}
	dragOnto(t, tt, "窄的规则集", "宽的规则集")
	if got := sets(); !slices.Equal(got, []string{second, first}) {
		t.Errorf("sets after dragging the second onto the first: %v", got)
	}
	a, _ := tt.Find("窄的规则集")
	b, _ := tt.Find("宽的规则集")
	if a.Y >= b.Y {
		t.Errorf("the list still shows the old order: %v %v", a, b)
	}
	dragOnto(t, tt, "窄的规则集", "Claude") // a service and a set are not one another's place
	if got := sets(); !slices.Equal(got, []string{second, first}) {
		t.Errorf("sets after a drag onto a service: %v", got)
	}
}
