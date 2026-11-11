package main

// A provider's own routing: when the user chooses it, the proxy-groups and
// rules of a complete Clash config replace the route groups (D1 in
// docs/product-ia.md). The config comes from internal/core; here is what the
// rest of the app sees of it. Its groups that rules point at are routes like
// any other, so 今日, the map, 活动 and the menu bar need no second way of
// working; what would edit bowel's route groups is out of reach until the
// user comes back to them.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/mcp"
	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

// importedCache is guarded by routesMu. Reading a config is not for every
// connection: applyConfig, which follows every change of the file, drops it.
type importedCache struct {
	loaded bool
	key    string // the provider it was read for, "" when none is in use
	now    *core.Imported
	err    string // why the routing in use could not be read
	info   map[string]routingInfo
}

// routingInfo is what a provider's file brings, as of updatedAt.
type routingInfo struct {
	at      time.Time
	local   string // the local files there were to match by name
	routing ProviderRouting
}

// localFiles gives, for a `type: file` proxy-provider of config's, the files
// bowel keeps under that name: those of the enabled local file Providers
// called the same. The user put each there; the path the config names for
// its provider is never read (issue #75).
func (a *App) localFiles(config string) func(name string) []string {
	return func(name string) []string {
		var paths []string
		for _, p := range a.providers.List() {
			if p.Kind == provider.File && p.Enabled && p.ID != config && p.Name == name {
				paths = append(paths, a.providers.Path(p.ID))
			}
		}
		return paths
	}
}

// localSig changes when what localFiles has to offer does.
func (a *App) localSig() string {
	var b strings.Builder
	for _, p := range a.providers.List() {
		if p.Kind == provider.File && p.Enabled {
			b.WriteString(p.ID + "\x00" + p.Name + "\x00")
		}
	}
	return b.String()
}

func (a *App) dropImported() {
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	a.imp.loaded, a.routesNow = false, nil
}

// importedNow is the routing in use from a provider's config, nil with
// bowel's own.
func (a *App) importedNow() *core.Imported {
	s := a.settings.Get()
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	return a.importedLocked(s)
}

func (a *App) importedLocked(s settings.Settings) *core.Imported {
	key := ""
	if p, ok := a.providers.Get(s.ImportedRouting); ok && p.Enabled {
		key = p.ID
	}
	if a.imp.loaded && a.imp.key == key {
		return a.imp.now
	}
	a.imp.loaded, a.imp.key, a.imp.now, a.imp.err = true, key, nil, ""
	if key == "" {
		return nil
	}
	data, err := a.providers.Content(key)
	var im *core.Imported
	if err == nil {
		im, err = core.ParseImported(data, a.localFiles(key))
	}
	if err == nil && im == nil {
		err = errors.New(i18n.T("这份配置里没有路由组", "This config has no route groups"))
	}
	if err != nil {
		a.imp.err = err.Error()
		return nil
	}
	a.imp.now = im
	return im
}

// importedRoutes are im's routes: "this traffic goes that way", which is
// what a group is when rules point at it. MATCH's target is the default
// route, the one the exit, the map and the menu bar follow.
func importedRoutes(im *core.Imported) []routes.Route {
	match := im.Match()
	route := func(id, name string) routes.Route {
		return routes.Route{ID: id, Name: name, Summary: fmt.Sprintf(i18n.T("%d 条规则指向这里", "%d rules point here"), im.RulesTo(name))}
	}
	direct := func(id, name string, reject bool) routes.Route {
		r := route(id, name)
		r.Direct, r.Reject, r.Color = true, reject, "--r-direct"
		return r
	}
	var def routes.Route
	switch match {
	case "", "DIRECT":
		def = direct(routes.Default, "DIRECT", false)
		def.Label = i18n.T("其他流量", "Other")
	case "REJECT", "REJECT-DROP":
		def = direct(routes.Default, match, true)
		def.Label = i18n.T("其他流量", "Other")
	default:
		def = route(routes.Default, match)
	}
	out := []routes.Route{def}
	n, hasDirect, hasReject := 0, false, false
	for _, t := range im.Targets() {
		switch t {
		case match, "PASS", "PASS-RULE", "COMPATIBLE", "GLOBAL":
		case "DIRECT":
			hasDirect = true
		case "REJECT", "REJECT-DROP":
			hasReject = true
		default:
			n++
			out = append(out, route("g"+strconv.Itoa(n), t))
		}
	}
	if hasDirect {
		r := direct("direct", "DIRECT", false)
		r.Label = i18n.T("直连", "Direct")
		out = append(out, r)
	}
	if hasReject {
		r := direct(routes.Reject, "REJECT", true)
		r.Label = i18n.T("拦截", "Block")
		out = append(out, r)
	}
	return out
}

