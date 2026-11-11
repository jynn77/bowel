package main

// 路由组: where traffic goes. A strict tree: a rule belongs to one service,
// a service to one route group, and only a group has an exit. The left
// column lists the groups, the right is the chosen one: its strategy, the
// same control as 代理's in-place expansion, and its services with their
// rules. Every change applies at once.
//
// Design: docs/product-ia.md and prototype/groups.html.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

type routesUI struct {
	group string // the group shown
	svc   string // the service open, "" for none
	pol   bool   // the strategy card is open
	test  string // 测试一个地址

	// The group's name as typed, and whose it is.
	name, nameOf, nameErr string
	focusName             bool
	nameH                 ui.Handle

	// The open service's name as typed, and whose it is.
	sname, snameOf, snameErr string

	// The new rule of the open service.
	rule, kind, ruleErr string
	ruleH               ui.Handle
	focusRule           bool
	conflict            *ruleConflict

	armed string // "g:id" or "s:id": 删除 asks to be pressed again
	delTo string // who inherits a group being deleted

	rs ruleSetDraft // 导入规则集
}

// ruleConflict is a rule that is already in another service.
type ruleConflict struct {
	rule routes.UserRule
	from routes.Service
}

// routeKinds are the choices for a new rule's kind; "" guesses.
var routeKinds = []string{"", routes.KindSuffix, routes.KindDomain, routes.KindKeyword, routes.KindIP}

func routeKindLabels() []string {
	out := []string{i18n.T("自动识别", "Detect")}
	for _, k := range routeKinds[1:] {
		out = append(out, routes.KindName(k))
	}
	return out
}

// openRoutes shows the tab at a group, and in it a service if svc is given.
func (u *UI) openRoutes(group, svc string) {
	u.page = "routes"
	u.selectGroup(group)
	u.routes.svc = svc
}

// ruleHome is where in 路由组 the rule a connection matched lives.
func (u *UI) ruleHome(cn Conn) (group, svc string, ok bool) {
	if u.snap.Imported != nil {
		return "", "", false // the rule is the config's: nowhere here to show it
	}
	st := u.shownSettings()
	sv, found := routes.FindMatched(routes.Build(st.Routing, st.Rules), cn.Rule, cn.Payload)
	return sv.Group, sv.ID, found
}

func (u *UI) selectGroup(id string) {
	r := &u.routes
	r.group, r.svc, r.pol, r.armed, r.nameOf, r.nameErr = id, "", false, "", "", ""
	u.home.region, u.home.query = "", ""
	u.clearRuleDraft()
	u.closeRuleSetImport()
}

func (u *UI) clearRuleDraft() {
	r := &u.routes
	r.rule, r.ruleErr, r.conflict, r.snameOf, r.snameErr = "", "", nil, "", ""
}

// changeRouting changes route groups as setOpt changes an option, at once
// on the page and then in the app, from the main window.
func (u *UI) changeRouting(fn func(*settings.Settings), after func()) {
	next := u.shownSettings().Clone()
	fn(&next)
	u.set.over = &next
	u.set.seq++
	if u.app == nil {
		return
	}
	seq := u.set.seq
	go func() {
		err := u.app.updateSettings(fn)
		if err == nil && after != nil {
			after()
		}
		saved := u.app.settings.Get()
		u.win.Update(func() {
			u.snap.Settings = saved
			if u.set.seq == seq {
				u.set.over = nil
			}
			if err != nil {
				u.toasts = append(u.toasts, err.Error())
			}
		})
	}()
}

// setRule puts r in the rules, or takes it out, and closes the connections
// it matches that go elsewhere, so that the change shows.
func (u *UI) setRule(r routes.UserRule, removed bool) {
	keep := r.Route
	if removed {
		keep = ""
	}
	u.changeRouting(func(s *settings.Settings) {
		if removed {
			s.Rules = routes.Remove(s.Rules, r)
			return
		}
		s.Routing.Claim(s.Rules, r)
		s.Rules = routes.Upsert(s.Rules, r)
	}, func() { u.app.reroute(r, keep) })
}

// policy is route rid's policy as the page shows it.
func (u *UI) policy(rid string) routes.Policy {
	q := u.shownSettings().Routing.Policy(rid)
	if q.Mode == "" {
		q.Mode = routes.ModeAuto
	}
	return q
}

func (u *UI) setStrategy(rid, mode string) {
	if u.policy(rid).Mode == mode {
		return
	}
	cur := u.route(rid).Node
	u.home.region, u.home.query = "", ""
	u.changeRouting(func(s *settings.Settings) { s.Routing.SetStrategy(rid, mode, cur) }, nil)
}

// changePolicy edits a group's strategy settings.
func (u *UI) changePolicy(rid string, fn func(*routes.Policy)) {
	u.changeRouting(func(s *settings.Settings) { s.Routing.ChangePolicy(rid, fn) }, nil)
}

func modeName(mode string) string {
	switch mode {
	case routes.ModeOrder:
		return i18n.T("顺序备用", "Backup order")
	case routes.ModeFixed:
		return i18n.T("固定节点", "Fixed node")
	}
	return i18n.T("自动最快", "Fastest")
}

// stratSeg chooses a route group's strategy.
func (u *UI) stratSeg(c *ui.Context, p *pal, rid string) {
	cur := u.policy(rid).Mode
	ui.Row(c).Padding(2).Radius(p.r(8)).Background(p.hair2).Children(func() {
		for _, mode := range routes.Modes {
			on := cur == mode
			b := ui.ButtonBase(c.Key(mode)).Padding(3, 11).Radius(p.r(6))
			if on {
				b.Background(p.card).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, .12))
			}
			b.OnClick(func() { u.setStrategy(rid, mode) }).Children(func() {
				w := 500
				if on {
					w = 600
				}
				ui.Text(c, modeName(mode)).FontSize(12).FontWeight(w).TextColor(p.ink).NoWrap()
			})
		}
	})
}

// autoSeg is what a group's in-place expansion offers: its strategy, and
// the way to what it carries.
func (u *UI) autoSeg(c *ui.Context, p *pal, rid string) {
	if u.snap.Imported != nil {
		return // a group's strategy is its kind, and the config's
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		btn(c.Key("services"), p, i18n.T("服务和规则", "Services and rules")).OnClick(func() { u.openRoutes(rid, "") })
		if !u.route(rid).Direct {
			u.stratSeg(c, p, rid)
		}
	})
}

