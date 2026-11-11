package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/netproxy"
	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/devnet"
	"github.com/oboo/moho/internal/gateway"
	"github.com/oboo/moho/internal/geo"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/ipinfo"
	"github.com/oboo/moho/internal/locate"
	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/monitor"
	"github.com/oboo/moho/internal/nowindow"
	"github.com/oboo/moho/internal/procinfo"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/ruleset"
	"github.com/oboo/moho/internal/settings"
	"github.com/oboo/moho/internal/sysproxy"
	"github.com/oboo/moho/internal/tun"
)

// App ties the core, the stores, the monitors and the window together.
type App struct {
	dataDir   string
	settings  *settings.Store
	providers *provider.Store
	ruleSets  *ruleset.Store
	core      *core.Manager
	mon       *monitor.Monitor
	loc       *locate.Locator
	qual      *ipinfo.Store
	http      *http.Client
	coreBin   string      // the bundled core
	tunBin    string      // the setuid copy TUN runs
	tunStale  atomic.Bool // tunBin is another version than coreBin
	// tunFault is why the core at tunBin did not start; while it is set
	// the bundled core runs in its place, with TUN out of its config.
	tunFault  atomic.Pointer[string]
	tunGone   atomic.Bool // what tunBin's installation stands for was removed: it is to be made again
	probePort int
	gw        *gateway.Gateway

	setMu     sync.Mutex // serialises updateSettings
	routesMu  sync.Mutex // guards the two below: see routes
	routesNow []routes.Route
	imp       importedCache // the routing of a provider's own config, if in use
	routesAt  uint64
	// proxyMu is held while the system proxy changes, so that quitting
	// waits for a change under way instead of being undone by it.
	proxyMu sync.Mutex
	// openAtLogin is whether Moho is a login item, as last read.
	openAtLogin bool
	mu          sync.Mutex
	pending     map[string]bool
	locating    map[string]bool
	qualifying  bool // a quality lookup is under way: one at a time
	sys         sysproxy.Status
	// proxyErr is why the last change of the system proxy failed. The
	// interface says so instead of leaving "another program changed it"
	// as the only reading of a switch that did nothing.
	proxyErr    string       // why the last try to take the system proxy failed
	failed      *failedWrite // that try, if the OS refused it: see retryWrite
	proxies     map[string]mihomo.Proxy
	provNodes   map[string][]string // provider id → node names, in the provider's order
	// nodesSig is the names /proxies had when provNodes was read, at
	// nodesAt: the providers' lists are read again when they change.
	nodesSig uint64
	nodesAt  time.Time
	// nodeProxies are the providers' nodes as last read. The maps below are
	// replaced, never changed, so a snapshot can hold on to them.
	nodeProxies map[string]mihomo.Proxy
	// strip holds the name rules compiled, what the settings say node names
	// are shown without (Settings.StripRules). Replaced by loadStrip whenever
	// the setting changes; never written after it is stored.
	strip atomic.Pointer[[]*regexp.Regexp]
	pings       map[string]pingResult
	sites       map[string]siteResult // by route id
	sitesBusy   map[string]bool
	routeAt     map[string]time.Time // route id → when its main target was last tested
	pingBusy    atomic.Bool
	mainBusy    atomic.Bool
	you         *locate.Result
	lastNode    map[string]string // route id → node, to notice automatic switches
	switched    *switchNote
	shareDay    string
	routeBytes  map[string]int64 // today's download per route id
	groupSeen   map[string]int64 // the monitor's GroupBytes when last added in
	monCancel   context.CancelFunc
	lastHash    [32]byte
	emitQueued  bool
	win         *mygo.Window
	ui          *UI
	// winShown is whether the main window is shown, kept from its events
	// for goroutines that may not wait on the main thread to ask.
	winShown atomic.Bool
	tray     *mygo.Tray
	quitting bool
	quitOnce sync.Once
	leaving  bool // beforeQuit is running; main thread only
	lastTick Tick
	// The gateway as last measured, and what its last change said.
	gwProbe   gwProbe
	gwProbing bool
	gwErr     error
	upd       updateState
	// headless: no window, tray or main thread's loop (`bowel daemon`,
	// app_daemon.go). stop ends such a run.
	headless bool
	stop     chan struct{}
	// watchedAt is when a command last read the connections or the logs,
	// in Unix seconds: they are polled every second while one watches.
	watchedAt atomic.Int64
	dev       devState
	tools     toolsState
}

type switchNote struct {
	route, from, to string // route is the route's ID; from and to are the core's node names
	at              time.Time
}

// switchShow is how long the news line says a switch. It is a moment's news,
// not a state: the route has already moved, and the line must not keep the
// title bar for as long as the node it left stays down.
const switchShow = 8 * time.Second

func newApp() (*App, error) {
	data, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return nil, err
	}
	migrateData(data)
	st, err := settings.Open(data)
	if err != nil {
		return nil, err
	}
	i18n.Set(st.Get().Language)
	provDir := storeDir(data, "providers", runtime.GOOS)
	ps, err := provider.Open(provDir)
	if err != nil {
		return nil, err
	}
	sets, err := ruleset.Open(storeDir(data, "rulesets", runtime.GOOS))
	if err != nil {
		return nil, err
	}
	bin := bundledCore()
	tun.CoreDir = filepath.Join(data, "core") // where the core runs, below
	tunBin := tun.Path(data, filepath.Base(bin))
	run := bin
	stale := false
	if tun.Installed(tunBin) {
		// A root copy older than this version: while TUN needs it, it stays
		// until the user authorizes a new one; otherwise the new core runs.
		stale = tun.Stale(bin, tunBin)
		if !stale || st.Get().Tun {
			run = tunBin
		}
	}
	cm, err := core.New(run, tun.CoreDir)
	if err != nil {
		return nil, err
	}
	cm.Spawn = spawnTun
	probe, err := core.FreePort()
	if err != nil {
		return nil, err
	}
	a := &App{
		dataDir: data, settings: st, providers: ps, ruleSets: sets, core: cm,
		mon:  monitor.New(filepath.Join(data, "stats.json")),
		loc:  locate.New(filepath.Join(data, "locations.json")),
		qual: ipinfo.Open(filepath.Join(data, "ipinfo.json")),
		http: &http.Client{Transport: netproxy.Transport()}, coreBin: bin, tunBin: tunBin, probePort: probe,
		pending: map[string]bool{}, locating: map[string]bool{}, proxies: map[string]mihomo.Proxy{},
		provNodes: map[string][]string{}, lastNode: map[string]string{}, routeBytes: map[string]int64{},
		gw: gateway.New(osGateway(), filepath.Join(data, "gateway-before.json")),
	}
	ps.Vet = a.vetUpdate
	a.tunStale.Store(stale)
	cm.SafePaths = []string{provDir, sets.Dir()} // for a core that is the app's child; see storeDir
	a.keepRuleSets()
	a.loadStrip()                     // what node names are shown without, before anything shows one
	a.mon.SkipGroup = core.ProbeGroup // the location probe's own traffic
	a.mon.FindProcess = procinfo.New().Find
	a.mon.Every = a.pollEvery
	cm.OnStatus = func(s core.Status) { a.onCoreStatus(s) }
	cm.OnListenError = func() { a.emit() }
	a.mon.OnTick(a.onTick)
	return a, nil
}

