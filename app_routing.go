package main

// The route groups at run time: what the user set (settings.Routing, in
// internal/routes), the node lists the config needs for it, and keeping the
// core's selectors on what each group's strategy says.

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"slices"
	"time"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

// routes are the route groups as the user has them now. It is asked for
// many times a second, for each connection: built once per change.
func (a *App) routes() []routes.Route {
	gen := a.settings.Gen()
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	if a.routesAt != gen || a.routesNow == nil {
		s := a.settings.Get()
		if im := a.importedLocked(s); im != nil {
			a.routesNow = importedRoutes(im)
		} else {
			a.routesNow = routes.Build(s.Routing, s.Rules)
		}
		a.routesAt = gen
	}
	return a.routesNow
}

// fileNodes are the full names of the enabled providers' nodes, read from
// their files, since the config that loads them must already list them.
func (a *App) fileNodes() []string {
	var out []string
	for _, p := range a.providers.List() {
		if !p.Enabled {
			continue
		}
		for _, name := range a.providers.Names(p.ID) {
			out = append(out, name+core.NodeSep+p.ID)
		}
	}
	slices.Sort(out)
	return out
}

// nodeLists are the node lists the config needs for the groups' strategies.
func (a *App) nodeLists(all []routes.Route) (scopes, orders map[string][]string) {
	scopes, orders = map[string][]string{}, map[string][]string{}
	names := a.fileNodes()
	for _, r := range all {
		if r.Direct {
			continue
		}
		q := r.Policy
		if q.Scoped() {
			list := []string{}
			for _, n := range names {
				country := ""
				if pl := a.loc.Guess(n, display(n)).Place; pl != nil {
					country = pl.Country
				}
				if inScope(q, providerOf(n), country) {
					list = append(list, n)
				}
			}
			scopes[r.ID] = list
		}
		if q.Mode == routes.ModeOrder || len(q.Order) > 0 {
			list := []string{}
			for _, n := range q.Order {
				if slices.Contains(names, n) {
					list = append(list, n) // one no longer there is skipped
				}
			}
			orders[r.ID] = list
		}
	}
	return scopes, orders
}

// inScope says whether a node from provider, leaving from country, is in
// the range of q's automatic choice.
func inScope(q routes.Policy, provider, country string) bool {
	return (len(q.Providers) == 0 || slices.Contains(q.Providers, provider)) &&
		(len(q.Regions) == 0 || slices.Contains(q.Regions, country))
}

// wanted is what route r's selector should be on.
func wanted(r routes.Route) string {
	switch r.Policy.Mode {
	case routes.ModeOrder:
		return routes.Backup(r)
	case routes.ModeFixed:
		if r.Policy.Fixed != "" {
			return r.Policy.Fixed
		}
	}
	return routes.Auto(r)
}

// adoptSelections fills in the strategy of groups that have none, from
// what the core remembers: routes from before strategies existed were
// either on their automatic group or on a node the user picked.
func (a *App) adoptSelections() {
	if a.importedNow() != nil {
		return // the core's selectors are the source's groups, not the strategies'
	}
	s := a.settings.Get()
	all := routes.Build(s.Routing, s.Rules)
	if !slices.ContainsFunc(all, func(r routes.Route) bool { return !r.Direct && s.Routing.Policy(r.ID).Mode == "" }) {
		return
	}
	var px map[string]mihomo.Proxy
	err := a.coreCall(3*time.Second, func(ctx context.Context, c *mihomo.Client) (err error) {
		px, err = c.Proxies(ctx)
		return err
	})
	if err != nil {
		return
	}
	_, _ = a.settings.Update(func(st *settings.Settings) {
		for _, r := range all {
			if r.Direct || st.Routing.Policy(r.ID).Mode != "" {
				continue
			}
			if now := px[routes.Group(r)].Now; now != "" && !routes.Internal(r, now) {
				st.Routing.PickNode(r.ID, now)
			} else {
				st.Routing.ChangePolicy(r.ID, func(q *routes.Policy) { q.Mode = routes.ModeAuto })
			}
		}
	})
}

// followPolicies puts every group's selector where its strategy says. A
// strategy is only a selector's position, so changing one reloads nothing.
func (a *App) followPolicies() error {
	if a.importedNow() != nil {
		return nil
	}
	all := a.routes()
	return a.coreCall(5*time.Second, func(ctx context.Context, c *mihomo.Client) error {
		px, err := c.Proxies(ctx)
		if err != nil {
			return err
		}
		var first error
		for _, r := range all {
			g, ok := px[routes.Group(r)]
			if r.Direct || !ok {
				continue
			}
			want := wanted(r)
			if g.Now == want || !slices.Contains(g.All, want) {
				continue // a fixed node that is gone shows as broken
			}
			if err := c.Select(ctx, routes.Group(r), want); err != nil && first == nil {
				first = err
			}
		}
		return first
	})
}