// setExitDirect says whether group rid's traffic goes without a node, and
// closes its connections, so that the change shows.
func (u *UI) setExitDirect(rid string, on bool) {
	u.changeRouting(func(s *settings.Settings) {
		s.Routing.ChangePolicy(rid, func(q *routes.Policy) { q.Direct = on })
	}, func() { u.app.closeRoute(rid) })
}

// exitChoice is where group rid's traffic leaves: by a node, or directly.
// It comes before the node strategy, which only the first of those has.
func (u *UI) exitChoice(c *ui.Context, p *pal, rid string) {
	q := u.policy(rid)
	opt := func(key, title, sub string, on bool, onClick func()) {
		b := ui.ButtonBase(c.Key(key)).Column().Gap(1).Grow(1).Basis(0).MinWidth(0).Padding(9, 12).Radius(p.r(8)).AlignItems(ui.Start)
		switch {
		case on:
			b.Border(1.5, p.ink)
		default:
			b.Border(1, p.hair)
			if b.Hovered() {
				b.Background(p.hover)
			}
		}
		b.OnClick(onClick).Children(func() {
			ui.Text(c, title).FontSize(13.5).FontWeight(600).TextColor(p.ink).NoWrap()
			ui.Text(c, sub).FontSize(11.5).TextColor(p.mute).SingleLine()
		})
	}
	ui.Column(c.Key("exit-choice")).Gap(8).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			opt("node", i18n.T("走节点", "Via a node"), modeName(q.Mode), !q.Direct, func() {
				if q.Direct {
					u.setExitDirect(rid, false)
				}
			})
			opt("direct", i18n.T("直接连接", "Direct"), i18n.T("不经过节点，用本机网络", "No node, this machine's network"), q.Direct, func() {
				if !q.Direct {
					u.setExitDirect(rid, true)
				}
			})
		})
		if q.Direct {
			text := i18n.T("没命中规则的网站会用本机网络访问。当前网络打不开的网站，需要加规则送到走节点的路由组。", "Sites no rule matches use this machine's network. A site that won't open on it needs a rule sending it to a route group with a node.")
			if rid != routes.Default {
				text = i18n.T("这个路由组的流量用本机网络访问，不经过节点。节点策略会保留，切回「走节点」后和原来一样。", "This group's traffic uses this machine's network, with no node. Its node strategy is kept, and is as it was once you switch back to a node.")
			}
			if u.snap.Mode == "global" {
				text += i18n.T("现在是全局模式，所有流量仍走节点，回到规则模式后生效。", " Global mode sends everything through a node, so this applies once you're back in rule mode.")
			}
			ui.Text(c, text).FontSize(12).TextColor(p.ink2).Padding(9, 12).Radius(p.r(8)).Background(p.warn.Alpha(.13))
		}
	})
}

// groupExit is the body of a route group's detail: where it leaves, and
// the nodes if a node is what it leaves by.
func (u *UI) groupExit(c *ui.Context, p *pal, rid string, w float32) {
	if u.snap.Imported == nil && routes.ChoosesExit(rid) {
		ui.Column(c).Padding(10, 26, 12, 26).Shrink(0).Children(func() { u.exitChoice(c, p, rid) })
		if u.route(rid).Direct {
			return
		}
	}
	u.nodePicker(c, p, rid, w)
}

// The route list's measures: matchLine draws through its swatches by them.
const (
	matchRowH    = 58
	matchLabelH  = 30
	matchNameH   = 22
	matchSwatchY = (matchRowH-40)/2 + matchNameH/2 // from a row's top
	matchSwatchX = 15
	matchGap     = 9 // the line stops this far from a swatch's middle
)

// matchLabel introduces the row for what the others miss.
func (u *UI) matchLabel(c *ui.Context, p *pal, other RouteView) {
	text := i18n.T("以上规则都没命中", "None of the rules above matched")
	if other.Share > 0 {
		text = fmt.Sprintf(i18n.T("以上规则都没命中 · 今日还剩 %d%% 的流量", "None of the rules above matched · %d%% of today's traffic is left"), int(other.Share*100+.5))
	}
	ui.Row(c).Height(matchLabelH).AlignItems(ui.Center).Padding(0, 10, 0, 32).Children(func() {
		ui.Text(c, text).FontSize(11).FontWeight(600).TextColor(p.mute).SingleLine()
	})
}

// matchLine runs down through the route groups' swatches into the row for
// what none of them matched: rules are tried in turn, and what isn't caught
// goes on. It thins by the share of today's traffic each group took.
func (u *UI) matchLine(c *ui.Context, p *pal, rest []RouteView, other RouteView) {
	if len(rest) == 0 {
		return
	}
	type seg struct {
		y0, y1, w float32
		from, to  ui.Color
	}
	y := func(i int) float32 { return float32(matchRowH*i + matchSwatchY) }
	weight := func(left float64) float32 { return float32(1.5 + 4*max(0, left)) }
	endY := float32(matchRowH*len(rest)+matchLabelH) + matchSwatchY
	segs := make([]seg, 0, len(rest)+1)
	first := p.routeColor(rest[0].ID)
	segs = append(segs, seg{y0: 2, y1: y(0) - matchGap, w: weight(1), from: first.Alpha(0), to: first})
	left := 1.0
	for i, r := range rest {
		left -= r.Share
		if i == len(rest)-1 {
			segs = append(segs, seg{y(i) + matchGap, endY - matchGap - 9, weight(left), p.routeColor(r.ID), p.routeColor(other.ID)})
			break
		}
		segs = append(segs, seg{y(i) + matchGap, y(i+1) - matchGap, weight(left), p.routeColor(r.ID), p.routeColor(rest[i+1].ID)})
	}
	arrow := p.routeColor(other.ID)
	ui.Box(c.Key("line")).Absolute().Left(0).Top(0).Bottom(0).Width(30).IgnoreInput(true).AccessHidden(true).Draw(func(pt *ui.Painter, r ui.Rect) {
		x := r.X + matchSwatchX
		for _, s := range segs {
			var path ui.Path
			path.MoveTo(x, r.Y+s.y0).LineTo(x, r.Y+s.y1).Cap(ui.RoundCap)
			pt.StrokePathGradient(&path, s.w, ui.LinearGradient{From: s.from, To: s.to, Angle: 180})
		}
		var tip ui.Path
		tip.MoveTo(x-5, r.Y+endY-matchGap-9).LineTo(x+5, r.Y+endY-matchGap-9).LineTo(x, r.Y+endY-matchGap).Close()
		pt.FillPath(&tip, arrow)
	})
}

