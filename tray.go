package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"runtime"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/i18n"
)

// The menu bar (L0 of docs/product-ia.md): a template icon, bright when
// Moho has taken over and faint when it has not; nothing beside it, unless
// the user asks for the rates there (Settings.TraySpeed): then they stand
// beside the icon as text, where the platform has text beside an icon
// (macOS, and the Linux panels that show an indicator's label), and in the
// icon's tooltip on Windows, which has none. A click opens the panel
// (ui_panel.go) under the icon; a right click, or a Control-click, the
// menu, which takes over, switches the node and the mode without opening
// the window.

// traySpeedEvery is how often the rates beside the icon are read while
// nothing else shows them.
const traySpeedEvery = 2 * time.Second

func (a *App) setupTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{ID: "moho", Icon: trayIcon(false), IconIsTemplate: true, ToolTip: trayName})
	if err != nil {
		log.Println("tray:", err)
		return
	}
	a.mu.Lock()
	a.tray = tray
	a.mu.Unlock()
	if runtime.GOOS == "linux" {
		// AppIndicator shows menus only and tells of no clicks: the menu
		// stays set, and refreshTray keeps it current.
		tray.SetMenu(a.trayMenu(a.snapshot()))
	} else {
		// No menu set, so that clicks reach OnClick; the menu is set for
		// its popup only.
		tray.OnClick(a.togglePanel)
		tray.OnRightClick(func() {
			if u := a.uiRef(); u != nil {
				u.panelClose()
			}
			tray.SetMenu(a.trayMenu(a.snapshot()))
			defer tray.SetMenu(nil)
			tray.PopUpMenu()
		})
	}
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { tray.Destroy() })
	a.refreshTray(a.snapshot())
}

// togglePanel opens the panel under the icon, or closes it. On the main
// thread, as tray callbacks run.
func (a *App) togglePanel() {
	a.mu.Lock()
	tray := a.tray
	a.mu.Unlock()
	u := a.uiRef()
	if tray == nil || u == nil {
		a.showWindow()
		return
	}
	if fl := u.panel.win.Load(); fl != nil {
		fl.Close()
		return
	}
	fl := mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:         tray,
		Placement:    mygo.PlacementBottom,
		Gap:          4,
		Width:        panelWidth,
		Height:       480,
		FitContent:   true,
		MinWidth:     panelWidth,
		MaxWidth:     panelWidth,
		MaxHeight:    820,
		Focusable:    true, // a click elsewhere closes it
		Shadow:       true,
		CornerRadius: 12,
		Content:      ui.View(u.panelView),
	})
	u.panel.win.Store(fl)
	if u.app != nil {
		u.app.mon.Wake() // the panel shows rates: at once, and every second
	}
	fl.OnClosed(func() { u.panel.win.CompareAndSwap(fl, nil) })
}

// trayOn is the icon last drawn: bright (taken over) or faint; and the
// text last set beside it and in its tooltip.
var trayOn = struct {
	sync.Mutex
	set, on    bool
	title, tip string
	// nodes: the Linux menu last set offers the nodes.
	nodes bool
}{tip: trayName}

// trayName is the tooltip when it tells no rates.
const trayName = "bowel"

func (a *App) refreshTray(s Snapshot) {
	a.mu.Lock()
	tray := a.tray
	a.mu.Unlock()
	if tray == nil {
		return
	}
	on := s.Takeover.On
	trayOn.Lock()
	changed := !trayOn.set || trayOn.on != on
	trayOn.set, trayOn.on = true, on
	trayOn.Unlock()
	if changed {
		_ = tray.SetIcon(trayIcon(on), true)
	}
	if runtime.GOOS == "linux" {
		a.setTrayMenu(tray, s)
	}
	a.traySpeed(tray)
	// The window's Update set the snapshot; the redraw queues after it.
	if u := a.uiRef(); u != nil {
		u.panelRedraw()
	}
}

// trayTick redraws the open panel's speeds, and those beside the icon if
// the user has them there.
func (a *App) trayTick() {
	a.mu.Lock()
	tray := a.tray
	a.mu.Unlock()
	if tray != nil {
		a.traySpeed(tray)
		// The Linux menu is built from the snapshot, which traffic does not
		// change: rebuild it when traffic through the port comes or goes
		// while not taken over, as that is what offers the nodes.
		if runtime.GOOS == "linux" {
			s := a.snapshot()
			trayOn.Lock()
			stale := trayOn.nodes != trayNodes(s.Takeover.On, a.tick())
			trayOn.Unlock()
			if stale {
				a.setTrayMenu(tray, s)
			}
		}
	}
	if u := a.uiRef(); u != nil {
		u.panelRedraw()
	}
}