// storeDir is where the files of one kind ("providers", "rulesets") are
// kept, which the core's config names. mihomo reads nothing outside its
// own directory unless a SAFE_PATHS variable allows it, and only a core
// that is the app's child can be given one. On Windows the core TUN runs
// on is started by a scheduled task, with no environment of the app's, so
// there the files live inside the core's directory, and what an earlier
// version kept beside it is moved in once. A move that fails (a file in
// use) leaves the files where they were, for the next start to try again.
func storeDir(data, name, goos string) string {
	old := filepath.Join(data, name)
	if goos != "windows" {
		return old
	}
	dir := filepath.Join(data, "core", name)
	if _, err := os.Lstat(dir); err == nil {
		return dir
	}
	if _, err := os.Lstat(old); err != nil {
		return dir // nothing to move
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err == nil {
		err = os.Rename(old, dir)
		if err == nil {
			return dir
		}
		log.Printf("moving %s into the core's directory: %v", old, err)
	}
	return old
}

// bundledCore finds mihomo: $MOHO_CORE, else bin/mihomo among the resources.
func bundledCore() string {
	if p := os.Getenv("MOHO_CORE"); p != "" {
		return p
	}
	name := "mihomo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dir, _ := mygo.App.Path(mygo.PathResources)
	if p := filepath.Join(dir, "bin", name); exists(p) {
		return p
	}
	return filepath.Join(dir, runtime.GOOS+"-"+runtime.GOARCH, "bin", name)
}

// registerFonts adds the bundled interface face (ui.go's sans), shipped
// unmodified with its license, as the OPPO Sans license requires.
func registerFonts() {
	res, _ := mygo.App.Path(mygo.PathResources)
	b, err := os.ReadFile(filepath.Join(res, "fonts", "OPPOSans4.0.ttf"))
	if err == nil {
		err = ui.RegisterFont(b, "")
	}
	if err != nil {
		log.Println("font:", err)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// copyGeo puts the bundled rule data next to the core's config once.
func (a *App) copyGeo() {
	res, _ := mygo.App.Path(mygo.PathResources)
	for _, f := range []string{"GeoSite.dat", "GeoIP.dat"} {
		dst := filepath.Join(a.core.Dir, f)
		if exists(dst) {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(res, "geo", f)); err == nil {
			_ = os.WriteFile(dst, b, 0o644)
		}
	}
}

// ---- Lifecycle ----

func (a *App) start() {
	// Here, not in newApp: SetSource waits for the main thread, which a
	// test's goroutine has no loop on.
	mygo.Theme.SetSource(mygo.ThemeSource(appearanceOf(a.settings.Get())))
	registerFonts()
	a.openAtLogin = openAtLoginOS()
	a.upd.notify = a.emit
	a.loadUpdateOutcome(mygo.App.Version())
	a.openWindow()
	a.setupTray()
	a.copyGeo()
	if s := a.settings.Get(); !s.Tun || !a.takeoverOn(s) {
		// TUN is not going to run this time: anything it left on the system
		// from a run that ended badly is in the way of the network, and the
		// core has not started to need it yet.
		a.repairTun()
	}
	go func() {
		a.detectDev(2 * time.Second)
		if err := a.applyConfig(); err != nil {
			log.Println("core:", err)
		}
	}()
	go a.loop()
	go a.updateLoop()
	go a.devLoop()
	go a.refreshTools()
	go a.probeSources()
	go a.debugDriver()
	a.startMCP()
	a.startCLI()
}

func (a *App) openWindow() {
	u := newUI(a)
	// Before the window, so that its first frame already knows what there
	// is: nothing to list opens on the first-run screen.
	u.setSnapshot(a.snapshot())
	u.setup.on = len(u.snap.Providers) == 0
	win := mygo.NewWindow(mygo.WindowOptions{
		Title: "bowel", Width: 1060, Height: 760, MinWidth: 960, MinHeight: 660,
		TitleBarStyle: mygo.TitleBarHidden, StateKey: "main",
		// The title bar is then as tall as the controls plus twice Y, and
		// they sit in its middle: with 14pt buttons, 50, as ui.go's
		// titleBar, so they line up with the tabs.
		TrafficLightPosition: &mygo.Point{X: 18, Y: 18},
		BackgroundColor:      "light-dark(#f6f4ef, #171615)",
		Content:              ui.View(u.view),
		// Opened at login, Moho can stay in the menu bar.
		Hidden: a.settings.Get().StartHidden && mygo.App.WasOpenedAtLogin(),
	})
	u.win = win
	a.mu.Lock()
	a.ui = u
	a.mu.Unlock()
	win.OnClose(func(e *mygo.CloseEvent) {
		a.mu.Lock()
		q := a.quitting
		a.mu.Unlock()
		if !q { // closing keeps Moho in the menu bar
			e.PreventDefault()
			win.Hide()
		}
	})
	// The Dock icon is there while a window is: hidden, Moho lives in
	// the menu bar alone.
	win.OnShow(func() { a.winShown.Store(true); a.mon.Wake(); a.syncDock() })
	win.OnHide(func() { a.winShown.Store(false); a.syncDock() })
	a.winShown.Store(!(a.settings.Get().StartHidden && mygo.App.WasOpenedAtLogin()))
	a.mu.Lock()
	a.win = win
	a.mu.Unlock()
	a.syncDock()
}

// syncDock shows the Dock icon while the main or settings window is
// open, and hides it otherwise. On the main thread.
func (a *App) syncDock() {
	if runtime.GOOS != "darwin" {
		return
	}
	a.mu.Lock()
	win, u := a.win, a.ui
	a.mu.Unlock()
	open := win != nil && win.IsVisible()
	if u != nil && (u.set.win.Load() != nil || u.clog.win.Load() != nil) {
		open = true
	}
	want := mygo.ActivationPolicyAccessory
	if open {
		want = mygo.ActivationPolicyRegular
	}
	if mygo.App.ActivationPolicy() != want {
		mygo.App.SetActivationPolicy(want)
	}
}

func (a *App) showWindow() {
	a.mu.Lock()
	win := a.win
	a.mu.Unlock()
	// Back in the Dock before the window shows, so it comes to the front
	// with the menu bar.
	if runtime.GOOS == "darwin" {
		mygo.App.Dock.Show()
	}
	if win != nil {
		win.Show()
		win.Focus()
	}
	mygo.App.Focus()
}

// onSecondInstance runs when bowel is started again while it runs: the
// window is most likely hidden, and that start was to get it back. On the
// main thread. An app on its way out stays hidden.
func (a *App) onSecondInstance([]string, string) {
	if a.leaving || a.isQuitting() {
		return
	}
	a.showWindow()
}

func (a *App) uiRef() *UI {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ui
}

// pollEvery is how often the monitor reads the core's connections: every
// second while the window or the menu bar panel shows them, every 2 s
// while only the menu bar icon shows the rates (Settings.TraySpeed), every
// 5 s otherwise. The totals stay exact, as they are the core's own
// counters; what goes is the rates' detail and connections too short to
// be seen.
func (a *App) pollEvery() time.Duration {
	if a.winShown.Load() {
		return time.Second
	}
	if u := a.uiRef(); u != nil && u.panel.win.Load() != nil {
		return time.Second
	}
	if time.Now().Unix()-a.watchedAt.Load() < 5 {
		return time.Second
	}
	if st := a.settings.Get(); st.TraySpeed && (a.takeoverOn(st) || trafficNow(a.tick())) {
		return traySpeedEvery
	}
	return 5 * time.Second
}

// nodeSig tells apart the sets of names in px, and of the nodes its groups
// can use.
func nodeSig(px map[string]mihomo.Proxy) uint64 {
	names := slices.Sorted(maps.Keys(px))
	h := fnv.New64a()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write([]byte{0})
		for _, m := range px[n].All {
			h.Write([]byte(m))
			h.Write([]byte{0})
		}
	}
	return h.Sum64()
}

// addNodes puts the providers' nodes into px beside the groups.
func addNodes(px, nodes map[string]mihomo.Proxy) {
	for name, n := range nodes {
		if _, group := px[name]; !group {
			px[name] = n
		}
	}
}

// loop refreshes what the core and the OS say, and runs the periodic jobs.
func (a *App) loop() {
	fast := time.NewTicker(2 * time.Second)
	slow := time.NewTicker(10 * time.Minute)
	defer fast.Stop()
	defer slow.Stop()
	a.refresh()
	for {
		select {
		case <-fast.C:
			a.refresh()
		case <-slow.C:
			a.autoUpdate()
			a.locateExits(false)
		}
	}
}

// nodesEvery is how often the providers' lists are read when nothing asks.
const nodesEvery = 15 * time.Second

func (a *App) refresh() {
	a.probeSoon()
	// The system proxy is judged only while it is wanted: otherwise no
	// scutil every 2 s for nothing.
	if st := a.settings.Get(); a.takeoverOn(st) && st.SystemProxy {
		if s, err := sysproxy.Check(st.MixedPort); err == nil {
			a.mu.Lock()
			a.sys = s
			if s.Ours {
				a.proxyErr = ""
			}
			a.mu.Unlock()
		}
	}
	if c, err := a.core.Client(); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		px, err := c.Proxies(ctx)
		// /proxies has the groups only. The nodes, and so their tests, are
		// in the providers' lists: read them when the names change or some
		// seconds on, since a big subscription makes a long answer.
		sig := nodeSig(px)
		a.mu.Lock()
		nodes, known, stale := a.provNodes, a.nodeProxies, sig != a.nodesSig || time.Since(a.nodesAt) > nodesEvery
		a.mu.Unlock()
		if err == nil && stale {
			var pp map[string]mihomo.ProxyProvider
			if pp, err = c.ProxyProviders(ctx); err == nil {
				nodes, known = map[string][]string{}, map[string]mihomo.Proxy{}
				im := a.importedNow()
				for name, p := range pp {
					id, ok := strings.CutPrefix(name, "p-")
					if im != nil && slices.Contains(im.ProviderNames(), name) { // the nodes of a source's own provider count as its
						id, ok = a.settings.Get().ImportedRouting, true
					}
					if !ok {
						continue
					}
					for _, n := range p.Proxies {
						nodes[id] = append(nodes[id], n.Name)
						known[n.Name] = n
					}
				}
			}
		}
		cancel()
		if err == nil {
			addNodes(px, known)
			a.mu.Lock()
			a.proxies, a.provNodes, a.nodeProxies = px, nodes, known
			if stale {
				a.nodesSig, a.nodesAt = sig, time.Now()
			}
			a.mu.Unlock()
			a.noticeSwitches()
			if stale {
				a.keepMeasured()
			}
		}
	}
	if a.settings.Get().IPQuality {
		// Turned on (or the exit changed) while the app runs: what the
		// address is gets asked within a frame or two, not at the next
		// quarter-hour look.
		a.mu.Lock()
		px := a.proxies
		a.mu.Unlock()
		a.qualitySoon(currentNode(px, a.route(routes.Default)))
	}
	a.emit()
}

func (a *App) onCoreStatus(s core.Status) {
	a.mu.Lock()
	if a.monCancel != nil {
		a.monCancel()
		a.monCancel = nil
	}
	a.mu.Unlock()
	if s.State == core.Running {
		if c, err := a.core.Client(); err == nil {
			ctx, cancel := context.WithCancel(context.Background())
			a.mu.Lock()
			a.monCancel = cancel
			a.mu.Unlock()
			go a.mon.Run(ctx, c)
		}
		go func() {
			a.applySystemProxy()
			a.refresh()
			a.locateExits(true)
			a.youSoon() // and once more, with the core settled
		}()
	}
	a.emit()
}

// tunEntry is TUN as it really is. wanted is the user's choice and on is
// whether takeover is; installed is whether its core has its
// authorisation; fault is why that core did not start, when another runs
// in its place.
func tunEntry(wanted, on, installed bool, fault string, state core.State) Entry {
	e := Entry{Wanted: wanted, Status: "off"}
	switch {
	case !installed:
		e.Status = "install"
	case !on || !wanted:
	case fault != "":
		e.Status, e.Detail = "error", fmt.Sprintf(i18n.T("TUN 的内核没有启动，其余功能照常：%s", "The core TUN runs on didn't start; everything else works: %s"), fault)
	case state == core.Running:
		e.Status = "on"
	default:
		e.Status, e.Detail = "error", i18n.T("内核没有运行", "The core isn't running")
	}
	return e
}

// ---- Config ----

func (a *App) takeoverOn(s settings.Settings) bool { return !s.Paused && (s.SystemProxy || s.Tun) }

// applyConfig rebuilds the runtime config and makes the core use it.
func (a *App) applyConfig() error {
	a.dropImported() // a provider's file, or whether it is in use, may have changed
	s := a.settings.Get()
	a.ensureIPv6(s)
	im := a.importedNow()
	err := a.applyDev(s, func(dev devnet.Overlay) ([]byte, error) { return a.buildConfig(s, dev, im) })
	if err != nil {
		a.emit()
		return err
	}
	if s.Mode == "global" {
		a.pointGlobal()
	}
	a.adoptSelections()
	logPolicyErr(a.followPolicies())
	a.mu.Lock()
	a.nodesAt = time.Time{}       // providers may hold other nodes now
	a.routeAt, a.sites = nil, nil // and a reload forgets what each URL did
	a.mu.Unlock()
	a.syncGateway()
	a.refresh()
	return nil
}

// buildConfig is the core's config for s; im, if not nil, is the routing to
// use in place of the route groups.
func (a *App) buildConfig(s settings.Settings, dev devnet.Overlay, im *core.Imported) ([]byte, error) {
	var refs []core.ProviderRef
	for _, p := range a.providers.List() {
		if p.Enabled {
			refs = append(refs, core.ProviderRef{ID: p.ID, Path: a.providers.Path(p.ID)})
		}
	}
	all := a.routes()
	scopes, orders := a.nodeLists(all)
	return core.BuildConfig(core.Input{
		Providers: refs, RuleSets: a.ruleSetRefs(s), Routes: all, UserRules: routes.Active(all, s.Rules), Scopes: scopes, Orders: orders, Mode: s.Mode, MixedPort: s.MixedPort, AllowLan: s.AllowLan,
		Tun:      s.Tun && a.takeoverOn(s) && osTun.installed(a.tunBin) && a.tunFault.Load() == nil,
		Gateway:  a.gatewayOn(s),
		LogLevel: s.LogLevel, TestURL: s.TestURL, MainURLs: mainURLs(s), ProbePort: a.probePort, Controller: a.core.ControllerFields(),
		Options: s.Options, Dev: dev, IPv6Usable: a.devEnv().IPv6.Usable, Imported: im,
	})
}

// pointGlobal makes global mode use the default route.
func (a *App) pointGlobal() {
	if c, err := a.core.Client(); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = c.Select(ctx, "GLOBAL", routes.Group(a.route(routes.Default)))
	}
}

// osProxy is what changes the system proxy. Tests replace it, so that they
// can never change the machine's settings.
var osProxy = struct {
	enable    func(host string, port int, bypass []string) error
	disable   func() error
	snapshot  func() (sysproxy.Saved, error)
	restore   func(context.Context, sysproxy.Saved) error
	listening func(ctx context.Context, host string, port int) bool
}{sysproxy.Enable, sysproxy.Disable, sysproxy.Snapshot, sysproxy.Restore, sysproxy.Listening}

