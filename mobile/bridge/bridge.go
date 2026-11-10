// Package bridge is the app side of bowel on mobile: what the Flutter interface
// calls through the native host, and the view values it draws.
//
// It reuses bowel's own providers, routes, config builder, controller client
// and monitor. mihomo itself lives in the tunnel package, in the extension.
package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/geo"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/locate"
	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/monitor"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
	"gopkg.in/yaml.v3"
)

// Sink receives what Go pushes to the interface: "tick", "conns", "view".
type Sink interface {
	Emit(name, payload string)
}

const socketName = "c.sock"

// tunFDFile holds the number of the running core's TUN descriptor.
const tunFDFile = "tun.fd"

const probePort = 17892

type bridge struct {
	dir   string
	sink  Sink
	store *provider.Store
	set   *settings.Store
	mon   *monitor.Monitor
	loc   *locate.Locator

	locating atomic.Bool

	setMu sync.Mutex // one change of the config at a time

	mu         sync.Mutex
	routesNow  []routes.Route
	routesAt   uint64
	vpn        string // off, connecting, on, disconnecting, error
	vpnErr     string
	version    string // the core's, once it has said
	wantConns  bool
	client     *mihomo.Client
	cancel     context.CancelFunc
	proxies    map[string]mihomo.Proxy
	provNodes  map[string][]string
	lastNode   map[string]string    // route id → the node it was last on
	probedAt   map[string]time.Time // when each node was last probed
	youProbed  time.Time            // when the phone's own place was last asked for
	traffic    *routeTraffic        // per-route bytes by day
	rangesSent time.Time            // when the 7 and 30 day totals last went out
	switched   *switchNote
	sites      map[string]siteResult
	updating   bool // the automatic Provider update is running

	shareDay   string           // the day routeBytes counts
	routeBytes map[string]int64 // today's bytes per route id
	groupSeen  map[string]int64 // each group's bytes at the last tick
	pushNanos  int64            // the last tick's work in Go
	pushBytes  int
}

// switchNote is a route's automatic group having moved off a failed node.
type switchNote struct {
	route, from, to string
	at              time.Time
}

var (
	bMu sync.Mutex
	b   *bridge
)

// Start opens the App Group folder and begins pushing a tick every second.
func Start(dir string, sink Sink) error {
	bMu.Lock()
	defer bMu.Unlock()
	if b != nil {
		return nil
	}
	store, err := provider.Open(filepath.Join(dir, "providers"))
	if err != nil {
		return err
	}
	set, err := openSettings(dir)
	if err != nil {
		return err
	}
	i18n.Set(set.Get().Language)
	nb := &bridge{
		dir: dir, sink: sink, store: store, set: set,
		mon: monitor.New(filepath.Join(dir, "stats.json")), vpn: "off", lastNode: map[string]string{},
	}
	nb.mon.SkipGroup = core.ProbeGroup
	nb.loc = locate.New(filepath.Join(dir, "locations.json"))
	nb.loc.Login = nb.probeLogin
	b = nb
	go nb.loop()
	return nil
}

// SetVPNStatus is how the host reports NEVPNStatus changes.
func SetVPNStatus(status, errText string) {
	bMu.Lock()
	x := b
	bMu.Unlock()
	if x == nil {
		return
	}
	x.setVPN(status, errText)
}

func (x *bridge) setVPN(status, errText string) {
	x.mu.Lock()
	was := x.vpn
	x.vpn, x.vpnErr = status, errText
	var start, stop bool
	if status == "on" && was != "on" {
		start = true
	}
	if status != "on" && x.cancel != nil {
		stop = true
	}
	x.mu.Unlock()
	if stop {
		x.disconnect()
	}
	if start {
		x.connect()
	}
	x.push()
}

func (x *bridge) connect() {
	c := mihomo.NewUnix(filepath.Join(x.dir, socketName))
	ctx, cancel := context.WithCancel(context.Background())
	x.mu.Lock()
	x.client, x.cancel = c, cancel
	x.mu.Unlock()
	go x.mon.Run(ctx, c)
	go func() {
		// The core answers a moment after the tunnel reports connected.
		for range 20 {
			if ctx.Err() != nil {
				return
			}
			if x.refresh(ctx) == nil {
				x.push()
				x.measure(ctx, c)
				return
			}
			time.Sleep(300 * time.Millisecond)
		}
	}()
}