// setTrayMenu sets the Linux menu for s, and notes whether it offers the
// nodes.
func (a *App) setTrayMenu(tray *mygo.Tray, s Snapshot) {
	trayOn.Lock()
	trayOn.nodes = trayNodes(s.Takeover.On, a.tick())
	trayOn.Unlock()
	tray.SetMenu(a.trayMenu(s))
}

// traySpeed sets the rates beside the icon and in its tooltip, or takes
// them away; the tray hears of it only when the text changes.
func (a *App) traySpeed(tray *mygo.Tray) {
	st := a.settings.Get()
	t := a.tick()
	title, tip := trayText(runtime.GOOS, st.TraySpeed && (a.takeoverOn(st) || trafficNow(t)), t)
	trayOn.Lock()
	newTitle, newTip := title != trayOn.title, tip != trayOn.tip
	trayOn.title, trayOn.tip = title, tip
	trayOn.Unlock()
	if newTitle {
		tray.SetTitle(title)
	}
	if newTip {
		tray.SetToolTip(tip)
	}
}

// trafficNow says whether t has traffic in it: a connection, or bytes
// moving. Not taken over, the tray tells its rates only then.
func trafficNow(t Tick) bool { return t.Active > 0 || t.Down > 0 || t.Up > 0 }

// trayNodes says whether the tray offers the nodes: taken over, or with
// traffic through the port, which leaves through the node chosen.
func trayNodes(on bool, t Tick) bool { return on || trafficNow(t) }

// trayText is what stands beside the icon and what its tooltip says, on
// the system goos, with the rates shown or not. Windows has no text beside
// a tray icon, so the tooltip tells the rates there. On Linux the tooltip
// is also the indicator's name, which stays.
func trayText(goos string, show bool, t Tick) (title, tip string) {
	tip = trayName
	if !show {
		return "", tip
	}
	if goos == "windows" {
		return "", fmt.Sprintf("%s · ↓ %s  ↑ %s", trayName, fmtRate(t.Down), fmtRate(t.Up))
	}
	return "↓" + padRate(t.Down) + " ↑" + padRate(t.Up), tip
}

// padRate is fmtRateShort at a fixed width, filled on the left with figure
// spaces, which are as wide as a digit in the menu bar: the item keeps its
// width as the numbers change, and its neighbors stay where they are.
func padRate(b int64) string {
	s := fmtRateShort(b)
	for n := utf8.RuneCountInString(s); n < 4; n++ {
		s = "\u2007" + s
	}
	return s
}

// fmtRateShort is a rate in at most four characters, for the menu bar:
// "0B", "512B", "60K", "1.3M", "999M", "2.1G".
func fmtRateShort(b int64) string {
	if b < 1000 {
		return fmt.Sprintf("%dB", max(b, 0))
	}
	v, i := float64(b), -1
	for v >= 1000 && i < 3 {
		v /= 1024
		i++
	}
	if v < 9.95 {
		return fmt.Sprintf("%.1f%c", v, "KMGT"[i])
	}
	return fmt.Sprintf("%.0f%c", v, "KMGT"[i])
}

func (a *App) trayMenu(s Snapshot) *mygo.Menu {
	head := stateName(s.State)
	if s.Exit != nil && s.Mode != "direct" && s.State != StateOff {
		where := ""
		if p := s.Exit.Where.Place; p != nil {
			where = p.Flag + " "
		}
		head += " · " + where + a.display(s.Exit.Node)
	}
	items := []*mygo.MenuItem{{Label: head, Disabled: true}}
	if s.Issue != nil {
		items = append(items, &mygo.MenuItem{Label: s.Issue.Text, Disabled: true})
	}
	items = append(items,
		&mygo.MenuItem{Label: i18n.T("接管", "Takeover"), Type: mygo.MenuItemCheckbox, Checked: s.Takeover.On, Accelerator: "CmdOrCtrl+T",
			Click: func(it *mygo.MenuItem, _ *mygo.Window) { on := it.Checked; go func() { a.logErr(a.setTakeover(on)) }() }},
		mygo.Separator(),
	)

	// One node list, the default exit's, as the panel has; the window has the routes.
	for _, r := range s.Routes {
		if r.ID != "default" || r.Direct {
			continue
		}
		label := i18n.T("节点", "Node")
		for _, n := range s.Nodes {
			if n.Name == r.Node {
				label = fmt.Sprintf(i18n.T("节点", "Node")+"\t%s%s", flagOf(n), n.Display)
			}
		}
		sub := a.routeMenu(r, s.Nodes)
		if r.Group != "" {
			sub = groupMenu(a, r, s.Imported)
		}
		items = append(items, &mygo.MenuItem{Label: label, Submenu: sub,
			Disabled: len(s.Nodes) == 0 || !trayNodes(s.Takeover.On, a.tick()) || s.Mode == "direct"})
	}
	items = append(items, mygo.Separator())
	for _, m := range [][2]string{{"rule", i18n.T("规则模式", "Rule mode")}, {"global", i18n.T("全局模式", "Global mode")}, {"direct", i18n.T("直连模式", "Direct mode")}} {
		id := m[0]
		items = append(items, &mygo.MenuItem{Label: m[1], Type: mygo.MenuItemRadio, Checked: s.Mode == id,
			Click: func(*mygo.MenuItem, *mygo.Window) { go func() { a.logErr(a.setMode(id)) }() }})
	}
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: i18n.T("打开 bowel", "Open bowel"), Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow() }},
		&mygo.MenuItem{Label: i18n.T("设置…", "Settings…"), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) {
			if u := a.uiRef(); u != nil {
				u.openSettings()
			}
		}},
		&mygo.MenuItem{Role: mygo.RoleQuit, Label: i18n.T("退出 bowel", "Quit bowel")},
	)
	return mygo.NewMenu(items)
}

