package bridge

// The route groups at run time, as the desktop has them in app_routing.go,
// and what the 路由组 pages read and change.

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/oboo/moho/internal/geo"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

const siteTimeout = 5 * time.Second

// siteResult is a route's other test sites, timed through node.
type siteResult struct {
	Node  string
	Sites []siteDelay
}

type siteDelay struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Delay  int    `json:"delay"`
	Tested bool   `json:"tested"`
}

func mainURLs(s settings.Settings) map[string]string {
	out := map[string]string{}
	for _, r := range routes.Build(s.Routing, nil) {
		if !r.Direct {
			out[r.ID] = s.TargetsOf(r.ID)[0].URL
		}
	}
	return out
}

// coreSame says whether two settings give the core the same config.
func coreSame(old, next settings.Settings) bool {
	if reflect.DeepEqual(coreView(old.Routing), coreView(next.Routing)) { // a strategy is a selector's position
		next.Routing = old.Routing
	}
	if maps.Equal(mainURLs(old), mainURLs(next)) { // only the main targets are in the config
		next.RouteTargets = old.RouteTargets
	}
	return reflect.DeepEqual(old, next)
}

func coreView(p routes.Plan) routes.Plan {
	p = p.Clone()
	p.ChangePolicy(routes.Default, func(*routes.Policy) {}) // a plan never changed lists no groups yet
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

// fileNodes are the full names of the enabled Providers' nodes, from their
// files, since the config that loads them must already list them.
func (x *bridge) fileNodes() []string {
	var out []string
	for _, p := range x.store.List() {
		if p.Enabled {
			out = append(out, fullNames(x.store.Names(p.ID), p.ID)...)
		}
	}
	slices.Sort(out)
	return out
}

func inScope(q routes.Policy, provider, country string) bool {
	return (len(q.Providers) == 0 || slices.Contains(q.Providers, provider)) &&
		(len(q.Regions) == 0 || slices.Contains(q.Regions, country))
}

// nodeLists are the node lists the config needs for the groups' strategies.
func (x *bridge) nodeLists(all []routes.Route) (scopes, orders map[string][]string) {
	scopes, orders = map[string][]string{}, map[string][]string{}
	names := x.fileNodes()
	for _, r := range all {
		if r.Direct {
			continue
		}
		q := r.Policy
		if q.Scoped() {
			list := []string{}
			for _, n := range names {
				if inScope(q, providerOf(n), placeOf(n).Country) {
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

// followPolicies puts every group's selector where its strategy says.
func (x *bridge) followPolicies(ctx context.Context, c *mihomo.Client) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	px, err := c.Proxies(ctx)
	if err != nil {
		return err
	}
	var first error
	for _, r := range x.routes() {
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
}

// ---- what the pages read ----

type ruleData struct {
	Text     string `json:"text"`
	Kind     string `json:"kind"`
	KindName string `json:"kindName"`
	Value    string `json:"value"`
	Shown    string `json:"shown"`
	Builtin  bool   `json:"builtin"`
	Off      bool   `json:"off"`
}

type serviceData struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Builtin bool       `json:"builtin"`
	Mine    bool       `json:"mine"`
	Off     bool       `json:"off"`
	Changed bool       `json:"changed"`
	Rules   []ruleData `json:"rules"`
}

type groupData struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Summary   string          `json:"summary"`
	Direct    bool            `json:"direct"`
	Reject    bool            `json:"reject"`
	Custom    bool            `json:"custom"`
	Mode      string          `json:"mode"`
	Regions   []string        `json:"regions"`
	Providers []string        `json:"providers"`
	Order     []string        `json:"order"`
	Fixed     string          `json:"fixed"`
	Now       string          `json:"now"`
	Services  []serviceData   `json:"services"`
	Targets   []routes.Target `json:"targets"`
	Sites     []siteDelay     `json:"sites"`
}

type pickData struct {
	Name       string `json:"name"`
	Display    string `json:"display"`
	Provider   string `json:"provider"`
	ProviderID string `json:"providerId"`
	Country    string `json:"country"`
	Flag       string `json:"flag"`
	Delay      int    `json:"delay"` // -1 untested, 0 timed out
}

type regionData struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type providerPick struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type routingData struct {
	Groups    []groupData    `json:"groups"`
	Nodes     []pickData     `json:"nodes"`
	Regions   []regionData   `json:"regions"`
	Providers []providerPick `json:"providers"`
	Kinds     []kindData     `json:"kinds"`
}

type kindData struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (x *bridge) routing() routingData {
	s := x.set.Get()
	all := x.routes()
	x.mu.Lock()
	px, sites := x.proxies, x.sites
	x.mu.Unlock()
	out := routingData{Groups: []groupData{}, Nodes: []pickData{}, Regions: []regionData{}, Providers: []providerPick{}, Kinds: []kindData{}}
	for _, k := range routes.Kinds {
		out.Kinds = append(out.Kinds, kindData{k, routes.KindName(k)})
	}
	count := map[string]int{}
	for _, p := range x.store.List() {
		if !p.Enabled {
			continue
		}
		out.Providers = append(out.Providers, providerPick{p.ID, p.Name})
		for _, n := range fullNames(x.store.Names(p.ID), p.ID) {
			pl := placeOf(n)
			d := -1
			if h := px[n].History; len(h) > 0 {
				d = h[len(h)-1].Delay
			}
			out.Nodes = append(out.Nodes, pickData{n, display(n), p.Name, p.ID, pl.Country, strings.ToLower(pl.Country), d})
			if pl.Country != "" {
				count[pl.Country]++
			}
		}
	}
	for code, n := range count {
		name := code
		if p, ok := geo.Country(code); ok {
			name = geo.Display(p.City)
		}
		out.Regions = append(out.Regions, regionData{code, name, n})
	}
	slices.SortFunc(out.Regions, func(a, b regionData) int { return b.Count - a.Count })
	for _, r := range all {
		g := groupData{ID: r.ID, Name: routes.Title(r), Summary: routes.Describe(r), Direct: r.Direct, Reject: r.Reject, Custom: r.Custom,
			Mode: r.Policy.Mode, Regions: nonNil(r.Policy.Regions), Providers: nonNil(r.Policy.Providers), Order: nonNil(r.Policy.Order), Fixed: r.Policy.Fixed,
			Services: []serviceData{}, Targets: []routes.Target{}, Sites: []siteDelay{}}
		if !r.Direct && !r.Reject {
			g.Now = display(currentNode(px, r))
			g.Targets = s.TargetsOf(r.ID)
			g.Sites = append(g.Sites, sites[r.ID].Sites...)
		}
		for _, sv := range r.Services {
			d := serviceData{ID: sv.ID, Name: routes.ServiceTitle(sv), Builtin: sv.Builtin, Mine: sv.Mine, Off: sv.Off, Changed: sv.Changed, Rules: []ruleData{}}
			for _, l := range sv.Rules {
				d.Rules = append(d.Rules, ruleData{Text: l.Text, Kind: l.Kind, KindName: routes.KindName(l.Kind), Value: l.Value, Shown: l.Shown(), Builtin: l.Builtin, Off: l.Off})
			}
			g.Services = append(g.Services, d)
		}
		out.Groups = append(out.Groups, g)
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// explainData says where a host goes, for 测试一个地址.
type explainData struct {
	Error     string `json:"error,omitempty"`
	Route     string `json:"route,omitempty"`
	Name      string `json:"name,omitempty"`
	Rule      string `json:"rule,omitempty"`
	Service   string `json:"service,omitempty"`
	ServiceID string `json:"serviceId,omitempty"`
	Exit      string `json:"exit,omitempty"`
	Note      string `json:"note,omitempty"`
}

func (x *bridge) explain(input string) explainData {
	host := strings.TrimSpace(input)
	if host == "" {
		return explainData{}
	}
	if t, err := routes.ParseRule(routes.KindDomain, host, ""); err == nil {
		host = t.Value
	} else if t, err := routes.ParseRule(routes.KindIP, host, ""); err == nil {
		host, _, _ = strings.Cut(t.Value, "/")
	} else {
		return explainData{Error: err.Error()}
	}
	s := x.set.Get()
	all := x.routes()
	id, rule, _, sure := routes.Explain(host, routes.Active(all, s.Rules), all)
	if !sure {
		for _, c := range slices.Backward(x.realConns()) {
			if (c.Host == host || c.IP == host) && c.Route != "" {
				id, rule = c.Route, c.Rule
				break
			}
		}
	}
	if id == "" {
		return explainData{Note: i18n.T("没有命中域名规则：国内网站和局域网按 GeoSite / GeoIP 走「直连」，其余走「默认」。", "No domain rule matches it: mainland sites and the LAN go Direct by GeoSite / GeoIP, the rest by Default.")}
	}
	r, err := x.route(id)
	if err != nil {
		return explainData{Rule: rule}
	}
	out := explainData{Route: id, Name: routes.Title(r), Rule: rule}
	if sv, ok := routes.FindRule(all, rule); ok {
		out.Service, out.ServiceID = routes.ServiceTitle(sv), sv.ID
	}
	x.mu.Lock()
	out.Exit = display(currentNode(x.proxies, r))
	x.mu.Unlock()
	return out
}

// todayShare counts what the groups carried since the last tick, and says
// each route's part of today's traffic.
func (x *bridge) todayShare(groupBytes map[string]int64) map[string]float64 {
	day := time.Now().Format("2006-01-02")
	all := x.routes()
	x.mu.Lock()
	defer x.mu.Unlock()
	days := x.trafficDays()
	if x.shareDay != day {
		// Picks up where the app left off when it restarts during the day.
		x.shareDay, x.routeBytes = day, days.day(day)
	}
	for g, v := range groupBytes {
		if d := v - x.groupSeen[g]; d > 0 {
			if id := routeOfGroup(all, g); id != "" {
				x.routeBytes[id] += d
				days.add(day, id, d)
			}
		}
	}
	x.groupSeen = groupBytes
	days.saveIfDue(time.Now())
	var total int64
	for _, v := range x.routeBytes {
		total += v
	}
	if total == 0 {
		return nil
	}
	out := make(map[string]float64, len(x.routeBytes))
	for id, v := range x.routeBytes {
		out[id] = float64(v) / float64(total)
	}
	return out
}

// routeOfGroup maps a group the core names to the route it belongs to.
func routeOfGroup(all []routes.Route, group string) string {
	for _, r := range all {
		if !r.Direct && (group == routes.Group(r) || routes.Internal(r, group)) {
			return r.ID
		}
	}
	if group == "DIRECT" {
		for _, r := range all {
			if r.Direct && !r.Reject {
				return r.ID
			}
		}
	}
	return ""
}

// ruleHome is where in the route groups the rule a connection matched lives.
type ruleHome struct {
	Group   string `json:"group,omitempty"`
	Service string `json:"service,omitempty"`
}

func (x *bridge) ruleHome(ruleType, payload string) ruleHome {
	sv, ok := routes.FindMatched(x.routes(), ruleType, payload)
	if !ok {
		return ruleHome{}
	}
	return ruleHome{sv.Group, sv.ID}
}

// parsedRule is what a rule being typed would become.
type parsedRule struct {
	Error    string `json:"error,omitempty"`
	KindName string `json:"kindName,omitempty"`
	Value    string `json:"value,omitempty"`
}

func parseRule(kind, text string) parsedRule {
	r, err := routes.ParseRule(kind, text, "")
	if err != nil {
		return parsedRule{Error: err.Error()}
	}
	return parsedRule{KindName: routes.KindName(r.Kind), Value: r.Value}
}

// ---- what the pages change ----

func (x *bridge) routeOp(ctx context.Context, a callArgs) error {
	switch a.Op {
	case "setStrategy":
		x.mu.Lock()
		r, err := x.routeLocked(a.ID)
		cur := currentNode(x.proxies, r)
		x.mu.Unlock()
		if err != nil {
			return err
		}
		return x.change(ctx, func(s *settings.Settings) { s.Routing.SetStrategy(a.ID, a.Mode, cur) })
	case "setScope":
		return x.change(ctx, func(s *settings.Settings) {
			s.Routing.ChangePolicy(a.ID, func(q *routes.Policy) { q.Regions, q.Providers = a.Regions, a.Providers })
		})
	case "setOrder":
		return x.change(ctx, func(s *settings.Settings) {
			s.Routing.ChangePolicy(a.ID, func(q *routes.Policy) { q.Order = a.Order })
		})
	case "setFixed":
		return x.change(ctx, func(s *settings.Settings) { s.Routing.PickNode(a.ID, a.Node) })
	case "newGroup":
		if msg := routes.GroupNameError(x.routes(), "", a.Name); msg != "" {
			return errors.New(msg)
		}
		return x.change(ctx, func(s *settings.Settings) { s.Routing.AddGroup(s.Rules, a.Name) })
	case "renameGroup":
		if msg := routes.GroupNameError(x.routes(), a.ID, a.Name); msg != "" {
			return errors.New(msg)
		}
		return x.change(ctx, func(s *settings.Settings) { s.Routing.RenameGroup(a.ID, a.Name) })
	case "deleteGroup":
		return x.change(ctx, func(s *settings.Settings) {
			s.Routing.DeleteGroup(s.Rules, a.ID, a.To)
			delete(s.RouteTargets, a.ID)
		})
	case "newService":
		if msg := routes.ServiceNameError(x.routes(), "", a.Name); msg != "" {
			return errors.New(msg)
		}
		return x.change(ctx, func(s *settings.Settings) { s.Routing.AddService(s.Rules, a.ID, a.Name) })
	case "renameService":
		if msg := routes.ServiceNameError(x.routes(), a.ID, a.Name); msg != "" {
			return errors.New(msg)
		}
		return x.change(ctx, func(s *settings.Settings) { s.Routing.RenameService(a.ID, a.Name) })
	case "toggleService":
		return x.change(ctx, func(s *settings.Settings) { s.Routing.ToggleService(a.ID) })
	case "toggleRule":
		return x.change(ctx, func(s *settings.Settings) { s.Routing.ToggleRule(a.ID, a.Text) })
	case "moveService":
		return x.change(ctx, func(s *settings.Settings) { s.Routing.MoveService(s.Rules, a.ID, a.To) })
	case "splitService":
		return x.change(ctx, func(s *settings.Settings) { s.Routing.SplitService(s.Rules, a.ID) })
	case "removeService":
		return x.change(ctx, func(s *settings.Settings) { s.Rules = s.Routing.RemoveService(s.Rules, a.ID) })
	case "addRule", "moveRule":
		r, err := routes.ParseRule(a.Kind, a.Text, a.Route)
		if err != nil {
			return err
		}
		all := x.routes()
		if a.Service != "" && a.Service != routes.MineID(a.Route) {
			r.Service = a.Service
		}
		if from, ok := routes.FindRule(all, r.Mihomo()); ok && a.Op == "addRule" {
			if from.ID == cmpService(a.Service, a.Route) {
				return errors.New(i18n.T("这条规则已经在这个服务里了", "That rule is already in this service"))
			}
			return &conflictError{from: routes.ServiceTitle(from)}
		}
		return x.setRule(ctx, r, false)
	case "removeRule":
		r, err := routes.ParseRule(a.Kind, a.Text, a.Route)
		if err != nil {
			return err
		}
		return x.setRule(ctx, r, true)
	}
	return errors.New("unknown route operation " + a.Op)
}

func cmpService(service, route string) string {
	if service != "" {
		return service
	}
	return routes.MineID(route)
}

// conflictError says a rule already lives in another service. The page
// offers to move it, on the code before the colon.
type conflictError struct{ from string }

func (e *conflictError) Error() string { return "conflict:" + e.from }

func (x *bridge) routeLocked(id string) (routes.Route, error) {
	if i := slices.IndexFunc(x.routesNow, func(r routes.Route) bool { return r.ID == id }); i >= 0 {
		return x.routesNow[i], nil
	}
	return routes.Route{}, errors.New(i18n.T("没有这条路由", "No such route"))
}

// setRule puts r in the rules, or takes it out, and closes the connections
// it matches that go elsewhere, so that the change shows.
func (x *bridge) setRule(ctx context.Context, r routes.UserRule, removed bool) error {
	err := x.change(ctx, func(s *settings.Settings) {
		if removed {
			s.Rules = routes.Remove(s.Rules, r)
			return
		}
		s.Routing.Claim(s.Rules, r)
		s.Rules = routes.Upsert(s.Rules, r)
	})
	if err != nil {
		return err
	}
	keep := r.Route
	if removed {
		keep = ""
	}
	x.reroute(ctx, r, keep)
	return nil
}

func (x *bridge) reroute(ctx context.Context, r routes.UserRule, keep string) {
	c, err := x.core()
	if err != nil {
		return
	}
	snap, err := c.Connections(ctx)
	if err != nil {
		return
	}
	byGroup := map[string]string{}
	for _, g := range x.routes() {
		byGroup[routes.Group(g)] = g.ID
	}
	for _, cn := range snap.Connections {
		host := cn.Metadata.Host
		if host == "" {
			host = cn.Metadata.SniffHost
		}
		if !r.Matches(host, cn.Metadata.DestinationIP) {
			continue
		}
		group := ""
		if len(cn.Chains) > 0 {
			group = cn.Chains[len(cn.Chains)-1]
		}
		if keep == "" || byGroup[group] != keep {
			_ = c.CloseConnection(ctx, cn.ID)
		}
	}
	x.mon.Wake()
}

// setTargets changes what a route is tested against; nil puts its defaults
// back. A new main target is tried first through the route's node, because
// the automatic group would otherwise find every node dead on it.
func (x *bridge) setTargets(ctx context.Context, routeID string, list []routes.Target) error {
	r, err := x.route(routeID)
	if err != nil {
		return err
	}
	if r.Direct {
		return errors.New(i18n.T("没有这条路由", "No such route"))
	}
	if list != nil {
		list = slices.Clone(list)
		for i, t := range list { // one the page only has an address for
			if t.Name == "" {
				parsed, err := routes.ParseTarget(t.URL)
				if err != nil {
					return err
				}
				list[i] = parsed
			}
		}
		if err := routes.ValidTargets(routeID, list); err != nil {
			return err
		}
		if c, _ := x.core(); c != nil && list[0].URL != x.set.Get().TargetsOf(routeID)[0].URL {
			x.mu.Lock()
			node := currentNode(x.proxies, r)
			x.mu.Unlock()
			if p := providerOf(node); p != "" {
				if _, err := c.NodeDelay(ctx, "p-"+p, node, list[0].URL, siteTimeout); err != nil {
					return err
				}
			}
		}
	}
	return x.change(ctx, func(s *settings.Settings) {
		if list == nil {
			delete(s.RouteTargets, routeID)
			return
		}
		if s.RouteTargets == nil {
			s.RouteTargets = map[string][]routes.Target{}
		}
		s.RouteTargets[routeID] = slices.Clone(list)
	})
}

// testSites times a route's other targets through the node it uses now.
func (x *bridge) testSites(routeID string) error {
	c, err := x.core()
	if err != nil {
		return err
	}
	r, err := x.route(routeID)
	if err != nil {
		return err
	}
	x.mu.Lock()
	node := currentNode(x.proxies, r)
	x.mu.Unlock()
	if providerOf(node) == "" {
		return errors.New(i18n.T("这条路由现在没有节点", "This route has no node now"))
	}
	targets := x.set.Get().TargetsOf(routeID)[1:]
	out := make([]siteDelay, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), siteTimeout+time.Second)
			defer cancel()
			ms, _ := c.NodeDelay(ctx, "p-"+providerOf(node), node, t.URL, siteTimeout) // a failure stays 0, a timeout
			out[i] = siteDelay{Name: t.Name, URL: t.URL, Delay: ms, Tested: true}
		})
	}
	wg.Wait()
	x.mu.Lock()
	next := maps.Clone(x.sites)
	if next == nil {
		next = map[string]siteResult{}
	}
	next[routeID] = siteResult{node, out}
	x.sites = next
	x.mu.Unlock()
	return nil
}
