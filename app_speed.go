package main

import (
	"context"
	"errors"
	"maps"
	"net"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/oboo/moho/internal/gateway"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/ping"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
	"github.com/oboo/moho/internal/tun"
)

const (
	pingTimeout = 3 * time.Second
	siteTimeout = 8 * time.Second
	pingWorkers = 16
	// speedAge is how long a ping or a site's time stands before it's
	// measured again by itself.
	speedAge = 10 * time.Minute
)

// pingResult is a node's last ping; ms is 0 when there's no answer, which
// says little: plenty of servers ignore ICMP.
type pingResult struct {
	ms int
	at time.Time
}

// siteResult is a route's other targets as timed through node.
type siteResult struct {
	node  string
	sites []SiteDelay
	at    time.Time
}

// keepMeasured measures, in the background, what has no recent result:
// the ping of every node, and each route's targets.
func (a *App) keepMeasured() {
	now := time.Now()
	var nodes, sites, mains []string
	a.mu.Lock()
	for _, ns := range a.provNodes {
		for _, n := range ns {
			if now.Sub(a.pings[n].at) > speedAge {
				nodes = append(nodes, n)
			}
		}
	}
	for _, r := range a.routes() {
		if !r.NodeGroups() {
			continue
		}
		if now.Sub(a.routeAt[r.ID]) > speedAge {
			mains = append(mains, r.ID) // the default route's are every node's delay
		}
		n, res := currentNode(a.proxies, r), a.sites[r.ID]
		if !r.Direct && providerOf(n) != "" && (res.node != n || now.Sub(res.at) > speedAge) {
			sites = append(sites, r.ID)
		}
	}
	a.mu.Unlock()
	if len(nodes) > 0 {
		go a.pingNodes(nodes)
	}
	if len(mains) > 0 {
		go a.testMains(mains)
	}
	for _, id := range sites {
		go a.testSites(id)
	}
}

// markTested notes a route's main target as just tested. a.mu is held.
func (a *App) markTested(routeID string) {
	if a.routeAt == nil {
		a.routeAt = map[string]time.Time{}
	}
	a.routeAt[routeID] = time.Now()
}

// testMains tests every node on the main targets of the routes, one route
// at a time: a route is a request through each node. One run at a time; a
// run asked for meanwhile is dropped.
func (a *App) testMains(ids []string) {
	if !a.mainBusy.CompareAndSwap(false, true) {
		return
	}
	defer a.mainBusy.Store(false)
	c, err := a.core.Client()
	if err != nil {
		return
	}
	s := a.settings.Get()
	for _, id := range ids {
		a.mu.Lock()
		a.markTested(id) // before, so that a refresh meanwhile doesn't ask again
		a.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_, _ = c.GroupDelay(ctx, routes.Group(a.route(id)), s.TargetsOf(id)[0].URL, 5*time.Second)
		cancel()
		a.mu.Lock()
		a.nodesAt = time.Time{} // the results are in the providers' lists
		a.mu.Unlock()
		a.refresh()
	}
}

// mainURLs is each route's main target; direct routes have none.
func mainURLs(s settings.Settings) map[string]string {
	out := map[string]string{}
	for _, r := range routes.Build(s.Routing, nil) {
		if r.NodeGroups() {
			out[r.ID] = s.TargetsOf(r.ID)[0].URL
		}
	}
	return out
}

// nodeTest is a node's tests of one URL, for the view.
func nodeTest(tests []mihomo.History) NodeTest {
	t := NodeTest{History: make([]int, 0, len(tests))}
	for _, h := range tests[max(0, len(tests)-12):] {
		t.History = append(t.History, h.Delay)
	}
	if len(tests) > 0 {
		t.Delay, t.Tested = tests[len(tests)-1].Delay, true
	}
	return t
}

// siteViews lists targets with their results, if those are of node.
func siteViews(targets []routes.Target, res siteResult, node string) []SiteDelay {
	out := make([]SiteDelay, len(targets))
	for i, t := range targets {
		out[i] = SiteDelay{Name: t.Name, URL: t.URL}
		if res.node != node {
			continue
		}
		if j := slices.IndexFunc(res.sites, func(d SiteDelay) bool { return d.URL == t.URL }); j >= 0 {
			out[i] = res.sites[j]
			out[i].Name = t.Name
		}
	}
	return out
}

func (a *App) allNodes() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var all []string
	for _, ns := range a.provNodes {
		all = append(all, ns...)
	}
	return all
}