// routeMenu lists automatic and the fastest nodes; the window has the rest.
func (a *App) routeMenu(r RouteView, all []NodeView) []*mygo.MenuItem {
	rid := r.ID
	items := []*mygo.MenuItem{{Label: i18n.T("自动（最快 + 故障转移）", "Auto (fastest + failover)"), Type: mygo.MenuItemRadio, Checked: r.Auto,
		Click: func(*mygo.MenuItem, *mygo.Window) { go func() { a.logErr(a.selectNode(rid, "")) }() }}, mygo.Separator()}
	nodes := make([]NodeView, len(all))
	for i, n := range all {
		nodes[i] = n.On(rid)
	}
	sort.SliceStable(nodes, func(i, j int) bool { return rank(nodes[i]) < rank(nodes[j]) })
	if len(nodes) > 15 {
		nodes = nodes[:15]
	}
	for _, n := range nodes {
		name := n.Name
		d := "—"
		if n.Tested {
			d = i18n.T("超时", "Timeout")
			if n.Delay > 0 {
				d = fmt.Sprintf("%d ms", n.Delay)
			}
		}
		items = append(items, &mygo.MenuItem{Label: flagOf(n) + n.Display + "\t" + d, Type: mygo.MenuItemRadio, Checked: !r.Auto && r.Node == name,
			Click: func(*mygo.MenuItem, *mygo.Window) { go func() { a.logErr(a.selectNode(rid, name)) }() }})
	}
	if len(all) > 15 {
		items = append(items, mygo.Separator(), &mygo.MenuItem{Label: fmt.Sprintf(i18n.T("全部 %d 个节点…", "All %d nodes…"), len(all)), Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow() }})
	}
	return items
}

// groupMenu is the members of the imported group a route is. Only the kind
// the user chooses in can be changed from here.
func groupMenu(a *App, r RouteView, im *ImportedView) []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for _, g := range im.Groups {
		if g.Name != r.Group {
			continue
		}
		if !g.Selectable {
			items = append(items, &mygo.MenuItem{Label: groupKindWord(g.Kind) + i18n.T(" · 由内核自己选择", " · the core chooses by itself"), Disabled: true})
		}
		for _, m := range g.Members {
			group, member := g.Name, m.Name
			item := &mygo.MenuItem{Label: display(member), Type: mygo.MenuItemRadio, Checked: g.Now == member, Disabled: !g.Selectable}
			item.Click = func(*mygo.MenuItem, *mygo.Window) { go func() { a.logErr(a.selectMember(group, member)) }() }
			items = append(items, item)
		}
	}
	return items
}

func rank(n NodeView) int {
	if !n.Tested {
		return 1 << 20
	}
	if n.Delay == 0 {
		return 1 << 21
	}
	return n.Delay
}

func flagOf(n NodeView) string {
	if n.Where.Place != nil && n.Where.Place.Flag != "" && flagDrawn(n.Where.Place.Flag) {
		return n.Where.Place.Flag + " "
	}
	return ""
}

func (a *App) logErr(err error) {
	if err != nil {
		log.Println(err)
		a.refreshTray(a.snapshot()) // undo a checkbox the click toggled
	}
}

// trayIcon draws the template icon, 36×36 px: the logo's squiggle in one
// ink, full when on and faded when off. That is all it tells.
func trayIcon(on bool) []byte {
	const n = 36
	cov := make([]float64, n*n)
	// The squiggle and its stroke span 194 of the logo's 200 units; fill 34 px.
	const scale = 34.0 / (170 + logoWidth)
	const o = 18 - 100*scale
	strokeLogo(logoPoints, cov, n, n, scale, o, o, logoWidth)
	ink := 1.0
	if !on {
		ink = .4
	}
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if c := cov[y*n+x] * ink; c > 0 {
				img.SetNRGBA(x, y, color.NRGBA{A: uint8(math.Min(1, c) * 255)})
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