// applySystemProxy makes the OS match the settings. prev are mixed ports
// the system may still point at, as after the port changed.
func (a *App) applySystemProxy(prev ...int) {
	a.proxyMu.Lock()
	defer a.proxyMu.Unlock()
	if a.isQuitting() {
		return // shutdown has put the system proxy back: it stays so
	}
	s := a.settings.Get()
	want := a.takeoverOn(s) && s.SystemProxy
	cur, err := sysproxy.Check(s.MixedPort)
	if err != nil {
		a.noteSystemProxy(cur, fmt.Errorf("reading the system proxy: %w", err))
		return
	}
	state := fmt.Sprintf("want=%v paused=%v systemProxy=%v port=%d cur={on:%v ours:%v %s:%d}",
		want, s.Paused, s.SystemProxy, s.MixedPort, cur.On, cur.Ours, cur.Host, cur.Port)
	step := "nothing to do"
	var opErr error
	switch {
	case want && !cur.Ours:
		step = "enable"
		opErr = a.enableProxy(s, prev...)
	case !want && cur.Ours:
		step = "disable"
		opErr = a.disableProxy(context.Background(), s.MixedPort)
	case !want:
		a.dropProxyBackup() // left by a crash, and the proxy has moved on since
		step = "drop the backup"
	}
	// Read it again before the interface draws: a switch that was just
	// flipped must not read as "another program changed it" for the second
	// or two until the next poll.
	now, cerr := sysproxy.Check(s.MixedPort)
	switch {
	case cerr != nil:
		a.writeSystemProxyLog(fmt.Sprintf("%s | %s | after: %v | err: %v", step, state, cerr, opErr))
		a.noteSystemProxy(cur, errors.Join(opErr, cerr))
	case opErr != nil || (want && !now.Ours):
		// What was asked for did not happen: say why, in the log and on
		// the screen, rather than letting another program take the blame.
		if opErr == nil {
			opErr = fmt.Errorf("the change did not take: the proxy is %v, ours %v", now.On, now.Ours)
		}
		log.Println("system proxy:", opErr)
		a.writeSystemProxyLog(fmt.Sprintf("%s | %s | after={on:%v ours:%v %s:%d} | err: %v",
			step, state, now.On, now.Ours, now.Host, now.Port, opErr))
		a.noteSystemProxy(now, opErr)
	default:
		a.noteSystemProxy(now, nil)
	}
}

// noteSystemProxy records what the system proxy is now and, when a change
// was asked for and did not happen, why. A windowed build has no console for
// log.Println, so the reason would otherwise be lost and the screen could
// only say that something else changed the proxy.
func (a *App) noteSystemProxy(st sysproxy.Status, err error) {
	a.mu.Lock()
	a.sys = st
	if err != nil {
		a.proxyErr = err.Error()
	} else {
		a.proxyErr = ""
	}
	a.mu.Unlock()
}

// writeSystemProxyLog appends a line to sysproxy.log, which is what to ask
// for when the switch does nothing: a windowed build has no console.
func (a *App) writeSystemProxyLog(msg string) {
	path := filepath.Join(a.dataDir, "sysproxy.log")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if e != nil {
		return
	}
	defer f.Close()
	if st, e := f.Stat(); e == nil && st.Size() > 32<<10 {
		_ = f.Truncate(0)
		_, _ = f.Seek(0, 0)
	}
	_, _ = f.WriteString(time.Now().Format("2006-01-02 15:04:05") + " " + msg + "\n")
}

// proxyBackup is the system proxy as it was before Moho took it over. It is
// kept on disk, so that a crash does not lose it.
func (a *App) proxyBackup() string { return filepath.Join(a.dataDir, "sysproxy-before.json") }

// enableProxy points the system proxy at the core. The first time, it
// keeps what the system had, for disableProxy to put back; while the
// system still points at Moho (s's port, or one of prev), it keeps the
// first copy.
func (a *App) enableProxy(s settings.Settings, prev ...int) error {
	ports := append([]int{s.MixedPort}, prev...)
	keep := false
	if exists(a.proxyBackup()) {
		for _, p := range ports {
			if cur, err := sysproxy.Check(p); err == nil && cur.Ours {
				keep = true
			}
		}
	}
	return a.pointProxy(s, ports, keep)
}

// pointProxy does enableProxy's work: unless keep says the copy on disk
// stays, it first replaces that with what the system has, without ports.
// A failure is kept for the status to tell: it is not another program's doing.
func (a *App) pointProxy(s settings.Settings, ports []int, keep bool) error {
	failed, err := a.writeProxy(s, ports, keep)
	a.mu.Lock()
	a.proxyErr, a.failed = "", failed
	if err != nil {
		a.proxyErr = firstLine(err.Error())
	}
	a.mu.Unlock()
	return err
}