// chip is one choice of a row of toggles. It keeps the width widest takes
// chosen, the widest its label gets, so that in a row that wraps, choosing
// never moves a chip to another line from under the pointer.
func chip(c *ui.Context, p *pal, key, label, widest string, on bool, onClick func()) {
	tw, _ := c.MeasureText(0, ui.Span{Text: widest, Size: 12, Weight: 600})
	b := ui.ButtonBase(c.Key(key)).Padding(3, 10).Radius(p.r(99)).Shrink(0).MinWidth(tw + 20).Justify(ui.Center)
	switch {
	case on:
		b.Background(p.hair2)
	case b.Hovered():
		b.Background(p.hover)
	}
	col, w := p.mute, 500
	if on {
		col, w = p.ink, 600
	}
	b.OnClick(onClick).Children(func() { ui.Text(c, label).FontSize(12).FontWeight(w).TextColor(col).NoWrap() })
}

// toggled is list with v added, or taken out if it was in.
func toggled(list []string, v string) []string {
	if i := slices.Index(list, v); i >= 0 {
		return slices.Delete(slices.Clone(list), i, i+1)
	}
	return append(slices.Clone(list), v)
}

// scopeRows is 自动最快's range: which nodes the fastest is picked from.
func (u *UI) scopeRows(c *ui.Context, p *pal, rid string, q routes.Policy) {
	row := func(key, label string, body func()) {
		ui.Row(c.Key(key)).Gap(10).AlignItems(ui.Baseline).Children(func() {
			ui.Text(c, label).FontSize(11.5).TextColor(p.mute).NoWrap().Width(30).Shrink(0)
			// Wrapped, not scrolled: a row this short would be half covered
			// by its scroll bar, which takes presses before the chips do.
			ui.Row(c.Key("chips")).Wrap().GapX(2).GapY(4).AlignItems(ui.Center).Grow(1).Shrink(1).Basis(0).MinWidth(0).Children(body)
		})
	}
	count := func(match func(NodeView) bool) string {
		n := 0
		for _, v := range u.snap.Nodes {
			if match(v) {
				n++
			}
		}
		return fmt.Sprintf(" %d", n)
	}
	ui.Column(c).Gap(6).Padding(10, 26).BorderWidth(0, 0, 1, 0).BorderColor(p.hair2).Children(func() {
		row("regions", i18n.T("地区", "Where"), func() {
			u.regionChips(c, p, rid, q, func(r string) (label, widest string) {
				in := func(n NodeView) bool { return n.Where.Place != nil && n.Where.Place.Country == r }
				return r + count(func(n NodeView) bool {
					return in(n) && (len(q.Providers) == 0 || slices.Contains(q.Providers, n.Provider))
				}), r + count(in)
			})
		})
		row("providers", i18n.T("来源", "From"), func() {
			all := i18n.T("全部", "All")
			chip(c, p, "all", all, all, len(q.Providers) == 0, func() { u.changePolicy(rid, func(q *routes.Policy) { q.Providers = nil }) })
			for _, pv := range u.snap.Providers {
				if !pv.Enabled {
					continue
				}
				id := pv.ID
				in := func(n NodeView) bool { return n.Provider == id }
				label := pv.Name + count(func(n NodeView) bool {
					return in(n) && (len(q.Regions) == 0 || n.Where.Place != nil && slices.Contains(q.Regions, n.Where.Place.Country))
				})
				chip(c, p, id, label, pv.Name+count(in), slices.Contains(q.Providers, id), func() {
					u.changePolicy(rid, func(q *routes.Policy) { q.Providers = toggled(q.Providers, id) })
				})
			}
		})
	})
}

// exitWord is where a group leaves now, in a few words.
func (u *UI) exitWord(r RouteView) string {
	if r.Direct {
		return noNodeWord(r)
	}
	switch r.Broken {
	case "":
	case "fixed":
		return fmt.Sprintf(i18n.T("%s 不通", "%s is down"), u.nodeDisplay(r.Policy.Fixed))
	default:
		return i18n.T("没有可用节点", "No usable node")
	}
	if n, ok := u.node(r.Node); ok {
		return nodeFlag2(n) + n.Display
	}
	return "—"
}

func (u *UI) nodeDisplay(name string) string {
	if n, ok := u.node(name); ok {
		return n.Display
	}
	if u.app != nil {
		return u.app.stripped(display(name))
	}
	return display(name)
}

// nodeFlag2 is a node's flag as text, with a space after it.
func nodeFlag2(n NodeView) string {
	if n.Where.Place != nil && n.Where.Place.Flag != "" {
		return n.Where.Place.Flag + " "
	}
	return ""
}

// policyWord is a strategy in a few words: 自动最快 · HK.
func (u *UI) policyWord(r RouteView) string {
	if r.Direct {
		return noNodeWord(r)
	}
	q := u.policy(r.ID)
	switch q.Mode {
	case routes.ModeOrder:
		if i := slices.Index(q.Order, r.Node); i >= 0 && r.Broken == "" {
			return fmt.Sprintf(i18n.T("顺序备用 · 第 %d 位", "Backup order · No. %d"), i+1)
		}
	case routes.ModeAuto:
		if w := scopeWord(q, u.providerName); w != "" {
			return modeName(q.Mode) + " · " + w
		}
	}
	return modeName(q.Mode)
}

func (u *UI) delayWord(r RouteView) (string, ui.Color, bool) {
	n, ok := u.node(r.Node)
	if r.Direct || r.Broken != "" || !ok {
		return "—", ui.Color{}, false
	}
	n = n.On(r.ID)
	return fmtDelay(n), ui.Color{}, n.Tested
}

// ---- The page ----