// listedNode is the name the node list has for one a group names: the
// provider's copy of a node carries its marker, the group's has none.
func (a *App) listedNode(name string, provNodes map[string][]string) string {
	if a.importedNow() == nil || name == "" {
		return name
	}
	id := a.settings.Get().ImportedRouting
	if full := name + core.NodeSep + id; slices.Contains(provNodes[id], full) {
		return full
	}
	return name
}

// sourceName is the name in the source's config of a node of the node list.
func (a *App) sourceName(node string) string {
	if providerOf(node) == a.settings.Get().ImportedRouting {
		return display(node)
	}
	return node
}

func (a *App) ruleCount(s settings.Settings) int {
	if im := a.importedNow(); im != nil {
		return len(im.Rules)
	}
	return routes.Count(a.routes()) + len(routes.Active(a.routes(), s.Rules))
}

// importedView is im as it stands in the core.
func (a *App) importedView(s settings.Settings, im *core.Imported, px map[string]mihomo.Proxy, provNodes map[string][]string) *ImportedView {
	if im == nil {
		return nil
	}
	v := &ImportedView{Provider: s.ImportedRouting, Name: a.providerTitle(s.ImportedRouting), Rules: len(im.Rules), PausedRules: len(s.Rules), PausedSets: len(a.ruleSetRefs(s)), Groups: []GroupView{}}
	byName := map[string]string{}
	for _, r := range a.routes() {
		byName[r.Name] = r.ID
	}
	groups := map[string]bool{}
	for _, g := range im.Groups {
		groups[core.GroupName(g)] = true
	}
	for _, g := range im.Groups {
		name := core.GroupName(g)
		p := px[name]
		gv := GroupView{Name: name, Kind: core.GroupKind(g), Route: byName[name], Rules: im.RulesTo(name), Now: p.Now, Selectable: core.GroupKind(g) == "select", Members: []MemberView{}}
		leaf := p.Now
		for i := 0; i < 16 && px[leaf].Now != ""; i++ {
			leaf = px[leaf].Now
		}
		if leaf != "DIRECT" && leaf != "REJECT" && leaf != "REJECT-DROP" {
			gv.Exit = a.listedNode(leaf, provNodes)
		}
		for _, m := range p.All {
			mv := MemberView{Name: m, Group: groups[m]}
			if !mv.Group {
				mv.Node = a.listedNode(m, provNodes)
			}
			if h := px[m].History; len(h) > 0 {
				mv.Tested, mv.Delay = true, h[len(h)-1].Delay
			}
			gv.Members = append(gv.Members, mv)
		}
		v.Groups = append(v.Groups, gv)
	}
	return v
}

// importedIssue is the news line about the routing in use: a config that
// has stopped working, or an update that was not applied.
func (a *App) importedIssue(s settings.Settings, snap Snapshot) *Issue {
	a.routesMu.Lock()
	why := a.imp.err
	a.routesMu.Unlock()
	if why != "" {
		return &Issue{Level: "warn", Text: i18n.T("读不了这份配置的路由，现在用的是 bowel 路由", "This config's routing can't be read; bowel's routing is in use"), Detail: why, Action: "providers", Label: i18n.T("查看", "View")}
	}
	if snap.Imported == nil {
		return nil
	}
	for _, p := range snap.Providers {
		if p.ID == s.ImportedRouting && p.Error != "" {
			return &Issue{Level: "warn", Text: fmt.Sprintf(i18n.T("%s 这次更新没有应用，路由没有变化", "%s: the last update wasn't applied; the routing is unchanged"), a.providerShown(p.ID)), Detail: p.Error, Action: "providers", Label: i18n.T("查看", "View")}
		}
	}
	return nil
}

// providerRouting says what p's file brings. Asked for each snapshot, it
// reads a file only when that has changed.
func (a *App) providerRouting(p provider.Provider) ProviderRouting {
	s := a.settings.Get()
	local := a.localSig()
	a.routesMu.Lock()
	info, ok := a.imp.info[p.ID]
	a.routesMu.Unlock()
	if !ok || !info.at.Equal(p.UpdatedAt) || info.local != local {
		info = routingInfo{at: p.UpdatedAt, local: local}
		if data, err := a.providers.Content(p.ID); err == nil && core.HasGroups(data) {
			if im, err := core.ParseImported(data, a.localFiles(p.ID)); err != nil {
				info.routing.Problem = err.Error()
				var missing *core.MissingFile
				if errors.As(err, &missing) {
					info.routing.NeedsFile = missing.Name
				}
			} else if im != nil {
				info.routing.Groups, info.routing.Rules = len(im.Groups), len(im.Rules)
			}
		}
		a.routesMu.Lock()
		if a.imp.info == nil {
			a.imp.info = map[string]routingInfo{}
		}
		a.imp.info[p.ID] = info
		a.routesMu.Unlock()
	}
	r := info.routing
	r.InUse = p.Enabled && s.ImportedRouting == p.ID && r.Groups > 0
	return r
}