// writeProxy says, when the OS refused the write, what retryWrite needs to
// try it again. The copy on disk is done by then, whatever the write left.
func (a *App) writeProxy(s settings.Settings, ports []int, keep bool) (*failedWrite, error) {
	if !keep {
		saved, err := osProxy.snapshot()
		if err != nil {
			return nil, fmt.Errorf("reading the system proxy: %w", err)
		}
		saved.Forget(ports...)
		b, err := json.MarshalIndent(saved, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(a.proxyBackup(), b, 0o600); err != nil {
			return nil, err
		}
	}
	bypass := a.bypassOf(s)
	if err := osProxy.enable("127.0.0.1", s.MixedPort, bypass); err != nil {
		left, serr := osProxy.snapshot()
		if serr != nil {
			return nil, err // no telling later whether another program came
		}
		return &failedWrite{left: left, inputs: a.writeInputs(s)}, err
	}
	a.dev.mu.Lock()
	a.dev.bypass = bypass
	a.dev.mu.Unlock()
	return nil, nil
}

// proxyRetries is how many times a write the OS refused is tried again. With
// the waits of retryWrite that is over some ten minutes, enough for what
// passes; a write that can never work costs a handful of tries, on every OS,
// without telling the errors apart.
const proxyRetries = 5

// failedWrite is a write of the system proxy that the OS refused. Fields
// change under proxyMu only.
type failedWrite struct {
	left   sysproxy.Saved // the system proxy just after it
	inputs string         // what the write was made from: see writeInputs
	tries  int            // tried again so many times
	wait   int            // devLoop ticks before the next try
}

// writeInputs is what a write depends on besides the OS: when it changes, a
// write that failed may work, and the tries start over.
func (a *App) writeInputs(s settings.Settings) string {
	env := a.devEnv()
	return strings.Join(append([]string{env.PrimaryIface, overlayOf(s, env).Fingerprint()}, s.Bypass...), "\n")
}

// coreUp says whether there is a core for the system proxy to point at.
var coreUp = func(a *App) bool { return a.core.Status().State == core.Running }

// retryWrite tries again a write of the system proxy that the OS refused.
// That is bowel finishing what it was asked to do, unlike taking the proxy
// back from another program, which only the user decides: so nothing happens
// without a failure of bowel's own on record, and once the system proxy is
// no longer as the failure left it, bowel lets it be. The caller holds proxyMu.
func (a *App) retryWrite(s settings.Settings) {
	a.mu.Lock()
	f, why := a.failed, a.proxyErr
	if why == "" {
		a.failed = nil
	}
	a.mu.Unlock()
	if f == nil || why == "" || !coreUp(a) {
		return
	}
	if in := a.writeInputs(s); in != f.inputs {
		f.inputs, f.tries, f.wait = in, 0, 0
	}
	if f.tries >= proxyRetries {
		return
	}
	if f.wait > 0 {
		f.wait--
		return
	}
	cur, err := sysproxy.Check(s.MixedPort)
	if err != nil || cur.Ours {
		return // the next refresh clears the failure of a proxy that is ours
	}
	now, err := osProxy.snapshot()
	if err != nil {
		return
	}
	if !reflect.DeepEqual(now, f.left) {
		a.mu.Lock()
		a.failed = nil
		a.mu.Unlock()
		log.Println("system proxy: changed since the failed write, not trying again")
		return
	}
	// The copy on disk is from before the first failure. What the system has
	// now may be half of bowel's write, so it must not replace that copy.
	err = a.pointProxy(s, []int{s.MixedPort}, exists(a.proxyBackup()))
	if err == nil {
		log.Println("system proxy: taken over on try", f.tries+2)
		return
	}
	tries := f.tries + 1
	a.mu.Lock()
	if a.failed != nil {
		a.failed.tries, a.failed.wait = tries, 1<<tries-1
	}
	a.mu.Unlock()
	if firstLine(err.Error()) != why {
		log.Println("system proxy:", err)
	}
	if tries == proxyRetries {
		log.Println("system proxy: giving up after", tries+1, "tries:", why)
	}
}

// disableProxy puts back the system proxy enableProxy kept, or, with no
// copy kept, turns the proxies off. If ctx ends first the copy stays on
// disk, for the next launch to finish from.
func (a *App) disableProxy(ctx context.Context, port int) error {
	b, err := os.ReadFile(a.proxyBackup())
	var saved sysproxy.Saved
	if err != nil || json.Unmarshal(b, &saved) != nil {
		return osProxy.disable()
	}
	saved.TurnOffDead(func(host string, port int) bool { return osProxy.listening(ctx, host, port) })
	if err := osProxy.restore(ctx, saved); err != nil {
		if ctx.Err() != nil {
			return err
		}
		// Never leave it pointing at Moho, whatever else failed.
		if cur, cerr := sysproxy.Check(port); cerr == nil && cur.Ours {
			_ = osProxy.disable()
		}
		a.dropProxyBackup()
		return err
	}
	a.dropProxyBackup()
	return nil
}

func (a *App) dropProxyBackup() { _ = os.Remove(a.proxyBackup()) }

// ---- Pending (loading states) ----

func (a *App) begin(key string) func() {
	a.mu.Lock()
	a.pending[key] = true
	a.mu.Unlock()
	a.emit()
	return func() {
		a.mu.Lock()
		delete(a.pending, key)
		a.mu.Unlock()
		a.emit()
	}
}

func (a *App) busy(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pending[key]
}

// ---- Actions ----

func (a *App) setTakeover(on bool) error {
	done := a.begin("takeover")
	defer done()
	_, err := a.settings.Update(func(s *settings.Settings) {
		s.Paused = !on
		if on && !s.SystemProxy && !s.Tun {
			s.SystemProxy = true
		}
	})
	if err != nil {
		return err
	}
	return a.afterTakeover()
}

func (a *App) setSystemProxy(on bool) error {
	done := a.begin("entry:sys")
	defer done()
	if _, err := a.settings.Update(func(s *settings.Settings) {
		s.SystemProxy = on
		if on && s.Paused { // both switches showed off: only this one goes on
			s.Paused, s.Tun = false, false
		}
	}); err != nil {
		return err
	}
	return a.afterTakeover()
}

func (a *App) setTun(on bool) error {
	done := a.begin("entry:tun")
	defer done()
	if on && a.tunNeedsInstall() {
		if err := a.installTun(); err != nil {
			return err
		}
	} else if on {
		a.retryTunCore()
	}
	if _, err := a.settings.Update(func(s *settings.Settings) {
		s.Tun = on
		if on && s.Paused { // both switches showed off: only this one goes on
			s.Paused, s.SystemProxy = false, false
		}
	}); err != nil {
		return err
	}
	err := a.afterTakeover()
	if !on {
		a.tunOff()
	}
	return err
}

// tunOff puts back what turning TUN on changed outside the config: the core
// it runs, which was the copy TUN's privilege needs, and what TUN left on
// the system once it stopped using it.
func (a *App) tunOff() {
	a.core.SetBin(a.coreBin)
	a.repairTun()
}

// osTunReset takes back what TUN left on the system. Tests replace it.
var osTunReset = tun.Reset

// repairTun takes back what TUN left behind once it is off: the routes that
// send traffic into an adapter with no session, and the resolver cache its
// fake IPs filled. The core does this itself when it closes TUN, but a core
// that was killed cannot, and on Windows every stop is a kill.
func (a *App) repairTun() {
	if err := osTunReset(); err != nil {
		log.Println("tun:", err)
	}
}

// tunOffWait is how long the core is given to close TUN before the system is
// asked to do it: closing it is the core's work, and it takes a moment.
const tunOffWait = 1500 * time.Millisecond

// closeTun turns TUN off in the running core, before it is stopped for good.
// The core's own close is what ends the session, takes the adapter and its
// routes away and flushes the fake IPs; a killed core leaves all three, and
// a default route into a tunnel that is gone leaves the machine without a
// network.
func (a *App) closeTun() {
	if _, err := a.core.Client(); err != nil {
		return // nothing running to take it down
	}
	s := a.settings.Get()
	s.Tun, s.Gateway = false, false
	cfg, err := a.buildConfig(s, overlayOf(s, a.devEnv()), nil)
	if err != nil {
		log.Println("tun:", err)
		return
	}
	if err := a.core.Apply(cfg); err != nil {
		log.Println("tun:", err)
		return
	}
	time.Sleep(tunOffWait) // the route table follows within it
}

// tunNeedsInstall is whether turning TUN on takes an authorization. On
// Linux an installation from before the polkit rule, or whose rule names
// another device than the settings now do, is one that does.
func (a *App) tunNeedsInstall() bool {
	a.tellTunDevice()
	return !osTun.installed(a.tunBin) || a.tunStale.Load() || a.tunGone.Load() || osTun.needsRule(a.tunBin)
}

// tellTunDevice gives the installation the device's name as the settings
// have it now, which the rule it leaves on Linux names.
func (a *App) tellTunDevice() {
	if a.settings != nil {
		tun.SetDevice(a.settings.Get().TunDevice)
	}
}

// leaveTunCore answers a core that did not come up: when it was the copy
// TUN runs on, the bundled core takes its place, so that one failure does
// not cost the user everything else the core does, and why is kept for
// the TUN entry to show. It reports whether the core is to be started
// again, with a config built anew (TUN is out of it now).
func (a *App) leaveTunCore(err error) bool {
	var se *core.StartError
	if !errors.As(err, &se) || a.tunBin == a.coreBin || a.core.Bin != a.tunBin {
		return false
	}
	why := err.Error()
	a.tunFault.Store(&why)
	if errors.Is(err, tun.ErrGone) {
		a.tunGone.Store(true)
	}
	log.Println("tun: its core did not start, running the bundled one:", why)
	a.core.Stop()
	a.core.SetBin(a.coreBin)
	return true
}

// retryTunCore goes back to the copy TUN runs on after leaveTunCore left
// it, for the next config to start; and a core that is not running has
// nothing to reload, so the next config starts it whatever it was.
func (a *App) retryTunCore() {
	if a.tunFault.Swap(nil) == nil {
		return
	}
	a.core.Stop()
	a.core.SetBin(a.tunBin)
}

// tunFailed is whether TUN is wanted and is not what the user has: asking
// for it again is then a request to try again, not one already met.
func (a *App) tunFailed() bool {
	return a.snapshot().Takeover.Tun.Status == "error"
}

// installTun puts this version's core in the root copy and runs on it.
func (a *App) installTun() error {
	a.tellTunDevice()
	if err := osTun.install(a.coreBin, a.tunBin); err != nil {
		return err
	}
	a.tunInstalled()
	return nil
}

// tunInstalled moves the core to the root copy, just (re)installed.
func (a *App) tunInstalled() {
	a.tunStale.Store(false)
	a.tunGone.Store(false)
	a.tunFault.Store(nil)
	a.core.Stop()
	a.core.SetBin(a.tunBin)
}

// updateTun replaces a root copy older than this version, on the user's word.
func (a *App) updateTun() error {
	done := a.begin("tun-update")
	defer done()
	if err := a.installTun(); err != nil {
		return err
	}
	return a.afterTakeover()
}

func (a *App) afterTakeover() error {
	err := a.applyConfig()
	a.applySystemProxy()
	a.refresh()
	a.youSoon()
	return err
}

// tunSettle is how long a change of takeover is given before the user's own
// place is measured again: for a moment after TUN comes up, the core's own
// outbound is not yet bound to the interface that keeps it out of the tunnel,
// and a probe taken then goes through the tunnel and comes back with the
// exit's place — which is what the map would then show as "you".
const tunSettle = 6 * time.Second

// youSoon measures the user's own place again once the change has settled.
func (a *App) youSoon() {
	go func() {
		time.Sleep(tunSettle)
		if a.isQuitting() {
			return
		}
		_, _ = a.probeErr(context.Background(), "DIRECT")
	}()
}

func (a *App) setLan(on bool) error {
	done := a.begin("lan")
	defer done()
	if _, err := a.settings.Update(func(s *settings.Settings) { s.AllowLan = on }); err != nil {
		return err
	}
	return a.applyConfig()
}

func (a *App) setMode(mode string) error {
	switch mode {
	case "rule", "global", "direct":
	default:
		return errors.New(i18n.T("未知模式: ", "Unknown mode: ") + mode)
	}
	done := a.begin("mode")
	defer done()
	if _, err := a.settings.Update(func(s *settings.Settings) { s.Mode = mode }); err != nil {
		return err
	}
	c, err := a.core.Client()
	if err != nil {
		return nil // applied at the next start
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if s := a.settings.Get(); !s.DevOff {
		// The mode is part of the rules then, so they are what changes.
		dev := overlayOf(s, a.devEnv())
		if !dev.Empty() {
			if err := a.applyConfig(); err != nil {
				return err
			}
			closeRerouted(ctx, c, dev)
			a.refresh()
			return nil
		}
	}
	if err := c.Patch(ctx, map[string]any{"mode": mode}); err != nil {
		return err
	}
	if mode == "global" {
		a.pointGlobal()
	}
	_ = c.CloseAllConnections(ctx) // old connections keep their old route otherwise
	a.refresh()
	return nil
}

// retake points the system proxy back at the core, on the user's word.
// What the system has now is another program's, so a copy already on disk
// stays: that is what the user had before bowel.
func (a *App) retake() error {
	done := a.begin("retake")
	defer done()
	err := a.pointBack()
	a.refresh()
	return err
}

func (a *App) pointBack() error {
	a.proxyMu.Lock()
	defer a.proxyMu.Unlock()
	if a.isQuitting() {
		return nil // shutdown has put the system proxy back: it stays so
	}
	s := a.settings.Get()
	return a.pointProxy(s, []int{s.MixedPort}, exists(a.proxyBackup()))
}

// updateSettings changes the core's options from the 设置 page: it checks
// them, saves them and reloads the core. The system proxy follows a new
// port or bypass list. On a failure nothing is kept.
// migrateData brings the data of the app's old name, Moho, to dir, the
// first time it runs as bowel ("Moho Dev" to "bowel Dev" in development):
// the settings, providers, stats and the core with its root copy for TUN.
// Renames keep the copy's setuid. What dir has already stays.
func migrateData(dir string) {
	base := filepath.Base(dir)
	if !strings.HasPrefix(base, "bowel") {
		return
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err == nil {
		return // moved before, or new data of its own
	}
	old := filepath.Join(filepath.Dir(dir), "Moho"+strings.TrimPrefix(base, "bowel"))
	ents, err := os.ReadDir(old)
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	for _, e := range ents {
		to := filepath.Join(dir, e.Name())
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		_ = os.Rename(filepath.Join(old, e.Name()), to)
	}
	_ = os.Remove(old) // when all of it moved
}

// appOnly says whether next differs from old only in what the core does
// not see: the language, starting hidden, the menu bar's speed, the theme
// color, the appearance, the skin and the board's borders.
func appOnly(old, next settings.Settings) bool {
	next.Language, next.StartHidden, next.Accent, next.Appearance = old.Language, old.StartHidden, old.Accent, old.Appearance
	next.Skin, next.Borders, next.CommonRegions = old.Skin, old.Borders, old.CommonRegions
	next.TraySpeed = old.TraySpeed
	next.StripRules = old.StripRules
	next.MCPPolicy = old.MCPPolicy                  // the MCP server's, not the core's
	if routingCoreSame(old.Routing, next.Routing) { // a strategy is a selector's position
		next.Routing = old.Routing
	}
	if maps.Equal(mainURLs(old), mainURLs(next)) { // only the main targets are in the config
		next.RouteTargets = old.RouteTargets
	}
	return reflect.DeepEqual(old, next)
}

func (a *App) updateSettings(fn func(*settings.Settings)) error {
	return a.updateSettingsAnd(fn, false)
}

// listenPorts are the ports the core listens on with settings s, the probe's
// aside; 0 for one that is off.
func listenPorts(s settings.Settings) []int {
	return append([]int{s.MixedPort, s.HTTPPort, s.SocksPort, s.ExtPort()}, s.ExtraPortNumbers()...)
}

// updateSettingsAnd is updateSettings; with reapply the core's config is
// built again even when the settings leave it as it was, for a change that
// comes with other providers or rule sets (importConfig).
func (a *App) updateSettingsAnd(fn func(*settings.Settings), reapply bool) error {
	a.setMu.Lock() // one change at a time: each starts from the last
	defer a.setMu.Unlock()
	done := a.begin("settings")
	defer done()
	old := a.settings.Get()
	next := old.Clone()
	fn(&next)
	if err := next.Validate(); err != nil {
		return err
	}
	// A port the core does not listen on yet must be free.
	had := listenPorts(old)
	for _, p := range listenPorts(next) {
		if p == 0 || slices.Contains(had, p) {
			continue // the core's already: it moves between listeners on reload
		}
		if p == a.probePort {
			return fmt.Errorf(i18n.T("端口 %d 已被 bowel 自己占用", "Port %d is taken by bowel itself"), p)
		}
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			return fmt.Errorf(i18n.T("端口 %d 被其他程序占用", "Port %d is taken by another program"), p)
		}
		l.Close()
	}
	// An extra port is given a route group there is. One whose group is
	// deleted later stays in the settings, closed.
	for _, e := range next.ExtraPorts {
		if !slices.Contains(old.ExtraPorts, e) && !slices.ContainsFunc(routes.Build(next.Routing, nil), func(r routes.Route) bool { return r.ID == e.Route }) {
			return fmt.Errorf(i18n.T("端口 %d 指向不存在的路由组: %s", "Port %d points at a route group that doesn't exist: %s"), e.Port, e.Route)
		}
	}
	if _, err := a.settings.Update(func(s *settings.Settings) { *s = next }); err != nil {
		return err
	}
	a.loadStrip() // what node names are shown without, before the interface re-reads them
	i18n.Set(next.Language)
	if appearanceOf(next) != appearanceOf(old) && !a.headless {
		mygo.Theme.SetSource(mygo.ThemeSource(appearanceOf(next)))
	}
	if next.TraySpeed != old.TraySpeed {
		a.mon.Wake() // the rates beside the icon: now, and at their own pace
		a.trayTick()
	}
	if !reapply && appOnly(old, next) { // the core does not care
		var err error
		if !reflect.DeepEqual(old.Routing, next.Routing) {
			err = a.followPolicies()
		}
		a.refresh()
		return err
	}
	if err := a.applyConfig(); err != nil {
		// Back to what worked, so that the next start does not fail too.
		_, _ = a.settings.Update(func(s *settings.Settings) { *s = old })
		i18n.Set(old.Language)
		_ = a.applyConfig()
		return err
	}
	if next.MixedPort != old.MixedPort {
		a.applySystemProxy(old.MixedPort) // points the system at the new port
	} else if !slices.Equal(next.Bypass, old.Bypass) && a.takeoverOn(next) && next.SystemProxy {
		if cur, err := sysproxy.Check(next.MixedPort); err == nil && cur.Ours {
			if err := a.enableProxy(next); err != nil {
				return err
			}
		}
	}
	a.refresh()
	return nil
}

// resetSettings puts the core's options back to Moho's defaults.
func (a *App) resetSettings() error {
	return a.updateSettings((*settings.Settings).ResetCore)
}

// coreCall runs one controller request, with a timeout.
func (a *App) coreCall(timeout time.Duration, fn func(context.Context, *mihomo.Client) error) error {
	c, err := a.core.Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return fn(ctx, c)
}

// updateGeo asks the core to download the geo databases again. It answers
// at once and downloads in the background, then loads them.
func (a *App) updateGeo() error {
	done := a.begin("geo")
	defer done()
	return a.coreCall(2*time.Minute, func(ctx context.Context, c *mihomo.Client) error { return c.UpdateGeo(ctx) })
}

// flushDNS forgets the resolver's cache and its fake-ip mappings.
func (a *App) flushDNS() error {
	return a.coreCall(5*time.Second, func(ctx context.Context, c *mihomo.Client) error {
		if err := c.FlushDNS(ctx); err != nil {
			return err
		}
		return c.FlushFakeIP(ctx)
	})
}

// setOpenAtLogin makes Moho a login item of the user, or not.
func (a *App) setOpenAtLogin(on bool) error {
	if err := setOpenAtLoginOS(on); err != nil {
		return err
	}
	a.mu.Lock()
	a.openAtLogin = openAtLoginOS()
	a.mu.Unlock()
	a.refresh()
	return nil
}

// readOpenAtLogin asks the system again: the user can change it in System
// Settings.
func (a *App) readOpenAtLogin() {
	on := openAtLoginOS()
	a.mu.Lock()
	changed := a.openAtLogin != on
	a.openAtLogin = on
	a.mu.Unlock()
	if changed {
		a.refresh()
	}
}

// revealConfig shows the core's runtime config where files are shown: in
// the Finder on the Mac, in Explorer on Windows, chosen.
func (a *App) revealConfig() error {
	p := a.core.ConfigPath()
	if runtime.GOOS == "windows" {
		// /select opens the folder with the file chosen. Explorer reports
		// success as exit code 1, as it has since forever; anything else
		// is a failure, and no explorer at all is the worst one.
		if err := nowindow.Command("explorer", "/select,"+p).Run(); err != nil {
			var ee *exec.ExitError
			if !errors.As(err, &ee) || ee.ExitCode() != 1 {
				return err
			}
		}
		return nil
	}
	return nowindow.Command("open", "-R", p).Run()
}

func (a *App) route(id string) routes.Route {
	for _, r := range a.routes() {
		if r.ID == id {
			return r
		}
	}
	return routes.Route{}
}

// selectNode pins a route group to node; "" sends it back to the fastest.
func (a *App) selectNode(routeID, node string) error {
	r := a.route(routeID)
	if r.ID == "" || !r.NodeGroups() {
		return errors.New(i18n.T("没有这条路由", "No such route"))
	}
	if a.importedNow() != nil {
		return a.selectMember(routes.Group(r), a.sourceName(node))
	}
	if _, err := a.core.Client(); err != nil {
		return err
	}
	done := a.begin("node:" + routeID)
	defer done()
	err := a.updateSettings(func(s *settings.Settings) { s.Routing.PickNode(routeID, node) })
	if err != nil {
		return err
	}
	a.keepMeasured()
	if node != "" {
		go a.probe(node)
	}
	return nil
}

func (a *App) testRoute(ctx context.Context, routeID string) error {
	r := a.route(routeID)
	if r.ID == "" || !r.NodeGroups() {
		return errors.New(i18n.T("没有这条路由", "No such route"))
	}
	c, err := a.core.Client()
	if err != nil {
		return err
	}
	done := a.begin("test:" + routeID)
	defer done()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Go(func() { a.pingNodes(a.allNodes()) })
	wg.Go(func() { a.testSites(routeID) })
	_, err = c.GroupDelay(ctx, routes.Group(r), a.settings.Get().TargetsOf(routeID)[0].URL, 5*time.Second)
	wg.Wait()
	// The new results are in the providers' lists: read them now.
	a.mu.Lock()
	a.nodesAt = time.Time{}
	a.markTested(routeID)
	a.mu.Unlock()
	a.refresh()
	return err
}

func (a *App) locate(ctx context.Context, node string) error {
	_, err := a.probeErr(ctx, node)
	return err
}

// probe measures a node's exit in the background, showing "locating".
func (a *App) probe(node string) {
	if r := a.loc.Guess(node, display(node)); r.Source == "probe" {
		a.qualitySoon(node) // its address is known; what it is may not be
		return
	}
	_, _ = a.probeErr(context.Background(), node)
}

func (a *App) probeErr(ctx context.Context, node string) (locate.Result, error) {
	c, err := a.core.Client()
	if err != nil {
		return locate.Result{}, err
	}
	a.loc.SetSource(a.settings.Get().LocateURL)
	a.mu.Lock()
	a.locating[node] = true
	a.mu.Unlock()
	a.emit()
	r, err := a.loc.Probe(ctx, c, a.probePort, node)
	a.mu.Lock()
	delete(a.locating, node)
	if node == "DIRECT" && err == nil {
		a.you = &r
	}
	a.mu.Unlock()
	a.emit()
	if err == nil {
		a.qualitySoon(node)
	}
	return r, err
}

// qualitySoon asks the address services about the node's exit, in the
// background, one lookup at a time: what an address is changes slowly, and
// the answers are kept in a file.
func (a *App) qualitySoon(node string) {
	if node == "" || node == "DIRECT" {
		return // the user's own address is not the exit's
	}
	st := a.settings.Get()
	if !st.IPQuality {
		return // the setting is off: nothing is asked about any address
	}
	a.qual.SetSource(st.IPInfoURL) // the service the settings name, now
	ip := a.loc.Guess(node, display(node)).IP
	if ip == "" {
		return // no probe has said where it exits yet
	}
	if _, ok := a.qual.Get(ip); ok {
		return
	}
	a.mu.Lock()
	if a.qualifying {
		a.mu.Unlock()
		return
	}
	a.qualifying = true
	a.mu.Unlock()
	go func() {
		defer func() {
			a.mu.Lock()
			a.qualifying = false
			a.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if _, err := a.qual.Lookup(ctx, ip); err != nil {
			log.Println("ipinfo:", err)
			return
		}
		a.emit()
	}()
}

// youEvery is how often the user's own place is measured again. It is worth
// repeating: with TUN in effect a wrong reading (taken while the tunnel was
// still settling) stays wrong for as long as it is trusted, and the map puts
// "you" wherever it says.
const youEvery = 10 * time.Minute

// locateExits measures the user's own place and the nodes routes use now.
func (a *App) locateExits(force bool) {
	a.mu.Lock()
	you := a.you
	a.mu.Unlock()
	if force || you == nil || time.Since(you.At) > youEvery {
		_, _ = a.probeErr(context.Background(), "DIRECT")
	}
	a.mu.Lock()
	px := a.proxies
	a.mu.Unlock()
	seen := map[string]bool{}
	for _, r := range a.routes() {
		if r.Direct {
			continue
		}
		if n := currentNode(px, r); n != "" && !seen[n] {
			seen[n] = true
			if force {
				_, _ = a.probeErr(context.Background(), n)
			} else {
				a.probe(n)
			}
		}
	}
	// The exit's address may be known from a cached probe, with nothing said
	// about it yet.
	a.qualitySoon(currentNode(px, a.route(routes.Default)))
}

// currentNode is the node a route uses now, following the groups its
// selector points through. "" if it has none: REJECT, in a range or list
// with nothing in it.
func currentNode(px map[string]mihomo.Proxy, r routes.Route) string {
	g, ok := px[routes.Group(r)]
	if !ok {
		return ""
	}
	if g.Now == "" {
		return routes.Group(r) // a rule of an imported config may name a node
	}
	n := g.Now
	for i := 0; i < 4 && routes.Internal(r, n); i++ {
		n = px[n].Now
	}
	for i := 0; i < 16 && px[n].Now != ""; i++ { // an imported group's members may be groups
		n = px[n].Now
	}
	if routes.Internal(r, n) || n == "REJECT" {
		return ""
	}
	return n
}

// noticeSwitches records a route's automatic group moving off a failed node.
// What is kept here is the core's own names: how they read goes through the
// user's name rules when the note is shown (switchFor), where a.mu is not
// held — rendering here would deadlock, and a rule changed in between would
// be lost.
func (a *App) noticeSwitches() {
	s := a.settings.Get()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.routes() {
		if r.Direct {
			continue
		}
		n := currentNode(a.proxies, r)
		prev := a.lastNode[r.ID]
		a.lastNode[r.ID] = n
		if now := a.proxies[routes.Group(r)].Now; prev == "" || prev == n || n == "" || (now != routes.Auto(r) && now != routes.Backup(r)) {
			continue
		}
		targets := s.TargetsOf(r.ID)
		if len(targets) == 0 {
			continue
		}
		if h := a.proxies[prev].HistoryOf(targets[0].URL); len(h) > 0 && h[len(h)-1].Delay == 0 {
			note := &switchNote{route: r.ID, from: prev, to: n, at: time.Now()}
			a.switched = note
			a.hideSwitchAt(note)
		}
	}
}

// hideSwitchAt takes the note out when its time is up, rather than at the
// next refresh, up to two seconds later. The pointer says which note this
// timer is about: a newer switch has a timer of its own.
func (a *App) hideSwitchAt(n *switchNote) {
	time.AfterFunc(switchShow, func() {
		a.mu.Lock()
		gone := a.switched == n
		if gone {
			a.switched = nil
		}
		a.mu.Unlock()
		if gone {
			a.emit() // outside the lock: emit and snapshot take a.mu
		}
	})
}

// ---- Providers ----

func (a *App) detectProvider(input string) (string, error) {
	k, err := provider.Detect(input)
	return string(k), err
}

func (a *App) addProvider(ctx context.Context, input string) (ProviderView, error) {
	done := a.begin("add")
	defer done()
	p, err := a.providers.Add(ctx, a.http, input)
	if err != nil {
		return ProviderView{}, err
	}
	return a.afterProviderAdded(p)
}

func (a *App) addProviderFile(ctx context.Context) (*ProviderView, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: i18n.T("添加 Provider", "Add Provider"),
		Filters: []mygo.FileFilter{{Name: i18n.T("Clash 配置或节点列表", "Clash config or node list"), Extensions: []string{"yaml", "yml", "txt", "conf"}}},
	})
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	done := a.begin("add")
	defer done()
	p, err := a.providers.AddFile(paths[0])
	if err != nil {
		return nil, err
	}
	v, err := a.afterProviderAdded(p)
	return &v, err
}

// addProviderFileAs adds a local file under name, the name a config has for
// a proxy-provider that is a local file: the config can be used from then on.
func (a *App) addProviderFileAs(ctx context.Context, name string) error {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: fmt.Sprintf(i18n.T("选择「%s」的文件", "Choose the file of %s"), name),
		Filters: []mygo.FileFilter{{Name: i18n.T("Clash 配置或节点列表", "Clash config or node list"), Extensions: []string{"yaml", "yml", "txt", "conf"}}},
	})
	if err != nil || len(paths) == 0 {
		return err
	}
	return a.addFileAs(paths[0], name)
}