func (u *UI) routesPage(c *ui.Context, p *pal) {
	if u.snap.Imported != nil {
		u.importedPage(c, p)
		return
	}
	s := u.shownSettings()
	all := routes.Build(s.Routing, s.Rules)
	st := &u.routes
	at := slices.IndexFunc(all, func(r routes.Route) bool { return r.ID == st.group })
	if at < 0 {
		u.selectGroup(routes.Default)
		at = max(0, slices.IndexFunc(all, func(r routes.Route) bool { return r.ID == routes.Default }))
	}
	nsvc, nrules := 0, 0
	for _, r := range all {
		for _, sv := range r.Services {
			if !sv.Mine && !sv.Off {
				nsvc++
			}
			nrules += len(sv.Live())
		}
	}
	ui.Column(c).Fill().Children(func() {
		pageHead(c, p, i18n.T("路由组 · 流量往哪里去", "Routes · Where traffic goes"),
			fmt.Sprintf(i18n.T("%d 个路由组", "%d route groups"), len(all)),
			fmt.Sprintf(i18n.T("%d 个服务 · %d 条规则，写得越具体越优先", "%d services · %d rules, the more specific first"), nsvc, nrules), true, nil)
		u.routesTester(c, p, s, all)
		ui.Row(c).Grow(1).Basis(0).MinHeight(0).Padding(0, 26, 18, 26).Gap(18).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Width(244).Shrink(0).Gap(8).Children(func() {
				ui.Scroll(c.Key("list")).Grow(1).Basis(0).MinHeight(0).Gap(2).Children(func() {
					for _, r := range all {
						if r.ID != routes.Default {
							u.routesItem(c, p, r)
						}
					}
					ui.Text(c, i18n.T("以上规则都没命中", "None of the rules above matched")).FontSize(11).FontWeight(600).TextColor(p.mute).SingleLine().Padding(8, 10, 2, 10)
					u.routesItem(c, p, all[slices.IndexFunc(all, func(r routes.Route) bool { return r.ID == routes.Default })])
				})
				ui.Text(c, i18n.T("拖动可以调整顺序。规则集从上到下匹配；其他规则越具体越优先，同样具体时靠上的路由组优先。", "Drag to reorder. Rule sets match from the top down; other rules go most specific first, and of two equally specific ones the higher group's wins.")).
					FontSize(11).TextColor(p.mute).Padding(0, 10)
				btn(c.Key("new"), p, i18n.T("＋ 新建路由组", "＋ New route group")).AlignSelf(ui.Start).OnClick(func() { u.newGroup(all) })
			})
			ui.Scroll(c.Key("main:"+st.group)).Grow(1).Basis(0).MinWidth(0).Padding(0, 12, 0, 0).Children(func() {
				u.routesGroup(c, p, all, all[at])
			})
		})
	})
}

func (u *UI) newGroup(all []routes.Route) {
	base := i18n.T("新路由组", "New group")
	s := u.shownSettings().Clone()
	id := s.Routing.AddGroup(s.Rules, base)
	u.changeRouting(func(s *settings.Settings) { s.Routing.AddGroup(s.Rules, base) }, nil)
	u.selectGroup(id)
	u.routes.focusName = true
}

// groupDrag and setDrag are a route group and a rule set being dragged to
// another's place, by their ids.
type (
	groupDrag string
	setDrag   string
)

func (u *UI) routesItem(c *ui.Context, p *pal, r routes.Route) {
	st := &u.routes
	on := st.group == r.ID
	v := u.route(r.ID)
	b := ui.ButtonBase(c.Key(r.ID)).Column().Gap(2).Padding(8, 10).Radius(p.r(8)).AlignItems(ui.Stretch)
	over := false
	if !routes.Fixed(r.ID) { // the fixed groups stay where they are, after the others
		b.Drag(groupDrag(r.ID))
		if id, ok := ui.Drop[groupDrag](b); ok {
			u.changeRouting(func(s *settings.Settings) { s.Routing.MoveGroup(string(id), r.ID) }, nil)
		}
		id, ok := ui.DragOver[groupDrag](b)
		over = ok && string(id) != r.ID
	}
	switch {
	case over: // it would take this one's place
		b.Background(p.hair)
	case on:
		b.Background(p.hair2)
	case b.Hovered():
		b.Background(p.hover)
	}
	b.OnClick(func() { u.selectGroup(r.ID) }).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			swatch(c, p.routeColor(r.ID), 9).Shrink(0)
			ui.Text(c, routes.Title(r)).FontSize(13).FontWeight(600).TextColor(p.ink).SingleLine().Grow(1).Basis(0).MinWidth(0)
			if d, _, _ := u.delayWord(v); !r.Direct {
				ui.Text(c, d).FontSize(11.5).FontFeatures("tnum").TextColor(p.mute).NoWrap().Shrink(0)
			}
		})
		line := u.exitWord(v)
		if r.Direct && !v.Direct { // just chosen: the app has yet to say so
			line = noNodeWord(v)
		}
		if !r.Direct {
			line = modeName(u.policy(r.ID).Mode) + " · " + line
		}
		col := p.mute
		if v.Broken != "" {
			col = p.signal
		}
		shimmer(ui.Text(c, line).FontSize(11.5).TextColor(col).SingleLine(), u.pending("node:"+r.ID), p).Margin(0, 0, 0, 17)
	})
}

// routesTester says where an address would go: group, service and rule.
func (u *UI) routesTester(c *ui.Context, p *pal, s settings.Settings, all []routes.Route) {
	st := &u.routes
	ui.Row(c).Margin(0, 26, 12, 26).Padding(8, 14).Gap(12).AlignItems(ui.Center).Radius(p.r(10)).Background(p.card).Border(1, p.hair).Children(func() {
		ui.Text(c, i18n.T("测试一个地址", "Test an address")).FontSize(12.5).FontWeight(600).TextColor(p.ink).NoWrap().Shrink(0)
		ui.TextInput(c.Key("test"), &st.test).Label(i18n.T("测试地址", "Address to test")).Placeholder("api.anthropic.com").
			Padding(4, 8).Radius(p.r(6)).Background(p.paper).Border(1, p.hair).FontSize(12.5).Font(actMono).Width(210).Shrink(0)
		ui.Row(c).Gap(7).AlignItems(ui.Center).Grow(1).Basis(0).MinWidth(0).Children(func() { u.testerPath(c, p, s, all) })
	})
}