// pingNodes measures the way to each node's server, around the core. One
// run at a time; a run asked for meanwhile is dropped.
func (a *App) pingNodes(names []string) {
	if !a.pingBusy.CompareAndSwap(false, true) {
		return
	}
	defer a.pingBusy.Store(false)
	c, err := a.core.Client()
	if err != nil {
		return
	}
	// A TUN would answer in place of the servers, and its DNS with fake
	// addresses: leave through the real interface, with real addresses.
	st := a.settings.Get()
	iface := ""
	tunOn := a.takeoverOn(st) && st.Tun && tun.Installed(a.tunBin) && a.tunFault.Load() == nil
	if tunOn {
		// ping can be bound to an interface on macOS only. Windows leaves
		// it to the routing table, which the TUN now owns, so a TUN there
		// means measuring through the node or not at all: the second, when
		// the TUN drops ICMP, leaves the column empty rather than wrong.
		if iface = gateway.PrimaryInterface(); iface == "" && runtime.GOOS != "windows" {
			return
		}
	}
	servers := map[string]map[string]string{}
	for _, n := range names {
		if id := providerOf(n); servers[id] == nil {
			servers[id] = a.providers.Servers(id)
		}
	}
	results := make([]pingResult, len(names))
	var wg sync.WaitGroup
	sem := make(chan struct{}, pingWorkers)
	for i, n := range names {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if host, ok := servers[providerOf(n)][display(n)]; ok {
				results[i].ms = measurePing(c, host, iface, tunOn)
			}
			results[i].at = time.Now()
		})
	}
	wg.Wait()
	a.mu.Lock()
	next := maps.Clone(a.pings)
	if next == nil {
		next = map[string]pingResult{}
	}
	for i, n := range names {
		next[n] = results[i]
	}
	a.pings = next
	a.mu.Unlock()
	a.emit()
}

func measurePing(c *mihomo.Client, host, iface string, tunOn bool) int {
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	ip, err := resolveServer(ctx, c, host, tunOn)
	if err != nil {
		return 0
	}
	ms, _ := ping.ICMP(ctx, ip, iface, pingTimeout)
	return ms
}

func resolveServer(ctx context.Context, c *mihomo.Client, host string, tunOn bool) (string, error) {
	if net.ParseIP(host) != nil {
		return host, nil
	}
	if tunOn {
		return c.LookupIP(ctx, host)
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return "", err
	}
	return ips[0].String(), nil
}

// testSites times a route's other targets through the node it uses now.
func (a *App) testSites(routeID string) {
	r := a.route(routeID)
	a.mu.Lock()
	node := currentNode(a.proxies, r)
	if a.sitesBusy[routeID] || providerOf(node) == "" {
		a.mu.Unlock()
		return
	}
	if a.sitesBusy == nil {
		a.sitesBusy = map[string]bool{}
	}
	a.sitesBusy[routeID] = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.sitesBusy, routeID)
		a.mu.Unlock()
	}()
	c, err := a.core.Client()
	if err != nil {
		return
	}
	targets := a.settings.Get().TargetsOf(routeID)[1:]
	out := make([]SiteDelay, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), siteTimeout+time.Second)
			defer cancel()
			// A failure is left as 0, which the view shows as a timeout.
			ms, _ := c.NodeDelay(ctx, "p-"+providerOf(node), node, t.URL, siteTimeout)
			out[i] = SiteDelay{Name: t.Name, URL: t.URL, Delay: ms, Tested: true}
		})
	}
	wg.Wait()
	a.mu.Lock()
	next := maps.Clone(a.sites)
	if next == nil {
		next = map[string]siteResult{}
	}
	next[routeID] = siteResult{node, out, time.Now()}
	a.sites = next
	a.mu.Unlock()
	a.emit()
}

// setRouteTargets changes what a route is tested against; nil puts its
// defaults back. A new main target is tried first, through the node the
// route uses, because the automatic group would otherwise find every node
// dead on it and stop choosing.
func (a *App) setRouteTargets(routeID string, list []routes.Target) error {
	r := a.route(routeID)
	if r.ID == "" || !r.NodeGroups() {
		return errors.New(i18n.T("没有这条路由", "No such route"))
	}
	done := a.begin("targets:" + routeID)
	defer done()
	if list != nil {
		if err := routes.ValidTargets(routeID, list); err != nil {
			return err
		}
		if list[0].URL != a.settings.Get().TargetsOf(routeID)[0].URL {
			if err := a.tryTarget(r, list[0].URL); err != nil {
				return err
			}
		}
	}
	err := a.updateSettings(func(s *settings.Settings) {
		if list == nil {
			delete(s.RouteTargets, routeID)
			return
		}
		if s.RouteTargets == nil {
			s.RouteTargets = map[string][]routes.Target{}
		}
		s.RouteTargets[routeID] = slices.Clone(list)
	})
	if err != nil {
		return err
	}
	go func() { _ = a.testRoute(context.Background(), routeID) }()
	return nil
}

// tryTarget requests url through the node route r uses. Without a node
// there is nothing to try it with.
func (a *App) tryTarget(r routes.Route, url string) error {
	a.mu.Lock()
	node := currentNode(a.proxies, r)
	a.mu.Unlock()
	if providerOf(node) == "" {
		return nil
	}
	c, err := a.core.Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), siteTimeout+time.Second)
	defer cancel()
	if _, err := c.NodeDelay(ctx, "p-"+providerOf(node), node, url, siteTimeout); err != nil {
		return errors.New(i18n.T("这个地址经当前节点没有响应，不能设为主目标", "This address didn't answer through the current node, so it can't be the main target"))
	}
	return nil
}