func (a *App) addFileAs(path, name string) error {
	done := a.begin("add")
	defer done()
	p, err := a.providers.AddFile(path)
	if err != nil {
		return err
	}
	if p, err = a.providers.Rename(p.ID, name); err != nil {
		_ = a.providers.Delete(p.ID)
		return err
	}
	_, err = a.afterProviderAdded(p)
	return err
}

// reloadProvider reads a local file's Provider again from the file it came
// from. That is a file chosen on this machine; when there is none to go
// back to (pasted text, a configuration imported from elsewhere), it asks.
func (a *App) reloadProvider(ctx context.Context, id string) error {
	p, ok := a.providers.Get(id)
	if !ok {
		return fmt.Errorf(i18n.T("没有这个 Provider: %s", "No such Provider: %s"), id)
	}
	path := ""
	if p.Source == "" {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: mygo.CallerWindow(ctx), Title: fmt.Sprintf(i18n.T("选择「%s」的文件", "Choose the file of %s"), p.Name),
			Filters: []mygo.FileFilter{{Name: i18n.T("Clash 配置或节点列表", "Clash config or node list"), Extensions: []string{"yaml", "yml", "txt", "conf"}}},
		})
		if err != nil || len(paths) == 0 {
			return err
		}
		path = paths[0]
	}
	return a.reloadFile(id, path)
}