func (u *UI) testerPath(c *ui.Context, p *pal, s settings.Settings, all []routes.Route) {
	st := &u.routes
	sep := func(t string) { ui.Text(c, t).FontSize(12).TextColor(p.faint).Shrink(0) }
	host := strings.TrimSpace(st.test)
	if host == "" {
		ui.Text(c, i18n.T("输入域名或 IP，看它会走哪里", "Type a domain or an IP to see where it goes")).FontSize(12).TextColor(p.faint).SingleLine()
		return
	}
	if t, err := routes.ParseRule(routes.KindDomain, host, ""); err == nil {
		host = t.Value
	} else if t, err := routes.ParseRule(routes.KindIP, host, ""); err == nil {
		host, _, _ = strings.Cut(t.Value, "/")
	} else {
		ui.Text(c, err.Error()).FontSize(12).TextColor(p.signal).SingleLine()
		return
	}
	id, rule, set, _, sure := routes.ExplainSets(host, routes.Active(all, s.Rules), all, u.matchRuleSet())
	seen := false
	if !sure {
		for i := len(u.act.conns) - 1; i >= 0; i-- {
			if cn := u.act.conns[i]; (cn.Host == host || cn.IP == host) && cn.Route != "" {
				id, rule, seen = cn.Route, actRule(cn), true
				break
			}
		}
	}
	if id == "" {
		ui.Text(c, i18n.T("没有命中域名规则：国内网站和局域网按 GeoSite / GeoIP 走「直连」，其余归「其他流量」。", "No domain rule matches it: mainland sites and the LAN go Direct by GeoSite / GeoIP, the rest is Other.")).
			FontSize(12).TextColor(p.ink2).SingleLine().Tooltip(host)
		return
	}
	v := u.route(id)
	swatch(c, p.routeColor(id), 9).Shrink(0)
	ui.Text(c, v.Name).FontSize(12.5).FontWeight(600).TextColor(p.ink).NoWrap().Shrink(0)
	var sv routes.Service
	found := false
	if !seen {
		if set != "" {
			sv, found = routes.FindMatched(all, "RuleSet", routes.RuleSetProvider(set))
		} else {
			sv, found = routes.FindRule(all, rule)
		}
		if found {
			sep("/")
			ui.Text(c, routes.ServiceTitle(sv)).FontSize(12.5).TextColor(p.ink).NoWrap().Shrink(0)
		}
	}
	sep("/")
	ui.Text(c, rule).FontSize(11.5).Font(actMono).TextColor(p.mute).SingleLine().Shrink(1).MinWidth(0)
	sep("→")
	ui.Text(c, u.exitWord(v)).FontSize(12.5).FontWeight(600).TextColor(p.ink).SingleLine().Shrink(1).MinWidth(0)
	if found {
		ui.Spacer(c)
		ui.ButtonBase(c.Key("see")).Shrink(0).OnClick(func() { u.openRoutes(id, sv.ID) }).Children(func() {
			ui.Text(c, i18n.T("查看", "View")).FontSize(12).FontWeight(600).Underline().TextColor(p.ink).NoWrap()
		})
	}
}