// measure tests every node once. The auto groups are lazy and start on the
// first node, which in many subscriptions is a fake one saying how much
// traffic is left, with no place and no flag.
func (x *bridge) measure(ctx context.Context, c *mihomo.Client) {
	for _, p := range x.store.List() {
		if p.Enabled {
			_ = c.HealthCheck(ctx, "p-"+p.ID)
		}
	}
	if x.refresh(ctx) == nil {
		x.push()
	}
	x.locateExits()
}

func (x *bridge) disconnect() {
	x.mu.Lock()
	cancel := x.cancel
	x.client, x.cancel, x.proxies, x.provNodes = nil, nil, nil, nil
	x.lastNode, x.switched = map[string]string{}, nil
	x.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (x *bridge) refresh(ctx context.Context) error {
	x.mu.Lock()
	c := x.client
	x.mu.Unlock()
	if c == nil {
		return fmt.Errorf("not connected")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	px, err := c.Proxies(ctx)
	if err != nil {
		return err
	}
	pv, err := c.ProxyProviders(ctx)
	if err != nil {
		return err
	}
	nodes := map[string][]string{}
	for name, p := range pv {
		id, ok := strings.CutPrefix(name, "p-")
		if !ok {
			continue
		}
		for _, n := range p.Proxies {
			nodes[id] = append(nodes[id], n.Name)
			// /proxies has the groups only: a node and its tests are here.
			px[n.Name] = n
		}
	}
	version := ""
	x.mu.Lock()
	known := x.version != ""
	x.mu.Unlock()
	if !known {
		if v, err := c.Version(ctx); err == nil {
			version = v.Version
		}
	}
	all := x.routes()
	x.mu.Lock()
	x.proxies, x.provNodes = px, nodes
	if version != "" {
		x.version = version
	}
	x.noticeSwitchesLocked(all)
	x.mu.Unlock()
	return nil
}

// noticeSwitchesLocked records a route's automatic group moving off a failed
// node, which the 代理 page says for a while.
func (x *bridge) noticeSwitchesLocked(all []routes.Route) {
	for _, r := range all {
		if r.Direct {
			continue
		}
		n := currentNode(x.proxies, r)
		prev := x.lastNode[r.ID]
		x.lastNode[r.ID] = n
		if now := x.proxies[routes.Group(r)].Now; prev == "" || prev == n || (now != routes.Auto(r) && now != routes.Backup(r)) {
			continue
		}
		if h := x.proxies[prev].History; len(h) > 0 && h[len(h)-1].Delay == 0 {
			x.switched = &switchNote{route: r.ID, from: display(prev), to: display(n), at: time.Now()}
		}
	}
}

// loop is the one-second heartbeat of the interface.
func (x *bridge) loop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	n := 0
	for range t.C {
		n++
		x.tick()
		if n%5 == 0 {
			x.mu.Lock()
			real := x.client != nil
			x.mu.Unlock()
			if real && x.refresh(context.Background()) == nil {
				x.push()
				go x.locateExits()
			}
		}
		if n%60 == 1 {
			go x.autoUpdate()
			go x.locateYou()
		}
	}
}

func (x *bridge) tick() {
	began := time.Now()
	x.mu.Lock()
	want := x.wantConns
	x.mu.Unlock()

	s := x.mon.Now()
	tk := tickData{Down: s.Down, Up: s.Up, Active: s.Active, TodayDown: s.TodayDown, TodayUp: s.TodayUp, MonthDown: s.MonthDown, ByRoute: map[string]int64{}}
	for _, r := range x.routes() {
		tk.ByRoute[r.ID] = s.ByGroup[routes.Group(r)]
	}
	tk.Today = x.todayShare(s.GroupBytes)
	tk.Ranges = x.trafficRanges(began)
	var conns []connData
	if want {
		conns = x.realConns()
	}
	x.mu.Lock()
	tk.GoNanos, tk.PrevBytes = x.pushNanos, x.pushBytes
	x.mu.Unlock()
	tk.SentMs = time.Now().UnixMilli()
	tick, _ := json.Marshal(tk)
	x.sink.Emit("tick", string(tick))
	size := len(tick)
	if want {
		data, _ := json.Marshal(conns)
		x.sink.Emit("conns", string(data))
		size += len(data)
	}
	x.mu.Lock()
	x.pushNanos, x.pushBytes = time.Since(began).Nanoseconds(), size
	x.mu.Unlock()
}

func (x *bridge) realConns() []connData {
	list := x.mon.Conns()
	out := make([]connData, 0, len(list))
	byGroup := map[string]string{}
	for _, r := range x.routes() {
		byGroup[routes.Group(r)] = r.ID
	}
	for _, c := range list {
		cv := connData{ID: c.ID, Host: c.Host, Status: c.Status, DownRate: c.DownRate, UpRate: c.UpRate, Start: c.Start.UnixMilli(),
			Rule: ruleText(c.Rule, c.Payload), RuleType: c.Rule, Payload: c.Payload, IP: c.IP, Port: c.Port, Net: strings.ToUpper(c.Network), Down: c.Down, Up: c.Up, Err: c.Error}
		if !c.End.IsZero() {
			cv.End = c.End.UnixMilli()
		}
		if n := len(c.Chains); n > 0 {
			cv.Route = byGroup[c.Chains[n-1]]
			cv.Node = display(c.Chains[0])
			if c.Chains[n-1] == monitor.Reject { // refused by a rule: no node was used
				cv.Route, cv.Node = routes.Reject, ""
			}
		}
		out = append(out, cv)
	}
	return out
}

func (x *bridge) push() {
	v := x.view()
	data, _ := json.Marshal(v)
	x.sink.Emit("view", string(data))
}

// Call runs one request from the interface. payload and the result are JSON.
func Call(method, payload string) (string, error) {
	if method == "ping" {
		// The latency probe does nothing else, on purpose.
		return payload, nil
	}
	bMu.Lock()
	x := b
	bMu.Unlock()
	if x == nil {
		return "", fmt.Errorf("bridge not started")
	}
	var args callArgs
	if payload != "" {
		if err := json.Unmarshal([]byte(payload), &args); err != nil {
			return "", err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch method {
	case "view":
		return encode(x.view())
	case "conns":
		x.mu.Lock()
		x.wantConns = args.On
		x.mu.Unlock()
		return "null", nil
	case "setMode":
		return "null", x.setMode(ctx, args.Mode)
	case "select":
		return "null", x.selectNode(ctx, args.Route, args.Node)
	case "addProvider":
		return "null", x.addProvider(ctx, args.Input)
	case "prepareTunnel":
		return "null", x.prepareTunnel()
	case "addFile":
		return "null", x.addFile(ctx, args.Path)
	case "updateProvider":
		return "null", x.updateProvider(ctx, args.ID)
	case "updateAll":
		return encode(x.updateAll())
	case "renameProvider":
		return "null", x.renameProvider(args.ID, args.Name)
	case "deleteProvider":
		return "null", x.deleteProvider(ctx, args.ID)
	case "setProviderEnabled":
		return "null", x.setProviderEnabled(ctx, args.ID, args.On)
	case "setAutoUpdate":
		return "null", x.setAutoUpdate(args.ID, args.Hours)
	case "shareProvider":
		return encode(x.shareProvider(args.ID))
	case "decodeQR":
		return encode(decodeQR(args.Path))
	case "testRoute":
		return "null", x.testRoute(ctx, args.Route)
	case "testProvider":
		return "null", x.testProvider(ctx, args.ID)
	case "closeConn":
		return "null", x.closeConn(ctx, args.ID)
	case "closeConns":
		return "null", x.closeConns(ctx, args.IDs)
	case "addRule":
		return "null", x.addRule(ctx, args.Host, args.Route)
	case "logs":
		return encode(x.logs())
	case "routing":
		return encode(x.routing())
	case "routeOp":
		return "null", x.routeOp(ctx, args)
	case "explain":
		return encode(x.explain(args.Host))
	case "parseRule":
		return encode(parseRule(args.Kind, args.Text))
	case "ruleHome":
		return encode(x.ruleHome(args.Kind, args.Text))
	case "setTargets":
		return "null", x.setTargets(ctx, args.Route, args.Targets)
	case "testSites":
		return "null", x.testSites(args.Route)
	case "settings":
		return encode(x.settingsData())
	case "setLanguage":
		return "null", x.setLanguage(args.Name)
	case "setSettings":
		return "null", x.setSettings(ctx, args.Patch)
	case "resetCore":
		return "null", x.resetCore(ctx)
	case "flushDNS":
		return "null", x.flushDNS(ctx)
	case "updateGeo":
		return "null", x.updateGeo()
	}
	return "", fmt.Errorf("unknown method %q", method)
}

type callArgs struct {
	Mode      string          `json:"mode"`
	Route     string          `json:"route"`
	Node      string          `json:"node"`
	Input     string          `json:"input"`
	On        bool            `json:"on"`
	ID        string          `json:"id"`
	IDs       []string        `json:"ids"`
	Name      string          `json:"name"`
	Path      string          `json:"path"`
	Host      string          `json:"host"`
	Hours     int             `json:"hours"`
	Op        string          `json:"op"`
	To        string          `json:"to"`
	Text      string          `json:"text"`
	Kind      string          `json:"kind"`
	Service   string          `json:"service"`
	Regions   []string        `json:"regions"`
	Providers []string        `json:"providers"`
	Order     []string        `json:"order"`
	Targets   []routes.Target `json:"targets"`
	Patch     json.RawMessage `json:"patch"`
}

func encode(v any) (string, error) {
	data, err := json.Marshal(v)
	return string(data), err
}

func (x *bridge) setMode(ctx context.Context, mode string) error {
	x.setMu.Lock()
	defer x.setMu.Unlock()
	if _, err := x.set.Update(func(s *settings.Settings) { s.Mode = mode }); err != nil {
		return err
	}
	if c, _ := x.core(); c != nil {
		if err := c.Patch(ctx, map[string]any{"mode": mode}); err != nil {
			return err
		}
	}
	x.push()
	return nil
}

// routes are the route groups as the user has them now, built once per change.
func (x *bridge) routes() []routes.Route {
	gen := x.set.Gen()
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.routesNow == nil || x.routesAt != gen {
		s := x.set.Get()
		x.routesNow, x.routesAt = routes.Build(s.Routing, s.Rules), gen
	}
	return x.routesNow
}

func (x *bridge) route(id string) (routes.Route, error) {
	all := x.routes()
	if i := slices.IndexFunc(all, func(r routes.Route) bool { return r.ID == id }); i >= 0 {
		return all[i], nil
	}
	return routes.Route{}, errors.New(i18n.T("没有这条路由", "No such route"))
}

// selectNode makes a group's strategy that one node, or the fastest for "".
func (x *bridge) selectNode(ctx context.Context, routeID, node string) error {
	if _, err := x.route(routeID); err != nil {
		return err
	}
	return x.change(ctx, func(s *settings.Settings) { s.Routing.PickNode(routeID, node) })
}

// change edits the settings with fn, as one step: checked, saved, and made
// the core's. If the core refuses the result, the old settings come back.
func (x *bridge) change(ctx context.Context, fn func(*settings.Settings)) error {
	x.setMu.Lock()
	defer x.setMu.Unlock()
	old := x.set.Get()
	next := old.Clone()
	fn(&next)
	if err := next.Validate(); err != nil {
		return err
	}
	if _, err := x.set.Update(func(s *settings.Settings) { *s = next }); err != nil {
		return err
	}
	if c, _ := x.core(); c != nil {
		if !coreSame(old, next) {
			if err := x.reload(ctx, c); err != nil {
				_, _ = x.set.Update(func(s *settings.Settings) { *s = old })
				_ = x.writeConfig()
				x.push()
				return err
			}
		}
		_ = x.followPolicies(ctx, c)
		_ = x.refresh(ctx)
	}
	x.push()
	return nil
}

// applyConfig makes the core use the Providers as they are now.
func (x *bridge) applyConfig(ctx context.Context) error {
	x.setMu.Lock()
	defer x.setMu.Unlock()
	if c, _ := x.core(); c != nil {
		if err := x.reload(ctx, c); err != nil {
			x.push()
			return err
		}
		_ = x.followPolicies(ctx, c)
		_ = x.refresh(ctx)
	}
	x.push()
	return nil
}

// autoUpdate refreshes the subscriptions that are due. The app only runs
// while it is open, so what fell due meanwhile is done on the next opening.
func (x *bridge) autoUpdate() {
	x.mu.Lock()
	busy := x.updating
	x.updating = true
	x.mu.Unlock()
	if busy {
		return
	}
	defer func() {
		x.mu.Lock()
		x.updating = false
		x.mu.Unlock()
	}()
	ids := x.store.Due(time.Now())
	if len(ids) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, id := range ids {
		_, _ = x.store.Update(ctx, http.DefaultClient, id)
	}
	_ = x.applyConfig(ctx)
}

func (x *bridge) addProvider(ctx context.Context, input string) error {
	if _, err := x.store.Add(ctx, http.DefaultClient, input); err != nil {
		return err
	}
	return x.applyConfig(ctx)
}

// prepareTunnel writes the config the extension or service starts from.
func (x *bridge) prepareTunnel() error {
	if !slices.ContainsFunc(x.store.List(), func(p provider.Provider) bool { return p.Enabled }) {
		return errors.New(i18n.T("还没有 Provider", "No Provider yet"))
	}
	return x.writeConfig()
}

// writeConfig writes config.yaml for the tunnel: bowel's own config, with
// the desktop-only parts replaced by what a phone's VPN needs.
func (x *bridge) writeConfig() error {
	refs := []core.ProviderRef{}
	for _, p := range x.store.List() {
		if p.Enabled {
			refs = append(refs, core.ProviderRef{ID: p.ID, Path: x.store.Path(p.ID)})
		}
	}
	s := x.set.Get()
	all := routes.Build(s.Routing, s.Rules)
	scopes, orders := x.nodeLists(all)
	opts := s.Options.Clone()
	opts.FindProcess, opts.Interface = "off", ""
	raw, err := core.BuildConfig(core.Input{
		Providers: refs, Routes: all, UserRules: routes.Active(all, s.Rules), Scopes: scopes, Orders: orders,
		Mode: s.Mode, Tun: true, LogLevel: s.LogLevel, TestURL: s.TestURL, MainURLs: mainURLs(s), ProbePort: probePort, Options: opts,
	})
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	tun := cfg["tun"].(map[string]any)
	tun["stack"], tun["mtu"] = "gvisor", 1500
	tun["auto-route"], tun["auto-detect-interface"], tun["strict-route"] = false, false, false
	tun["inet4-address"] = []string{"198.18.0.1/30"}
	tun["dns-hijack"] = []string{"any:53"}
	// A reload has to name the descriptor the running core was started with.
	// Without it the core drops the TUN and can't open another. Whoever
	// starts the core next writes its own number over this one.
	if fd, ok := x.tunFD(); ok {
		tun["file-descriptor"] = fd
	}
	if runtime.GOOS == "android" {
		// An app cannot read other apps' sockets on Android.
		cfg["find-process-mode"] = "off"
	}
	delete(cfg, "mixed-port")
	if err := x.lockProbe(cfg); err != nil {
		return err
	}
	if runtime.GOOS == "ios" {
		// The extension has little memory; the rule data ships with the app.
		cfg["geo-auto-update"] = false
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(x.dir, "config.yaml"), out, 0o600)
}

// tunFD is the TUN descriptor the service or extension last started the core with.
func (x *bridge) tunFD() (int, bool) {
	raw, err := os.ReadFile(filepath.Join(x.dir, tunFDFile))
	if err != nil {
		return 0, false
	}
	fd, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	return fd, err == nil
}

func (x *bridge) core() (*mihomo.Client, error) {
	x.mu.Lock()
	c := x.client
	x.mu.Unlock()
	if c == nil {
		return nil, errors.New(i18n.T("未接管时不能这样做", "Turn bowel on first"))
	}
	return c, nil
}

func (x *bridge) addFile(ctx context.Context, path string) error {
	if _, err := x.store.AddFile(path); err != nil {
		return err
	}
	return x.applyConfig(ctx)
}

func (x *bridge) updateProvider(ctx context.Context, id string) error {
	if _, err := x.store.Update(ctx, http.DefaultClient, id); err != nil {
		x.push()
		return err
	}
	return x.applyConfig(ctx)
}

// updateAll refreshes every enabled subscription; it says how many failed.
func (x *bridge) updateAll() int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	failed := 0
	for _, p := range x.store.List() {
		if p.Kind != provider.Subscription || !p.Enabled {
			continue
		}
		if _, err := x.store.Update(ctx, http.DefaultClient, p.ID); err != nil {
			failed++
		}
	}
	_ = x.applyConfig(ctx)
	return failed
}

func (x *bridge) renameProvider(id, name string) error {
	if _, err := x.store.Rename(id, name); err != nil {
		return err
	}
	x.push()
	return nil
}

func (x *bridge) deleteProvider(ctx context.Context, id string) error {
	if err := x.store.Delete(id); err != nil {
		return err
	}
	return x.applyConfig(ctx)
}

func (x *bridge) setProviderEnabled(ctx context.Context, id string, on bool) error {
	if _, err := x.store.SetEnabled(id, on); err != nil {
		return err
	}
	return x.applyConfig(ctx)
}

func (x *bridge) setAutoUpdate(id string, hours int) error {
	if _, err := x.store.SetAutoUpdate(id, hours); err != nil {
		return err
	}
	x.push()
	return nil
}

func (x *bridge) testRoute(ctx context.Context, routeID string) error {
	c, err := x.core()
	if err != nil {
		return err
	}
	r, err := x.route(routeID)
	if err != nil {
		return err
	}
	url := x.set.Get().TargetsOf(routeID)[0].URL
	if _, err := c.GroupDelay(ctx, routes.Group(r), url, 5*time.Second); err != nil {
		return err
	}
	_ = x.refresh(ctx)
	x.push()
	return nil
}

func (x *bridge) testProvider(ctx context.Context, id string) error {
	c, err := x.core()
	if err != nil {
		return err
	}
	if err := c.HealthCheck(ctx, "p-"+id); err != nil {
		return err
	}
	_ = x.refresh(ctx)
	x.push()
	return nil
}

func (x *bridge) closeConns(ctx context.Context, ids []string) error {
	c, err := x.core()
	if err != nil {
		return err
	}
	var first error
	for _, id := range ids {
		if err := c.CloseConnection(ctx, id); err != nil && first == nil {
			first = err
		}
	}
	x.mon.Wake()
	return first
}

func (x *bridge) closeConn(ctx context.Context, id string) error {
	c, err := x.core()
	if err != nil {
		return err
	}
	return c.CloseConnection(ctx, id)
}

// addRule sends a domain and everything under it to a route, ahead of the
// template's rules, and closes what it matches that goes elsewhere.
func (x *bridge) addRule(ctx context.Context, host, routeID string) error {
	r, err := routes.ParseRule(routes.KindSuffix, host, routeID)
	if err != nil {
		return err
	}
	return x.setRule(ctx, r, false)
}

func (x *bridge) reload(ctx context.Context, c *mihomo.Client) error {
	if err := x.writeConfig(); err != nil {
		return err
	}
	return c.Reload(ctx, filepath.Join(x.dir, "config.yaml"))
}

func (x *bridge) logs() []logData {
	out := []logData{}
	list := x.mon.Logs(0)
	for i := len(list) - 1; i >= 0; i-- {
		l := list[i]
		level := "info"
		if l.Level == "warning" || l.Level == "error" {
			level = "warn"
		}
		out = append(out, logData{Time: l.Time.UnixMilli(), Level: level, Text: l.Text})
	}
	return out
}

// ruleText says in words which rule matched a connection.
func ruleText(kind, payload string) string {
	switch kind {
	case "", "Match":
		return i18n.T("未命中，走默认", "No rule matched, default route")
	case "DomainSuffix":
		return i18n.T("域名后缀 ", "Domain suffix ") + payload
	case "Domain":
		return i18n.T("域名 ", "Domain ") + payload
	case "DomainKeyword":
		return i18n.T("域名关键词 ", "Domain keyword ") + payload
	case "GeoSite":
		return "GeoSite " + payload
	case "GeoIP":
		return "GeoIP " + payload
	case "IPCIDR", "IPCIDR6":
		return i18n.T("IP 段 ", "IP range ") + payload
	}
	return kind + " " + payload
}

func display(node string) string {
	if n, _, ok := strings.Cut(node, core.NodeSep); ok {
		return n
	}
	return node
}

func placeOf(name string) placeData {
	p, ok := geo.FromName(display(name))
	if !ok {
		return placeData{}
	}
	return placeData{Country: p.Country, City: geo.Display(p.City), Flag: p.Flag, Lat: p.Lat, Lon: p.Lon}
}