func (a *App) reloadFile(id, path string) error {
	done := a.begin("provider:" + id)
	defer done()
	if _, err := a.providers.Reload(id, path); err != nil {
		a.emit()
		return err
	}
	err := a.applyConfig()
	go a.healthCheck(id)
	return err
}

func (a *App) afterProviderAdded(p provider.Provider) (ProviderView, error) {
	if err := a.applyConfig(); err != nil {
		_ = a.providers.Delete(p.ID)
		_ = a.applyConfig()
		return ProviderView{}, fmt.Errorf(i18n.T("内核无法读取这个 Provider：%w", "The core can't read this Provider: %w"), err)
	}
	go a.healthCheck(p.ID)
	return a.providerView(p), nil
}

// healthCheck tests a provider's nodes and locates the ones in use.
func (a *App) healthCheck(id string) {
	if c, err := a.core.Client(); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = c.HealthCheck(ctx, "p-"+id)
		cancel()
	}
	a.refresh()
	a.locateExits(false)
}

func (a *App) updateProvider(ctx context.Context, id string) error {
	done := a.begin("provider:" + id)
	defer done()
	if _, err := a.providers.Update(ctx, a.http, id); err != nil {
		a.emit()
		return err
	}
	err := a.applyConfig()
	go a.healthCheck(id)
	return err
}