// routesGroup is the chosen group: name, strategy, services.
func (u *UI) routesGroup(c *ui.Context, p *pal, all []routes.Route, r routes.Route) {
	st := &u.routes
	v := u.route(r.ID)
	fixedName := routes.Fixed(r.ID)
	if st.nameOf != r.ID {
		st.name, st.nameOf, st.nameErr = routes.Title(r), r.ID, ""
	}
	named := 0
	live := 0
	for _, sv := range r.Services {
		if !sv.Mine && !sv.Off {
			named++
		}
		live += len(sv.Live())
	}
	share := int(v.Share*100 + .5)
	count := fmt.Sprintf(i18n.T("今日 %d%% 的流量", "%d%% of today's traffic"), share)
	if r.ID != routes.Default {
		count = fmt.Sprintf(i18n.T("%d 个服务 · %d 条规则 · 今日 %d%% 的流量", "%d services · %d rules · %d%% of today's traffic"), named, live, share)
	}
	if r.Reject { // it carries none
		count = fmt.Sprintf(i18n.T("%d 个服务 · %d 条规则", "%d services · %d rules"), named, live)
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(2, 0, 10, 0).Children(func() {
		swatch(c, p.routeColor(r.ID), 12).Shrink(0)
		if fixedName {
			ui.Text(c, routes.Title(r)).FontSize(22).FontWeight(700).TextColor(p.ink).NoWrap()
		} else {
			in := ui.TextInput(c.Key("name"), &st.name).Bind(&st.nameH).Label(i18n.T("路由组名字", "Route group name")).
				FontSize(22).FontWeight(700).TextColor(p.ink).Padding(0, 4).Margin(0, 0, 0, -4).Radius(p.r(6)).Grow(1).Basis(0).MinWidth(0)
			if st.focusName {
				st.focusName = false
				st.nameH.Focus(c)
			}
			switch {
			case st.nameErr != "":
				in.Border(1, p.signal)
			case st.nameH.Focused(c):
				in.Border(1, p.hair)
			default: // reads as the title until it's being edited
				in.Border(1, ui.RGBA(0, 0, 0, 0)).Background(ui.RGBA(0, 0, 0, 0))
			}
			if in.Changed() {
				st.nameErr = routes.GroupNameError(all, r.ID, st.name)
				if st.nameErr == "" {
					name := st.name
					u.changeRouting(func(s *settings.Settings) { s.Routing.RenameGroup(r.ID, name) }, nil)
				}
			}
		}
		if fixedName {
			ui.Spacer(c)
		}
		ui.Text(c, count).FontSize(12).FontFeatures("tnum").TextColor(p.mute).NoWrap().Shrink(0)
	})
	if st.nameErr != "" {
		ui.Text(c, st.nameErr).FontSize(11.5).TextColor(p.signal).Padding(0, 0, 8, 22)
	}
	note := func(t string) {
		ui.Text(c, t).FontSize(12).TextColor(p.mute).Padding(9, 12).Radius(p.r(8)).Background(p.hair2).Margin(0, 0, 10, 0)
	}
	if r.Reject {
		note(i18n.T("命中这里规则的连接会被拒绝，不经过节点，所以没有节点策略。", "Connections matching the rules here are refused, with no node, so there's no node strategy."))
	} else if !routes.ChoosesExit(r.ID) {
		note(i18n.T("这里的流量直接连接，不经过节点，所以没有节点策略。", "Traffic here connects directly, with no node, so there's no node strategy."))
	} else {
		ui.Column(c).Margin(0, 0, 14, 0).Children(func() { u.exitChoice(c, p, r.ID) })
		if !r.Direct {
			u.strategyCard(c, p, v)
		}
	}
	if r.ID == routes.Default {
		note(i18n.T("没命中任何规则的流量走这里。想让某个网站固定走这里，可以在这里加一条规则。", "Traffic no rule matched goes here. To keep a site here on purpose, add a rule here."))
	}
	u.servicesHead(c, p, all, r)
	if st.rs.open {
		u.ruleSetImport(c, p, all, r)
	}
	list := slices.Clone(r.Services)
	mineAt := func(s routes.Service) int {
		switch {
		case s.Source != "": // up here, so that one just imported is in view
			return 1
		case !s.Mine:
			return 2
		case len(s.Rules) > 0:
			return 0
		}
		return 3
	}
	slices.SortStableFunc(list, func(a, b routes.Service) int { return mineAt(a) - mineAt(b) })
	ui.Column(c).Children(func() {
		for _, sv := range list {
			u.serviceRow(c, p, all, r, sv)
		}
	})
	if !fixedName {
		u.groupDelete(c, p, all, r)
	}
}

// strategyCard is one line until opened, then the same control and node
// list as 代理's in-place expansion.
func (u *UI) strategyCard(c *ui.Context, p *pal, v RouteView) {
	st := &u.routes
	ui.Column(c.Key("pol")).Radius(p.r(10)).Background(p.card).Border(1, p.hair).Margin(0, 0, 14, 0).ClipX().Children(func() {
		b := ui.ButtonBase(c.Key("head")).Row().Gap(10).Padding(10, 14).AlignItems(ui.Center)
		b.Label(i18n.T("节点策略", "Node strategy")).OnClick(func() {
			st.pol = !st.pol
			u.home.region, u.home.query = "", ""
		}).Children(func() {
			ui.Text(c, i18n.T("节点策略", "Node strategy")).FontSize(12.5).FontWeight(600).TextColor(p.ink).NoWrap().Shrink(0)
			ui.Text(c, u.policyWord(v)).FontSize(12.5).TextColor(p.ink2).NoWrap().Shrink(0).Margin(0, 0, 0, 4)
			ui.Text(c, "→").FontSize(12).TextColor(p.faint).Shrink(0)
			col := p.ink
			if v.Broken != "" {
				col = p.signal
			}
			shimmer(ui.Text(c, u.exitWord(v)).FontSize(12.5).FontWeight(600).TextColor(col).SingleLine().Shrink(1).MinWidth(0), u.pending("node:"+v.ID), p)
			ui.Spacer(c)
			d, _, _ := u.delayWord(v)
			ui.Text(c, d).FontSize(12).FontFeatures("tnum").TextColor(p.mute).NoWrap().Shrink(0)
			chevron(c, p.mute, st.pol)
		})
		if st.pol {
			ui.Row(c).Padding(2, 14, 10, 14).Children(func() { u.stratSeg(c, p, v.ID) })
			ui.Column(c.Key("picker")).Height(420).BorderWidth(1, 0, 0, 0).BorderColor(p.hair2).Children(func() {
				u.nodePicker(c, p, v.ID, 660)
			})
		}
	})
}

func (u *UI) servicesHead(c *ui.Context, p *pal, all []routes.Route, r routes.Route) {
	st := &u.routes
	n := 0
	for _, s := range r.Services {
		if !s.Mine {
			n++
		}
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(4, 0, 8, 0).Children(func() {
		ui.Text(c, fmt.Sprintf(i18n.T("服务 · %d", "Services · %d"), n)).FontSize(11.5).FontWeight(700).TextColor(p.mute).Grow(1)
		head := i18n.T("移入已有服务…", "Move a service here…")
		labels := []string{head}
		var ids []string
		for _, g := range all {
			if g.ID == r.ID {
				continue
			}
			for _, s := range g.Services {
				if !s.Mine {
					labels = append(labels, routes.Title(g)+" / "+routes.ServiceTitle(s))
					ids = append(ids, s.ID)
				}
			}
		}
		cur := head
		if ui.Select(c.Key("pull"), &cur, labels).Label(head).Width(170).Changed() {
			if i := slices.Index(labels, cur) - 1; i >= 0 {
				id := ids[i]
				u.changeRouting(func(s *settings.Settings) { s.Routing.MoveService(s.Rules, id, r.ID) }, nil)
				st.svc = id
				u.clearRuleDraft()
			}
		}
		btn(c.Key("import-set"), p, i18n.T("＋ 导入规则集", "＋ Import rule set")).OnClick(u.openRuleSetImport)
		btn(c.Key("new-svc"), p, i18n.T("＋ 新建服务", "＋ New service")).OnClick(func() {
			base := i18n.T("新服务", "New service")
			s := u.shownSettings().Clone()
			id := s.Routing.AddService(s.Rules, r.ID, base)
			u.changeRouting(func(s *settings.Settings) { s.Routing.AddService(s.Rules, r.ID, base) }, nil)
			st.svc = id
			u.clearRuleDraft()
		})
	})
}

func (u *UI) serviceRow(c *ui.Context, p *pal, all []routes.Route, r routes.Route, s routes.Service) {
	st := &u.routes
	open := st.svc == s.ID
	live := s.Live()
	sub := i18n.T("还没有规则", "No rules yet")
	subBad := false
	switch {
	case s.Source != "":
		sub, subBad = u.ruleSetRowWord(s)
	case s.Off:
		sub = i18n.T("已停用，这些流量回到「其他流量」", "Off: its traffic goes back to Other")
	case len(live) > 0:
		shown := make([]string, 0, 2)
		for _, l := range live[:min(2, len(live))] {
			shown = append(shown, l.Shown())
		}
		sub = fmt.Sprintf(i18n.T("%d 条规则 · %s", "%d rules · %s"), len(live), strings.Join(shown, i18n.T("、", ", ")))
		if len(live) > 2 {
			sub += i18n.T(" 等", " and more")
		}
	}
	ui.Column(c.Key(s.ID)).BorderWidth(0, 0, 1, 0).BorderColor(p.hair2).Children(func() {
		b := ui.ButtonBase(c.Key("head")).Row().Gap(10).Padding(9, 4).AlignItems(ui.Center)
		over := false
		if s.Source != "" { // rule sets apply in the order they are listed in
			b.Drag(setDrag(s.ID))
			if id, ok := ui.Drop[setDrag](b); ok {
				u.changeRouting(func(st *settings.Settings) { st.Routing.MoveRuleSet(string(id), s.ID) }, nil)
			}
			id, ok := ui.DragOver[setDrag](b)
			over = ok && string(id) != s.ID
		}
		switch {
		case over: // it would take this one's place
			b.Background(p.hair)
		case b.Hovered():
			b.Background(p.hover)
		}
		if s.Off {
			b.Opacity(.5)
		}
		b.Label(routes.ServiceTitle(s)).OnClick(func() {
			if open {
				st.svc = ""
			} else {
				st.svc = s.ID
			}
			st.armed = ""
			u.clearRuleDraft()
		}).Children(func() {
			ui.Column(c).Gap(2).Grow(1).Basis(0).MinWidth(0).Children(func() {
				ui.Text(c, routes.ServiceTitle(s)).FontSize(13).FontWeight(600).TextColor(p.ink).SingleLine()
				subCol := p.mute
				if subBad {
					subCol = p.signal
				}
				ui.Text(c, sub).FontSize(11.5).TextColor(subCol).SingleLine()
			})
			tag := ""
			switch {
			case s.Builtin:
				tag = i18n.T("内置", "Built-in")
			case s.Source != "":
				tag = i18n.T("规则集", "Rule set")
			case !s.Mine:
				tag = i18n.T("自定义", "Custom")
			}
			if tag != "" {
				ui.Text(c, tag).FontSize(11).TextColor(p.faint).NoWrap().Shrink(0)
			}
			chevron(c, p.mute, open)
		})
		if open {
			u.serviceEditor(c, p, all, r, s)
		}
	})
}

func (u *UI) serviceEditor(c *ui.Context, p *pal, all []routes.Route, r routes.Route, s routes.Service) {
	st := &u.routes
	ui.Column(c.Key("editor")).Gap(8).Padding(2, 4, 12, 4).Children(func() {
		if !s.Builtin && !s.Mine {
			u.serviceName(c, p, all, s)
		}
		if s.Source != "" {
			u.ruleSetBody(c, p, s)
		} else {
			if s.Mine {
				ui.Text(c, i18n.T("在「日志」里点「让它走…」加的规则放在这里。", "Rules made with Send via… in Log land here.")).FontSize(12).TextColor(p.mute)
			}
			if len(s.Rules) == 0 {
				ui.Text(c, i18n.T("还没有规则，在下面加一条。", "No rules yet. Add one below.")).FontSize(12).TextColor(p.faint).Padding(6, 0)
			}
			ui.Column(c).Children(func() {
				for _, l := range s.Rules {
					u.ruleLine(c, p, s, l)
				}
			})
			u.ruleInput(c, p, all, s)
		}
		if s.Mine {
			return
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(6, 0, 0, 0).Children(func() {
			head := i18n.T("移到…", "Move to…")
			labels := []string{head}
			var ids []string
			for _, g := range all {
				if g.ID != s.Group {
					labels, ids = append(labels, routes.Title(g)), append(ids, g.ID)
				}
			}
			cur := head
			if ui.Select(c.Key("move"), &cur, labels).Label(head).Width(130).Changed() {
				if i := slices.Index(labels, cur) - 1; i >= 0 {
					to := ids[i]
					u.changeRouting(func(st *settings.Settings) { st.Routing.MoveService(st.Rules, s.ID, to) }, nil)
					u.selectGroup(to)
					st.svc = s.ID
				}
			}
			n := 0
			for _, o := range r.Services {
				if !o.Mine {
					n++
				}
			}
			if !r.Direct && n >= 2 {
				btn(c.Key("split"), p, i18n.T("单独成组", "Own group")).OnClick(func() {
					next := u.shownSettings().Clone()
					id := next.Routing.SplitService(next.Rules, s.ID)
					u.changeRouting(func(st *settings.Settings) { st.Routing.SplitService(st.Rules, s.ID) }, nil)
					u.selectGroup(id)
					st.svc = s.ID
				})
			}
			ui.Spacer(c)
			if s.Builtin && s.Changed {
				btn(c.Key("reset"), p, i18n.T("恢复默认", "Reset")).OnClick(func() {
					u.changeRouting(func(st *settings.Settings) { st.Rules = st.Routing.RemoveService(st.Rules, s.ID) }, nil)
				})
			}
			if s.Builtin || s.Source != "" {
				label := i18n.T("停用", "Turn off")
				if s.Off {
					label = i18n.T("启用", "Turn on")
				}
				btn(c.Key("off"), p, label).OnClick(func() {
					u.changeRouting(func(st *settings.Settings) { st.Routing.ToggleService(s.ID) }, nil)
				})
			}
			if s.Builtin {
				return
			}
			armed := st.armed == "s:"+s.ID
			label := i18n.T("删除", "Delete")
			if armed {
				label = i18n.T("确定删除", "Really delete")
			}
			dangerBtn(c.Key("del"), p, label, armed).OnClick(func() {
				if !armed {
					st.armed = "s:" + s.ID
					return
				}
				st.armed, st.svc = "", ""
				var after func()
				if s.Source != "" {
					after = func() { u.app.ruleSets.Delete(s.ID) } // once the core no longer reads it
				}
				u.changeRouting(func(st *settings.Settings) { st.Rules = st.Routing.RemoveService(st.Rules, s.ID) }, after)
			})
		})
	})
}

func dangerBtn(c *ui.Context, p *pal, label string, armed bool) ui.Element {
	b := ui.ButtonBase(c).Padding(5, 11).Radius(p.r(7)).Border(1, p.hair)
	col := p.signal
	if armed {
		b.Background(p.signal).BorderColor(p.signal)
		col = p.onAccent
	} else if b.Hovered() {
		b.BorderColor(p.signal)
	}
	return b.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(col).NoWrap() })
}

func (u *UI) serviceName(c *ui.Context, p *pal, all []routes.Route, s routes.Service) {
	st := &u.routes
	if st.snameOf != s.ID {
		st.sname, st.snameOf, st.snameErr = s.Name, s.ID, ""
	}
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("名字", "Name")).FontSize(12).TextColor(p.mute).NoWrap().Shrink(0)
			in := ui.TextInput(c.Key("sname"), &st.sname).Label(i18n.T("服务名字", "Service name")).Padding(4, 8).Radius(p.r(6)).
				Background(p.paper).Border(1, p.hair).FontSize(12.5).Width(220)
			if st.snameErr != "" {
				in.BorderColor(p.signal)
			}
			if in.Changed() {
				st.snameErr = routes.ServiceNameError(all, s.ID, st.sname)
				if st.snameErr == "" {
					name := st.sname
					u.changeRouting(func(st *settings.Settings) { st.Routing.RenameService(s.ID, name) }, nil)
				}
			}
		})
		if st.snameErr != "" {
			ui.Text(c, st.snameErr).FontSize(11.5).TextColor(p.signal).Margin(0, 0, 0, 36)
		}
	})
}