// routingCoreSame says whether two plans give the core the same config:
// names and which selector position a strategy means are not in it.
func routingCoreSame(a, b routes.Plan) bool {
	return reflect.DeepEqual(coreView(a), coreView(b))
}

func coreView(p routes.Plan) routes.Plan {
	p = p.Clone()
	for i := range p.Groups {
		g := &p.Groups[i]
		g.Name, g.Policy.Fixed = "", ""
		if g.Policy.Mode == routes.ModeOrder || len(g.Policy.Order) > 0 {
			g.Policy.Mode = routes.ModeOrder
		} else {
			g.Policy.Mode = ""
		}
	}
	for i := range p.Services {
		p.Services[i].Name = ""
	}
	return p
}

// brokenWhy says why a group has no way out now, "" if it has one: "scope"
// (nothing usable in its range), "order" (none of its list works), "fixed"
// (its node doesn't). Only what has been tested counts as down.
func brokenWhy(q routes.Policy, nodes []NodeView, rid string) string {
	if len(nodes) == 0 {
		return ""
	}
	alive := func(n NodeView) bool { n = n.On(rid); return !n.Tested || n.Delay > 0 }
	switch q.Mode {
	case routes.ModeFixed:
		if !slices.ContainsFunc(nodes, func(n NodeView) bool { return n.Name == q.Fixed && alive(n) }) {
			return "fixed"
		}
	case routes.ModeOrder:
		if !slices.ContainsFunc(nodes, func(n NodeView) bool { return slices.Contains(q.Order, n.Name) && alive(n) }) {
			return "order"
		}
	default:
		if q.Scoped() && !slices.ContainsFunc(nodes, func(n NodeView) bool { return nodeInScope(q, n) && alive(n) }) {
			return "scope"
		}
	}
	return ""
}

func nodeInScope(q routes.Policy, n NodeView) bool {
	country := ""
	if n.Where.Place != nil {
		country = n.Where.Place.Country
	}
	return inScope(q, n.Provider, country)
}

// scopeWord is a range in words: its regions, then its providers.
func scopeWord(q routes.Policy, provName func(string) string) string {
	out := ""
	add := func(s string) {
		if out != "" {
			out += i18n.T("、", ", ")
		}
		out += s
	}
	for _, r := range q.Regions {
		add(r)
	}
	for _, id := range q.Providers {
		add(provName(id))
	}
	return out
}

// brokenIssue is what the title bar says about a broken group.
func (a *App) brokenIssue(r RouteView, nodes []NodeView, also int) *Issue {
	is := &Issue{Level: "error", Action: "fix:" + r.ID}
	switch r.Broken {
	case "fixed":
		name := r.Policy.Fixed
		if n := slices.IndexFunc(nodes, func(n NodeView) bool { return n.Name == name }); n >= 0 {
			name = nodes[n].Display
		}
		is.Text = fmt.Sprintf(i18n.T("「%s」固定的 %s 不通", "%s: its fixed node %s is down"), r.Name, name)
		is.Detail = i18n.T("固定节点不会自动切换，这个路由组现在连不上。", "A fixed node doesn't switch by itself, so this route group can't connect now.")
		is.Label = i18n.T("换节点", "Change node")
	case "order":
		is.Text = fmt.Sprintf(i18n.T("「%s」备用列表里的节点都不通", "%s: every node on its list is down"), r.Name)
		is.Detail = i18n.T("这个路由组现在连不上。", "This route group can't connect now.")
		is.Label = i18n.T("调整顺序", "Edit order")
	default:
		is.Text = fmt.Sprintf(i18n.T("「%s」的范围（%s）里没有可用节点", "%s: no usable node in its range (%s)"), r.Name, scopeWord(r.Policy, a.providerTitle))
		is.Detail = i18n.T("范围不会被悄悄放宽，这个路由组现在连不上。", "A range is never widened on its own, so this route group can't connect now.")
		is.Label = i18n.T("改范围", "Change range")
	}
	if also > 0 {
		is.Text += fmt.Sprintf(i18n.T("，另有 %d 个路由组也是", ", and %d more route groups"), also)
	}
	return is
}

func (a *App) providerTitle(id string) string {
	if p, ok := a.providers.Get(id); ok {
		return p.Name
	}
	return id
}

// logPolicyErr records a selector that would not move.
func logPolicyErr(err error) {
	if err != nil {
		log.Println("route group:", err)
	}
}