func (a *App) updateAll(ctx context.Context) error {
	done := a.begin("providers")
	defer done()
	var errs []error
	for _, p := range a.providers.List() {
		if p.Kind == provider.Subscription && p.Enabled {
			if _, err := a.providers.Update(ctx, a.http, p.ID); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", p.Name, err))
			}
		}
	}
	if err := a.applyConfig(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (a *App) autoUpdate() {
	ids := a.providers.Due(time.Now())
	for _, id := range ids {
		if _, err := a.providers.Update(context.Background(), a.http, id); err != nil {
			log.Println("auto update:", err)
		}
	}
	if len(ids) > 0 {
		_ = a.applyConfig()
	}
	a.autoUpdateRuleSets()
}

func (a *App) setProviderEnabled(id string, on bool) error {
	done := a.begin("provider:" + id)
	defer done()
	if _, err := a.providers.SetEnabled(id, on); err != nil {
		return err
	}
	return a.applyConfig() // a disabled source's routing stops, and comes back with it
}

func (a *App) renameProvider(id, name string) error {
	p, err := a.providers.Rename(id, name)
	// A config's local file is found by its name: one in use may have just
	// gained or lost its file.
	if err == nil && p.Kind == provider.File && a.settings.Get().ImportedRouting != "" {
		return a.applyConfig()
	}
	a.emit()
	return err
}

func (a *App) setAutoUpdate(id string, hours int) error {
	_, err := a.providers.SetAutoUpdate(id, hours)
	a.emit()
	return err
}

func (a *App) deleteProvider(id string) error {
	done := a.begin("provider:" + id)
	defer done()
	if err := a.providers.Delete(id); err != nil {
		return err
	}
	if a.settings.Get().ImportedRouting == id {
		if _, err := a.settings.Update(func(s *settings.Settings) { s.ImportedRouting = "" }); err != nil {
			return err
		}
	}
	return a.applyConfig()
}

// ---- The user's rules ----

// addRule sends r's traffic to its route, in place of an older rule for
// the same traffic. Connections it matches that went elsewhere are closed,
// so that the change shows at once: their apps connect again, by the new
// way. It returns how many were closed.
func (a *App) addRule(r routes.UserRule) (int, error) {
	if err := a.rulesEditable(); err != nil {
		return 0, err
	}
	if a.route(r.Route).ID == "" {
		return 0, errors.New(i18n.T("没有这条路由", "No such route"))
	}
	if err := a.updateSettings(func(s *settings.Settings) {
		s.Routing.Claim(s.Rules, r)
		s.Rules = routes.Upsert(s.Rules, r)
	}); err != nil {
		return 0, err
	}
	return a.reroute(r, r.Route), nil
}

// removeRule takes out the rule for r's traffic, and closes the
// connections it matched.
func (a *App) removeRule(r routes.UserRule) (int, error) {
	if err := a.updateSettings(func(s *settings.Settings) { s.Rules = routes.Remove(s.Rules, r) }); err != nil {
		return 0, err
	}
	return a.reroute(r, ""), nil
}

// reroute closes the open connections r matches that are not on route
// keep. A failure only leaves them open: the rule holds for new ones.
func (a *App) reroute(r routes.UserRule, keep string) int {
	return a.rerouteBy(r.Matches, keep)
}

// closeUnmatched closes the connections no rule matched, so that a change
// of where they go shows. It reports how many it closed.
func (a *App) closeUnmatched() int {
	c, err := a.core.Client()
	if err != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap, err := c.Connections(ctx)
	if err != nil {
		return 0
	}
	var ids []string
	for _, cn := range snap.Connections {
		if cn.Rule == "Match" {
			ids = append(ids, cn.ID)
		}
	}
	if len(ids) > 0 && a.closeConns(ids) == nil {
		a.mon.Wake()
		return len(ids)
	}
	return 0
}

// closeRoute closes the connections on route group id, so that a change of
// where it leaves shows. It reports how many it closed.
func (a *App) closeRoute(id string) int {
	if id == routes.Default {
		return a.closeUnmatched()
	}
	c, err := a.core.Client()
	if err != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap, err := c.Connections(ctx)
	if err != nil {
		return 0
	}
	var ids []string
	for _, cn := range snap.Connections {
		if len(cn.Chains) > 0 && a.routeOfGroup(cn.Chains[len(cn.Chains)-1], cn.Rule) == id {
			ids = append(ids, cn.ID)
		}
	}
	if len(ids) > 0 && a.closeConns(ids) == nil {
		a.mon.Wake()
		return len(ids)
	}
	return 0
}

// rerouteBy is reroute for whatever match catches.
func (a *App) rerouteBy(match func(host, ip string) bool, keep string) int {
	c, err := a.core.Client()
	if err != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap, err := c.Connections(ctx)
	if err != nil {
		return 0
	}
	var ids []string
	for _, cn := range snap.Connections {
		host := cn.Metadata.Host
		if host == "" {
			host = cn.Metadata.SniffHost
		}
		if !match(host, cn.Metadata.DestinationIP) {
			continue
		}
		group := ""
		if len(cn.Chains) > 0 {
			group = cn.Chains[len(cn.Chains)-1]
		}
		if keep == "" || a.routeOfGroup(group, cn.Rule) != keep {
			ids = append(ids, cn.ID)
		}
	}
	if len(ids) > 0 && a.closeConns(ids) == nil {
		a.mon.Wake()
		return len(ids)
	}
	return 0
}

// ---- Activity ----

// routeOfGroup maps a mihomo group name (or DIRECT, REJECT) to a route id.
func (a *App) routeOfGroup(group, rule string) string {
	for _, r := range a.routes() {
		if (!r.Direct || r.OwnExit()) && (group == routes.Group(r) || routes.Internal(r, group)) {
			return r.ID
		}
	}
	if group == "REJECT" { // a route's empty range shows as the route's group
		return routes.Reject
	}
	if group != "DIRECT" {
		return ""
	}
	if rule == "Match" { // what no rule matched, when the default route goes direct
		if a.route(routes.Default).Policy.Direct {
			return routes.Default
		}
		return ""
	}
	if rule != "" {
		for _, r := range a.routes() {
			if r.Direct && !r.Reject && r.ID != routes.Default && !r.OwnExit() {
				return r.ID
			}
		}
	}
	return ""
}

// display is a node's name without Moho's provider marker.
func display(node string) string {
	if i := strings.Index(node, core.NodeSep); i >= 0 {
		return node[:i]
	}
	return node
}

// label is what people read for a node: its own name, but a node that is
// all of a pasted link provider goes by the provider's name. That name was
// taken from the link's #fragment, so renaming the provider renames the node
// for people; mihomo keeps the node's name, and with it the choices made.
// provNodes is the providers' nodes, as a.provNodes.
func (a *App) label(provNodes map[string][]string, node string) string {
	id := providerOf(node)
	if len(provNodes[id]) == 1 {
		if p, ok := a.providers.Get(id); ok && p.Kind == provider.Link {
			return trimFlag(a.stripped(p.Name))
		}
	}
	return trimFlag(display(node))
}

// trimFlag drops a flag emoji leading a name ("🇺🇸美国纽约" is "美国纽约"):
// the interface draws the place's flag before the name itself. A name that
// is nothing but a flag stays.
func trimFlag(s string) string {
	t := strings.TrimLeftFunc(s, func(r rune) bool {
		return r >= 0x1F1E6 && r <= 0x1F1FF || unicode.IsSpace(r)
	})
	if t == "" {
		return s
	}
	return t
}

// display is a node's label, without what the user's name rules match; not
// with a.mu held.
func (a *App) display(node string) string {
	a.mu.Lock()
	pn := a.provNodes
	a.mu.Unlock()
	return a.stripped(a.label(pn, node))
}

// stripped is s without what the settings' name rules match. The rules are
// written from the name as shown, and the shown name has none of the
// zero-width characters the core's name is full of: they go first, or
// "官方" could never match "官\u200b方" and "HKG" not "H\u200bK\u200bG". The
// passes then repeat until s stops changing: a rule may only match once
// another has run — the one dropping the zero-width characters, say — and
// that one is only first in the list by luck. The name the core goes by
// keeps what it was given, so choices, rules and the running config are
// unchanged; only what the interface shows drops it.
func (a *App) stripped(s string) string {
	rules := a.strip.Load()
	if rules == nil || len(*rules) == 0 {
		return s
	}
	s = geo.StripInvisible(s)
	for range 4 {
		next := s
		for _, re := range *rules {
			if re != nil {
				next = re.ReplaceAllString(next, "")
			}
		}
		if next == s {
			return s
		}
		s = next
	}
	return s
}

// nodeShown is a node as the news lines name it: "[Provider]Node", with both
// names without what the user's rules match. A node of no provider, one whose
// provider is gone, and a link provider's only node — which already goes by
// the provider's name — are their own name. It takes no lock: provNodes is the
// caller's copy, as a snapshot holds it, so it can run outside a.mu.
func (a *App) nodeShown(provNodes map[string][]string, node string) string {
	shown := a.stripped(a.label(provNodes, node)) // as NodeView.Display is made
	if shown == "" {
		shown = "…" // a rule took the whole name: the sentence still reads
	}
	id := providerOf(node)
	if id == "" {
		return shown
	}
	p, ok := a.providers.Get(id)
	if !ok {
		return shown // the provider is gone: the node alone is truer than a bare id
	}
	name := trimFlag(a.stripped(p.Name))
	if name == "" || name == shown {
		return shown
	}
	return "[" + name + "]" + shown
}

// providerShown is a provider's name as the news lines say it, by id: the id
// itself when the provider is gone, as providerTitle answers.
func (a *App) providerShown(id string) string {
	if p, ok := a.providers.Get(id); ok {
		if name := trimFlag(a.stripped(p.Name)); name != "" {
			return name
		}
	}
	return id
}

// loadStrip reads what the settings say node names are shown without, as
// the compiled rules stripped works from. Called when the app is made and
// whenever the setting changes; a rule that does not compile is skipped,
// since the settings refuse to save one.
func (a *App) loadStrip() {
	res, _ := settings.CompileRules(a.settings.Get().StripRules)
	a.strip.Store(&res)
}

func providerOf(node string) string {
	if i := strings.Index(node, core.NodeSep); i >= 0 {
		return node[i+len(core.NodeSep):]
	}
	return ""
}

// groupTitle is a chain's step as the interface shows it: a route's group
// by the route's title, anything else (a node, DIRECT) as it is.
func (a *App) groupTitle(name string) string {
	for _, r := range a.routes() {
		switch name {
		case routes.Group(r):
			return routes.Title(r)
		case routes.Auto(r):
			return routes.Title(r) + i18n.T("·自动", " · Auto")
		case routes.Backup(r):
			return routes.Title(r) + i18n.T("·备用", " · Backup")
		}
	}
	return name
}

func (a *App) isBackupStep(name string) bool {
	for _, r := range a.routes() {
		if strings.HasPrefix(name, routes.Backup(r)+"·") {
			return true
		}
	}
	return false
}

func (a *App) convConn(c monitor.Conn) Conn {
	group := ""
	if len(c.Chains) > 0 {
		group = c.Chains[len(c.Chains)-1]
	}
	v := Conn{
		ID: c.ID, App: c.App, Process: c.Process, Host: c.Host, IP: c.IP, Port: c.Port, Network: c.Network,
		Rule: c.Rule, Payload: c.Payload, Route: a.routeOfGroup(group, c.Rule),
		Down: c.Down, Up: c.Up, DownRate: c.DownRate, UpRate: c.UpRate, Start: c.Start, End: c.End,
		Status: c.Status, Error: c.Error,
	}
	for i := len(c.Chains) - 1; i >= 0; i-- {
		if a.isBackupStep(c.Chains[i]) {
			continue // one node's own group: says nothing the node doesn't
		}
		v.Chain = append(v.Chain, a.groupTitle(a.display(c.Chains[i])))
	}
	if c.Status == "blocked" {
		v.Chain = []string{routes.Title(a.route(routes.Reject))}
	}
	if len(c.Chains) > 1 || (len(c.Chains) == 1 && c.Chains[0] != "DIRECT" && group != c.Chains[0]) {
		v.Node = a.display(c.Chains[0])
	}
	return v
}

func (a *App) connections() []Conn {
	src := a.mon.Conns()
	out := make([]Conn, 0, len(src))
	for _, c := range src {
		out = append(out, a.convConn(c))
	}
	return out
}

func (a *App) closeConns(ids []string) error {
	c, err := a.core.Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var errs []error
	for _, id := range ids {
		if err := c.CloseConnection(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (a *App) logs(after int64) []LogLine {
	src := a.mon.Logs(after)
	out := make([]LogLine, len(src))
	for i, l := range src {
		out[i] = LogLine{Seq: l.Seq, Time: l.Time, Level: l.Level, Text: l.Text}
	}
	return out
}

// ---- Tick ----

func (a *App) onTick() {
	t := a.buildTick()
	a.mu.Lock()
	a.lastTick = t
	a.mu.Unlock()
	if u := a.uiRef(); u != nil {
		u.setTick(t)
	}
	a.trayTick()
}

func (a *App) tick() Tick {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastTick
}

func (a *App) buildTick() Tick {
	n := a.mon.Now()
	t := Tick{Down: n.Down, Up: n.Up, ByRoute: map[string]int64{}, Active: n.Active,
		TodayDown: n.TodayDown, TodayUp: n.TodayUp, MonthDown: n.MonthDown, Apps: []AppRate{}, Events: []Event{}}
	for g, v := range n.ByGroup {
		// A DIRECT group can be the direct route or the default's DIRECT; count it as direct.
		id := a.routeOfGroup(g, "x")
		if id != "" {
			t.ByRoute[id] += v
		}
	}
	// Today's share per route: the bytes since the last tick, as the
	// monitor counted them, however long ago that was.
	day := time.Now().Format("2006-01-02")
	a.mu.Lock()
	if a.shareDay != day {
		a.shareDay, a.routeBytes = day, map[string]int64{}
	}
	for g, v := range n.GroupBytes {
		if d := v - a.groupSeen[g]; d > 0 {
			if id := a.routeOfGroup(g, "x"); id != "" {
				a.routeBytes[id] += d
			}
		}
	}
	a.groupSeen = n.GroupBytes
	a.mu.Unlock()

	// Each app's busiest connection names its host: the fastest, the
	// oldest of equals, so that it holds still from one tick to the next.
	type top struct {
		rate  int64
		start time.Time
		id    string
	}
	apps := map[string]*AppRate{}
	best := map[string]top{}
	for _, c := range a.mon.Rates() {
		r := apps[c.App]
		if r == nil {
			r = &AppRate{App: c.App}
			apps[c.App] = r
		}
		r.Down += c.DownRate
		r.Up += c.UpRate
		r.Conns++
		b, seen := best[c.App]
		if !seen || c.DownRate > b.rate || c.DownRate == b.rate &&
			(c.Start.Before(b.start) || c.Start.Equal(b.start) && c.ID > b.id) {
			best[c.App] = top{c.DownRate, c.Start, c.ID}
			r.Host, r.Route = c.Host, a.routeOfGroup(c.Group, c.Rule)
		}
	}
	for _, r := range apps {
		t.Apps = append(t.Apps, *r)
	}
	sort.Slice(t.Apps, func(i, j int) bool {
		if t.Apps[i].Down != t.Apps[j].Down {
			return t.Apps[i].Down > t.Apps[j].Down
		}
		return t.Apps[i].Conns > t.Apps[j].Conns
	})
	if len(t.Apps) > 8 {
		t.Apps = t.Apps[:8]
	}
	for _, e := range a.mon.Events(30) {
		group := ""
		if len(e.Chains) > 0 {
			group = e.Chains[len(e.Chains)-1]
		}
		ev := Event{Time: e.Time, Text: e.App + " → " + e.Host, Route: a.routeOfGroup(group, "x"), Failed: e.Failed}
		if len(e.Chains) > 1 {
			ev.Node = a.display(e.Chains[0])
		}
		t.Events = append(t.Events, ev)
	}
	return t
}

// ---- Snapshot ----

// emit sends the snapshot when it changed, at most every 100 ms.
func (a *App) emit() {
	a.mu.Lock()
	if a.emitQueued {
		a.mu.Unlock()
		return
	}
	a.emitQueued = true
	a.mu.Unlock()
	time.AfterFunc(100*time.Millisecond, func() {
		a.mu.Lock()
		a.emitQueued = false
		a.mu.Unlock()
		s := a.snapshot()
		b, _ := json.Marshal(s)
		h := sha256.Sum256(b)
		a.mu.Lock()
		same := h == a.lastHash
		a.lastHash = h
		a.mu.Unlock()
		if !same {
			if u := a.uiRef(); u != nil {
				u.setSnapshot(s)
			}
			a.refreshTray(s)
		}
	})
}

func (a *App) snapshot() Snapshot {
	s := a.settings.Get()
	cs := a.core.Status()
	a.mu.Lock()
	sys, proxyErr, px, provNodes, you := a.sys, a.proxyErr, a.proxies, a.provNodes, a.you
	pings, sites := a.pings, a.sites
	openAtLogin := a.openAtLogin
	gwp, gwErr := a.gwProbe, a.gwErr
	pending := make([]string, 0, len(a.pending))
	for k := range a.pending {
		pending = append(pending, k)
	}
	locating := copyMap(a.locating)
	routeBytes := copyMap(a.routeBytes)
	a.mu.Unlock()
	slices.Sort(pending)

	on := a.takeoverOn(s)
	// After the lock: how a node reads goes through the name rules, and the
	// note is dropped here when it no longer tells what happens.
	sw := a.switchFor(px, provNodes, cs, s, on)
	snap := Snapshot{
		Update: a.updateView(), UpdateInfo: a.updateInfo(),
		Mode: s.Mode, Pending: pending, Version: mygo.App.Version(), RuleCount: a.ruleCount(s),
		Settings: s, ConfigPath: a.core.ConfigPath(), OpenAtLogin: openAtLogin, RuleSets: a.ruleSetViews(s),
		ControllerTCP: a.core.TCPAddr(),
		CanUndoImport: a.canUndoImport(),
		Core:          Core{State: string(cs.State), Version: cs.Version, Error: cs.Error},
		Routes:        []RouteView{},
		Nodes:         []NodeView{},
		Providers:     []ProviderView{},
	}

	// Takeover, as it really is.
	tk := Takeover{On: on, Port: s.MixedPort, Lan: s.AllowLan, Bypass: s.Bypass}
	tk.System = Entry{Wanted: s.SystemProxy, Status: "off"}
	if on && s.SystemProxy {
		switch {
		case sys.Ours:
			tk.System.Status = "on"
		case proxyErr != "": // bowel's own failure, whatever the system has
			tk.System.Status, tk.System.Detail = "error", proxyErr
		case sys.On:
			tk.System.Status, tk.System.Detail = "hijacked", fmt.Sprintf(i18n.T("现在指向 %s:%d", "Now points at %s:%d"), sys.Host, sys.Port)
		default:
			tk.System.Status, tk.System.Detail = "hijacked", i18n.T("系统代理被关掉了", "The system proxy was turned off")
		}
	}
	fault := ""
	if f := a.tunFault.Load(); f != nil {
		fault = *f
	}
	tk.Tun = tunEntry(s.Tun, on, osTun.installed(a.tunBin) && !a.tunGone.Load(), fault, cs.State)
	if s.AllowLan {
		tk.LanAddr = lanAddr(s.MixedPort)
	}
	tk.Gateway = gatewayView(s, on, tk.Tun.Status, a.gw.System().Ready(), a.gw.Awake(), gwp, gwErr)
	snap.Takeover = tk
	snap.Dev = a.devReport(s, tk.Tun.Status == "on")
	snap.Tools = a.toolsView()
	snap.Sources = a.sourcesView()

	// Nodes, in their providers' order.
	mains := mainURLs(s)
	// What the quality answers are checked against is the service the
	// settings name now: another one's word is asked again, not shown.
	a.qual.SetSource(s.IPInfoURL)
	addrs := map[string]map[string]string{}
	view := func(name string) NodeView {
		p := px[name]
		v := NodeView{Name: name, Display: a.stripped(a.label(provNodes, name)), Provider: providerOf(name), Type: p.Type, UDP: p.UDP, History: []int{}}
		if id := v.Provider; id != "" {
			if _, ok := addrs[id]; !ok {
				addrs[id] = a.providers.Addrs(id)
			}
			v.Address = addrs[id][display(name)]
		}
		v.Routes = make(map[string]NodeTest, len(mains))
		for id, u := range mains {
			v.Routes[id] = nodeTest(p.HistoryOf(u))
		}
		t := v.Routes[routes.Default]
		v.Delay, v.Tested = t.Delay, t.Tested
		if t.History != nil {
			v.History = t.History
		}
		v.Ping = pings[name].ms
		v.Where = located(a.loc.Guess(name, display(name)))
		if locating[name] {
			v.Where.Source = "locating"
		}
		if q, ok := a.qual.Get(v.Where.IP); ok && s.IPQuality {
			v.Quality = &q
		}
		return v
	}
	for _, p := range a.providers.List() {
		if !p.Enabled {
			continue
		}
		for _, n := range provNodes[p.ID] {
			snap.Nodes = append(snap.Nodes, view(n))
		}
	}

	// Routes.
	im := a.importedNow()
	snap.Imported = a.importedView(s, im, px, provNodes)
	var total int64
	for _, v := range routeBytes {
		total += v
	}
	for _, r := range a.routes() {
		if r.ID == routes.Default && r.Policy.Direct && s.Mode == "global" {
			r.Direct = false // global mode has no rules to miss: it goes by the nodes
		}
		rv := RouteView{ID: r.ID, Name: routes.Title(r), Color: r.Color, Summary: routes.Describe(r), Rules: len(r.Rules), Direct: r.Direct, Reject: r.Reject}
		if r.ID == routes.Default {
			rv.Rules = 1
		}
		for _, u := range s.Rules {
			if u.Route == r.ID && im == nil { // the user's rules are paused under an imported config
				rv.Mine++
			}
		}
		rv.Rules += rv.Mine
		if im != nil {
			rv.Group = routes.Group(r)
		}
		if !r.Direct {
			rv.Policy, rv.Auto = r.Policy, r.Policy.Mode == routes.ModeAuto
			rv.Node = a.listedNode(currentNode(px, r), provNodes)
			if rv.Node == "DIRECT" {
				rv.Node = ""
			}
			rv.Targets = s.TargetsOf(r.ID)
			rv.Main, rv.Sites = rv.Targets[0], siteViews(rv.Targets[1:], sites[r.ID], rv.Node)
			rv.Broken = brokenWhy(r.Policy, snap.Nodes, r.ID)
		}
		if total > 0 {
			rv.Share = float64(routeBytes[r.ID]) / float64(total)
		}
		snap.Routes = append(snap.Routes, rv)
	}
	// What the rest fall through to is listed after them.
	if i := slices.IndexFunc(snap.Routes, func(r RouteView) bool { return r.ID == routes.Default }); i >= 0 {
		last := snap.Routes[i]
		snap.Routes = append(slices.Delete(snap.Routes, i, i+1), last)
	}

	// Exit and the user's place.
	if def := a.route(routes.Default); !def.Direct || def.Policy.Direct && s.Mode == "global" {
		if n := a.listedNode(currentNode(px, def), provNodes); n != "" && n != "DIRECT" {
			snap.Exit = &Exit{Node: n, Where: view(n).Where}
		}
	}
	if you != nil {
		l := located(*you)
		snap.You = &l
	}

	// Providers.
	for _, p := range a.providers.List() {
		v := a.providerView(p)
		v.Nodes = len(provNodes[p.ID])
		snap.Providers = append(snap.Providers, v)
	}

	// The one light, and the news line.
	snap.State, snap.Issue = a.judge(on, s, cs, tk, snap, sw)
	return snap
}

// switchSay is a switch as the news line says it: the names shown, not the
// core's.
type switchSay struct {
	title, from, to string
	at              time.Time
}

// switchFor is the switch, if the news line should still say it: nil once its
// time is up, the route is on another node, the node it left answers again,
// or traffic is not being taken over. The names are rendered here rather than
// where the note is made, which runs under a.mu.
func (a *App) switchFor(px map[string]mihomo.Proxy, provNodes map[string][]string, cs core.Status, s settings.Settings, on bool) *switchSay {
	a.mu.Lock()
	n := a.switched
	if n == nil {
		a.mu.Unlock()
		return nil
	}
	note := *n // a copy: a newer switch may replace the pointer while this renders
	a.mu.Unlock()
	if !on || cs.State != core.Running || switchExpired(&note, time.Now()) || a.switchStale(&note, px, s) {
		a.dropSwitch(n)
		return nil
	}
	return &switchSay{
		title: routes.Title(a.route(note.route)),
		from:  a.nodeShown(provNodes, note.from),
		to:    a.nodeShown(provNodes, note.to),
		at:    note.at,
	}
}

// dropSwitch forgets n, unless a newer switch replaced it meanwhile.
func (a *App) dropSwitch(n *switchNote) {
	a.mu.Lock()
	if a.switched == n {
		a.switched = nil
	}
	a.mu.Unlock()
}

func switchExpired(n *switchNote, now time.Time) bool { return now.Sub(n.at) >= switchShow }

// switchStale says whether the note no longer tells what happens: the route is
// on another node, its group was given a node by hand, or the node it left
// answers again. The group check mirrors noticeSwitches'.
func (a *App) switchStale(n *switchNote, px map[string]mihomo.Proxy, s settings.Settings) bool {
	r := a.route(n.route)
	if r.ID == "" || r.Direct {
		return true
	}
	g, ok := px[routes.Group(r)]
	if !ok || (g.Now != routes.Auto(r) && g.Now != routes.Backup(r)) {
		return true // a fixed pick, or another strategy: the user chose
	}
	if currentNode(px, r) != n.to {
		return true // the group moved again
	}
	if targets := s.TargetsOf(r.ID); len(targets) > 0 {
		if h := px[n.from].HistoryOf(targets[0].URL); len(h) > 0 && h[len(h)-1].Delay > 0 {
			return true // the node it left works again
		}
	}
	return false
}

func (a *App) judge(on bool, s settings.Settings, cs core.Status, tk Takeover, snap Snapshot, sw *switchSay) (State, *Issue) {
	if !on {
		return StateOff, nil
	}
	if cs.State == core.Failed {
		return StateError, &Issue{Level: "error", Text: i18n.T("内核未运行", "The core isn't running"), Detail: firstLine(cs.Error)}
	}
	if cs.State == core.Running && cs.Error != "" {
		return StateError, &Issue{Level: "error", Text: i18n.T("代理端口打不开", "The proxy port can't be opened"), Detail: firstLine(cs.Error)}
	}
	if tk.System.Status == "error" {
		return StateError, &Issue{Level: "error", Text: i18n.T("没能接管系统代理", "Couldn't take over the system proxy"), Detail: tk.System.Detail, Action: "retake", Label: i18n.T("重新接管", "Take over again")}
	}
	if tk.System.Status == "hijacked" {
		return StateError, &Issue{Level: "error", Text: i18n.T("系统代理被其他程序改掉了", "Another program changed the system proxy"), Detail: i18n.T("网页流量现在没有经过 bowel。", "Web traffic isn't going through bowel now."), Action: "retake", Label: i18n.T("重新接管", "Take over again")}
	}
	if g := tk.Gateway; g.Status == "error" {
		return StateDegraded, &Issue{Level: "warn", Text: i18n.T("网关模式没有生效", "Gateway mode isn't working"), Detail: g.Detail}
	}
	if len(snap.Nodes) == 0 && s.Mode != "direct" {
		return StateDegraded, &Issue{Level: "warn", Text: i18n.T("还没有节点，流量正在直连", "No nodes yet; traffic is going direct"), Action: "providers", Label: i18n.T("添加提供商", "Add Provider")}
	}
	if sw != nil && time.Since(sw.at) < switchShow {
		return StateDegraded, &Issue{Level: "warn", Text: fmt.Sprintf(i18n.T("%s：%s不可用，已经切换到%s", "%s: %s is down, switched to %s"), sw.title, sw.from, sw.to), Action: "activity", Label: i18n.T("查看", "View")}
	}
	var broken []RouteView
	for _, r := range snap.Routes {
		if r.Broken != "" && (s.Mode == "rule" || r.ID == routes.Default) {
			broken = append(broken, r)
		}
	}
	if len(broken) > 0 {
		return StateDegraded, a.brokenIssue(broken[0], snap.Nodes, len(broken)-1)
	}
	for _, r := range snap.Routes {
		if r.Direct || r.Node == "" {
			continue
		}
		for _, n := range snap.Nodes {
			if n = n.On(r.ID); n.Name == r.Node && n.Tested && n.Delay == 0 && (s.Mode == "rule" || r.ID == routes.Default) {
				return StateDegraded, &Issue{Level: "warn", Text: fmt.Sprintf(i18n.T("%s：%s 测速失败", "%s: %s failed the speed test"), r.Name, n.Display), Action: "", Label: ""}
			}
		}
	}
	for _, p := range snap.Providers {
		switch p.Warn {
		case "quota":
			return StateDegraded, &Issue{Level: "warn", Text: fmt.Sprintf(i18n.T("%s 流量快用完了", "%s is almost out of data"), a.providerShown(p.ID)), Action: "providers", Label: i18n.T("查看", "View")}
		case "expire":
			return StateDegraded, &Issue{Level: "warn", Text: fmt.Sprintf(i18n.T("%s 快到期了", "%s expires soon"), a.providerShown(p.ID)), Action: "providers", Label: i18n.T("查看", "View")}
		}
	}
	if is := a.importedIssue(s, snap); is != nil {
		return StateOK, is
	}
	// Nothing is wrong yet, so the light stays green.
	if s.Tun && a.tunStale.Load() {
		return StateOK, &Issue{Level: "warn", Text: i18n.T("TUN 内核需要更新", "The TUN core needs updating"), Detail: i18n.T("bowel 升级后带了新内核，TUN 还在用旧的。授权一次就能换上，网络会中断几秒。", "This version of bowel has a newer core and TUN still runs the old one. One authorization replaces it; the network drops for a few seconds."), Action: "tun-update", Label: i18n.T("更新", "Update")}
	}
	return StateOK, nil
}

func (a *App) providerView(p provider.Provider) ProviderView {
	v := ProviderView{ID: p.ID, Name: p.Name, Kind: string(p.Kind), Source: maskSource(p), Enabled: p.Enabled,
		AutoUpdate: p.AutoUpdate, UpdatedAt: p.UpdatedAt, Error: p.Error}
	v.Routing = a.providerRouting(p)
	if u := p.Usage; u != nil {
		v.Usage = &Usage{Upload: u.Upload, Download: u.Download, Total: u.Total, Expire: u.Expire}
		if u.Total > 0 && float64(u.Upload+u.Download)/float64(u.Total) > .9 {
			v.Warn = "quota"
		} else if u.Expire > 0 && time.Until(time.Unix(u.Expire, 0)) < 7*24*time.Hour {
			v.Warn = "expire"
		}
	}
	return v
}

// maskSource hides a subscription's token: only scheme and host are shown.
func maskSource(p provider.Provider) string {
	switch p.Kind {
	case provider.Subscription:
		if u, err := url.Parse(p.Source); err == nil {
			return u.Scheme + "://" + u.Host + "/…"
		}
	case provider.File:
		return p.Source
	}
	return ""
}

func located(r locate.Result) Located {
	l := Located{Source: r.Source, IP: r.IP}
	if r.Place != nil {
		l.Place = &Place{Country: r.Place.Country, City: geo.Display(r.Place.City), Flag: r.Place.Flag, Lat: r.Place.Lat, Lon: r.Place.Lon}
	}
	return l
}

var _ = geo.Place{} // locate's places are geo.Places

func lanAddr(port int) string {
	addrs, _ := net.InterfaceAddrs()
	for _, ad := range addrs {
		if ip, ok := ad.(*net.IPNet); ok && ip.IP.To4() != nil && !ip.IP.IsLoopback() && ip.IP.IsPrivate() {
			return fmt.Sprintf("%s:%d", ip.IP, port)
		}
	}
	return ""
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

func copyMap[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