func (u *UI) ruleLine(c *ui.Context, p *pal, s routes.Service, l routes.Rule) {
	ui.Row(c.Key(l.Text)).Gap(10).Padding(5, 0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.hair2).Children(func() {
		col := p.ink
		if l.Off || s.Off {
			col = p.faint
		}
		ui.Text(c, l.Shown()).Font(actMono).FontSize(12.5).TextColor(col).SingleLine().Grow(1).Basis(0).MinWidth(0)
		ui.Text(c, routes.KindName(l.Kind)).FontSize(11).TextColor(p.mute).NoWrap().Shrink(0)
		if l.Builtin {
			label := i18n.T("停用", "Turn off")
			if l.Off {
				label = i18n.T("启用", "Turn on")
			}
			btn(c.Key("t"), p, label).OnClick(func() {
				u.changeRouting(func(st *settings.Settings) { st.Routing.ToggleRule(s.ID, l.Text) }, nil)
			})
			return
		}
		btn(c.Key("d"), p, i18n.T("删除", "Delete")).OnClick(func() { u.setRule(l.User(s), true) })
	})
}

func (u *UI) ruleInput(c *ui.Context, p *pal, all []routes.Route, s routes.Service) {
	st := &u.routes
	if st.focusRule {
		st.focusRule = false
		st.ruleH.Focus(c)
	}
	typed := strings.TrimSpace(st.rule) != ""
	r, err := routes.ParseRule(st.kind, st.rule, s.Group)
	if !s.Mine {
		r.Service = s.ID
	}
	add := func() {
		if !typed || err != nil {
			return
		}
		st.ruleErr, st.conflict = "", nil
		if from, ok := routes.FindRule(all, r.Mihomo()); ok {
			if from.ID == s.ID {
				st.ruleErr = i18n.T("这条规则已经在这个服务里了", "That rule is already in this service")
			} else {
				st.conflict = &ruleConflict{r, from}
			}
			return
		}
		u.setRule(r, false)
		st.rule, st.focusRule = "", true
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(4, 0, 0, 0).Children(func() {
		in := ui.TextInput(c.Key("rule"), &st.rule).Bind(&st.ruleH).Label(i18n.T("新规则", "New rule")).
			Placeholder(i18n.T("*.netflix.com、openai.com 或 10.0.0.0/8", "*.netflix.com, openai.com or 10.0.0.0/8")).
			Padding(5, 9).Radius(p.r(7)).Background(p.paper).Border(1, p.hair).FontSize(12.5).Font(actMono).Grow(1).Basis(0).MinWidth(0)
		if in.Changed() {
			st.ruleErr, st.conflict = "", nil
		}
		if in.Submitted() {
			add()
		}
		kl := routeKindLabels()
		cur := kl[max(0, slices.Index(routeKinds, st.kind))]
		if ui.Select(c.Key("kind"), &cur, kl).Label(i18n.T("匹配方式", "Match")).Width(128).Changed() {
			st.kind = routeKinds[max(0, slices.Index(kl, cur))]
		}
		b := btn(c.Key("add"), p, i18n.T("添加", "Add"))
		if typed && err == nil {
			b.OnClick(add)
		} else {
			b.Opacity(.45)
		}
	})
	switch {
	case st.ruleErr != "":
		ui.Text(c, st.ruleErr).FontSize(11.5).TextColor(p.signal)
	case !typed:
		ui.Text(c, i18n.T("粘贴链接也可以，会自动提取域名。", "A pasted link works too: the domain is taken from it.")).FontSize(11.5).TextColor(p.faint)
	case err != nil:
		ui.Text(c, err.Error()).FontSize(11.5).TextColor(p.signal)
	default:
		ui.Text(c, fmt.Sprintf(i18n.T("添加后：%s · %s", "Will add: %s · %s"), r.Value, routes.KindName(r.Kind))).FontSize(11.5).TextColor(p.ink2)
	}
	if cf := st.conflict; cf != nil {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(7, 10).Radius(p.r(8)).Background(p.hair2).Children(func() {
			path := u.route(cf.from.Group).Name + " / " + routes.ServiceTitle(cf.from)
			ui.Text(c, fmt.Sprintf(i18n.T("%s 已经在 %s 里，一条规则只能属于一个服务。", "%s is already in %s; a rule belongs to one service."), cf.rule.Describe(), path)).
				FontSize(12).TextColor(p.ink).Grow(1).Basis(0).MinWidth(0)
			btn(c.Key("cf-move"), p, i18n.T("移过来", "Move it here")).OnClick(func() {
				u.setRule(cf.rule, false)
				st.rule, st.conflict, st.focusRule = "", nil, true
			})
			btn(c.Key("cf-x"), p, i18n.T("取消", "Cancel")).OnClick(func() { st.conflict = nil })
		})
	}
}

func (u *UI) groupDelete(c *ui.Context, p *pal, all []routes.Route, r routes.Route) {
	st := &u.routes
	armed := st.armed == "g:"+r.ID
	ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.End).Padding(14, 0, 4, 0).Children(func() {
		var ids, names []string
		for _, g := range all {
			if g.ID != r.ID {
				ids, names = append(ids, g.ID), append(names, routes.Title(g))
			}
		}
		if !slices.Contains(ids, st.delTo) {
			st.delTo = routes.Default
		}
		if armed {
			ui.Text(c, i18n.T("服务交给", "Hand services to")).FontSize(12).TextColor(p.mute).NoWrap()
			cur := names[max(0, slices.Index(ids, st.delTo))]
			if ui.Select(c.Key("to"), &cur, names).Label(i18n.T("服务交给", "Hand services to")).Width(130).Changed() {
				st.delTo = ids[max(0, slices.Index(names, cur))]
			}
			btn(c.Key("del-x"), p, i18n.T("取消", "Cancel")).OnClick(func() { st.armed = "" })
		}
		label := i18n.T("删除路由组", "Delete route group")
		if armed {
			label = i18n.T("确定删除", "Really delete")
		}
		dangerBtn(c.Key("del"), p, label, armed).OnClick(func() {
			if !armed {
				st.armed = "g:" + r.ID
				return
			}
			to := st.delTo
			u.changeRouting(func(s *settings.Settings) {
				if s.Routing.DeleteGroup(s.Rules, r.ID, to) {
					delete(s.RouteTargets, r.ID)
				}
			}, nil)
			u.selectGroup(to)
		})
	})
}