// vetUpdate keeps an update of the provider in use from breaking the
// routing: what comes down must still have working groups, or the last good
// file stays and the update counts as failed. Never the template instead.
func (a *App) vetUpdate(id string, data []byte) error {
	s := a.settings.Get()
	if s.ImportedRouting != id {
		return nil
	}
	im, err := core.ParseImported(data, a.localFiles(id))
	if err == nil && im == nil {
		err = errors.New(i18n.T("这次下载的内容里没有路由组", "What was downloaded has no route groups"))
	}
	if err == nil {
		err = a.checkImported(s, im)
	}
	if err != nil {
		return fmt.Errorf(i18n.T("这次更新没有应用，继续用上一份：%w", "This update wasn't applied; the last one stays: %w"), err)
	}
	return nil
}

func (a *App) checkImported(s settings.Settings, im *core.Imported) error {
	cfg, err := a.buildConfig(s, overlayOf(s, a.devEnv()), im)
	if err != nil {
		return err
	}
	return a.core.Check(cfg)
}

// setImportedRouting uses provider id's own routing; "" goes back to
// bowel's. The user's rules and route groups are not touched either way.
// Connections already open would stay on the old routing: they are closed
// so that the change shows at once, as a change of rules does.
func (a *App) setImportedRouting(id string) error {
	if id != "" {
		p, ok := a.providers.Get(id)
		if !ok {
			return fmt.Errorf(i18n.T("没有这个 Provider: %s", "No such Provider: %s"), id)
		}
		if !p.Enabled {
			return errors.New(i18n.T("这个 Provider 已停用", "This Provider is disabled"))
		}
		data, err := a.providers.Content(id)
		if err != nil {
			return err
		}
		im, err := core.ParseImported(data, a.localFiles(id))
		if err != nil {
			return err
		}
		if im == nil {
			return errors.New(i18n.T("这份配置里没有路由组", "This config has no route groups"))
		}
	}
	if err := a.updateSettings(func(s *settings.Settings) { s.ImportedRouting = id }); err != nil {
		return err
	}
	_ = a.coreCall(5*time.Second, func(ctx context.Context, c *mihomo.Client) error { return c.CloseAllConnections(ctx) })
	go a.locateExits(false)
	return nil
}

// selectMember points an imported group of the kind the user chooses in at
// member. The core remembers it across reloads (store-selected).
func (a *App) selectMember(group, member string) error {
	im := a.importedNow()
	if im == nil {
		return errors.New(i18n.T("没有这个路由组", "No such route group"))
	}
	g, ok := im.GroupOf(group)
	if !ok {
		return errors.New(i18n.T("没有这个路由组", "No such route group"))
	}
	if core.GroupKind(g) != "select" {
		return fmt.Errorf(i18n.T("「%s」的类型是 %s，由内核自己选择", "%s is of the kind %s: the core chooses by itself"), group, core.GroupKind(g))
	}
	if member == "" {
		return errors.New(i18n.T("这个路由组需要选一个成员", "This route group needs a member chosen"))
	}
	done := a.begin("group:" + group)
	defer done()
	if err := a.coreCall(5*time.Second, func(ctx context.Context, c *mihomo.Client) error { return c.Select(ctx, group, member) }); err != nil {
		return err
	}
	a.refresh()
	return nil
}

// rulesEditable refuses a change to the user's rules while an imported
// config has them paused: it could not take effect.
func (a *App) rulesEditable() error {
	if a.importedNow() != nil {
		return errors.New(i18n.T("正在使用导入配置的路由，你的规则已暂停。改回 bowel 路由后才能修改", "An imported config's routing is in use and your rules are paused. Go back to bowel's routing to change them"))
	}
	return nil
}

// mcpImported is the imported routing as agents read it.
func mcpImported(v *ImportedView) map[string]any {
	groups := make([]map[string]any, 0, len(v.Groups))
	for _, g := range v.Groups {
		members := make([]string, 0, len(g.Members))
		for _, m := range g.Members {
			members = append(members, m.Name)
		}
		groups = append(groups, map[string]any{"name": g.Name, "kind": g.Kind, "now": g.Now, "exit": display(g.Exit), "selectable": g.Selectable, "rules": g.Rules, "members": members})
	}
	return map[string]any{"provider": v.Name, "rules": v.Rules, "pausedUserRules": v.PausedRules, "pausedRuleSets": v.PausedSets, "groups": groups}
}

// mcpSelectMember is select_node under an imported config.
func (a *App) mcpSelectMember(ctx context.Context, s *mcp.Session, group, member string) (any, error) {
	if r, err := a.findRoute(group); err == nil {
		group = routes.Group(r)
	}
	member = a.sourceName(member)
	err := a.change(ctx, s, words{"切换路由组的成员", "Choose a route group's member"}, group+" → "+member, func() error { return a.selectMember(group, member) })
	if err != nil {
		return nil, err
	}
	return fmt.Sprintf("%s now uses %s.", group, member), nil
}
