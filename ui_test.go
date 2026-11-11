package main

import (
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// fixture is a realistic state for rendering the interface without a core.
func fixture() Snapshot {
	place := func(cc, city, flag string, lat, lon float64) *Place {
		return &Place{Country: cc, City: city, Flag: flag, Lat: lat, Lon: lon}
	}
	hk := place("HK", "香港", "🇭🇰", 22.3, 114.2)
	jp := place("JP", "东京", "🇯🇵", 35.7, 139.7)
	sg := place("SG", "新加坡", "🇸🇬", 1.3, 103.8)
	us := place("US", "洛杉矶", "🇺🇸", 34.05, -118.24)
	node := func(name, prov, typ string, delay int, pl *Place, src string) NodeView {
		h := []int{delay, delay + 8, delay - 5, delay + 3, delay, delay + 12, delay - 2, delay, delay + 4, delay - 6}
		if delay == 0 {
			h = []int{0, 0, 120, 0, 0}
		}
		return NodeView{Name: name + "​‹" + prov, Display: name, Provider: prov, Type: typ, UDP: true, Delay: delay, Tested: true, History: h,
			Where: Located{Place: pl, Source: src}}
	}
	nodes := []NodeView{
		node("香港 03 IPLC", "a", "Shadowsocks", 41, hk, "probe"),
		node("香港 02", "a", "Trojan", 64, hk, "probe"),
		node("香港 01", "b", "Trojan", 72, hk, "name"),
		node("东京 01", "a", "VMess", 118, jp, "probe"),
		node("东京 02", "a", "VMess", 0, jp, "name"),
		node("新加坡 01", "a", "Shadowsocks", 143, sg, "probe"),
		node("洛杉矶 VPS", "c", "Hysteria2", 168, us, "probe"),
		node("Relay-7 专线", "d", "VLESS", 212, nil, "unknown"),
	}
	return Snapshot{
		State: StateOK, Core: Core{State: "running", Version: "v1.19.33"}, Mode: "rule", RuleCount: 79,
		Takeover: Takeover{On: true, Port: 7899, System: Entry{Wanted: true, Status: "on"}, Tun: Entry{Status: "install"},
			Bypass: []string{"localhost", "127.0.0.1", "*.local", "192.168.0.0/16"}},
		Routes: []RouteView{
			{ID: "default", Name: "其他流量", Summary: "没命中任何规则的流量", Rules: 1, Auto: true, Node: nodes[0].Name, Share: .31},
			{ID: "ai", Name: "AI 服务", Summary: "OpenAI · Claude · Gemini · Perplexity 等", Rules: 26, Node: nodes[6].Name, Share: .08},
			{ID: "media", Name: "流媒体", Summary: "YouTube · Netflix · Spotify · Disney+ 等", Rules: 10, Node: nodes[5].Name, Share: .52},
			{ID: "dev", Name: "开发", Summary: "GitHub · npm · Docker · PyPI · Go 等", Rules: 24, Auto: true, Node: nodes[3].Name, Share: .04},
			{ID: "direct", Name: "直连", Summary: "国内网站 · 局域网", Rules: 4, Direct: true, Share: .05},
			{ID: "reject", Name: "拦截", Summary: "命中规则的连接会被拒绝", Direct: true, Reject: true},
		},
		Nodes: nodes,
		Exit:  &Exit{Node: nodes[0].Name, Where: nodes[0].Where},
		You:   &Located{Place: place("CN", "上海", "🇨🇳", 31.2, 121.5), Source: "probe"},
		Providers: []ProviderView{
			{ID: "a", Name: "机场 A", Kind: "subscription", Source: "https://sub.example-a.com/…", Enabled: true, AutoUpdate: 12, UpdatedAt: time.Now().Add(-10 * time.Minute), Usage: &Usage{Download: 31 << 30, Total: 100 << 30}, Nodes: 42},
			{ID: "b", Name: "机场 B", Kind: "subscription", Source: "https://b.example.net/…", Enabled: true, UpdatedAt: time.Now().Add(-72 * time.Hour), Usage: &Usage{Download: 188 << 30, Total: 200 << 30}, Nodes: 18, Warn: "quota"},
			{ID: "c", Name: "自建 · 洛杉矶 VPS", Kind: "link", Enabled: true, Nodes: 1},
			{ID: "d", Name: "朋友分享的专线", Kind: "link", Enabled: true, Nodes: 1},
		},
	}
}

func fixtureTick(i int) Tick {
	w := func(base float64, p float64) int64 { return int64(base * (1 + .5*sinf(float64(i)/p))) }
	t := Tick{ByRoute: map[string]int64{"default": w(4e5, 9), "ai": w(1e5, 4), "media": w(7.2e5, 13), "dev": w(6e4, 3), "direct": w(7e4, 6)}, Up: w(6e4, 5),
		Active: 58, TodayDown: 2310 << 20, TodayUp: 186 << 20, MonthDown: 48 << 30,
		Apps: []AppRate{{App: "Arc", Host: "youtube.com", Route: "media", Down: 1210000, Conns: 18}, {App: "Docker", Host: "docker.io", Route: "dev", Down: 210000, Conns: 6},
			{App: "Claude", Host: "api.anthropic.com", Route: "ai", Down: 84000, Conns: 4}, {App: "Slack", Host: "slack.com", Route: "default", Down: 12000, Conns: 3}},
		Events: []Event{{Time: time.Now(), Text: "Arc → googlevideo.com", Route: "media", Node: "新加坡 01"}, {Time: time.Now(), Text: "Claude → api.anthropic.com", Route: "ai", Node: "洛杉矶 VPS"},
			{Time: time.Now(), Text: "git → github.com:22", Route: "dev", Node: "东京 01"}, {Time: time.Now(), Text: "Arc → x.com", Route: "default", Failed: true}}}
	for _, v := range t.ByRoute {
		t.Down += v
	}
	return t
}

var fontsOnce sync.Once

func sinf(x float64) float64 { return float64(float32(x) - float32(int(x/6.283))*6.283) } // cheap wobble

// fixtureUI is a UI over the fixture, with no app behind it, in the
// app's own fonts.
func fixtureUI() *UI {
	fontsOnce.Do(registerFonts)
	u := newUI(nil)
	u.snap = fixture()
	for i := 0; i < 90; i++ {
		t := fixtureTick(i)
		u.tick = t
		u.home.pushTick(t)
	}
	return u
}

// shot saves the tester's frame when MOHO_SHOTS names a directory.
func shot(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("MOHO_SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestHomeBoard(t *testing.T) {
	for _, dark := range []bool{false, true} {
		u := fixtureUI()
		tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{Dark: dark})
		suffix := map[bool]string{false: "", true: "-dark"}[dark]
		tt.Advance(time.Second)
		shot(t, tt, "home"+suffix)
		for _, id := range []string{"hero", "map", "mode", "takeover", "routes", "speed", "apps", "events"} {
			u.home.x, u.home.sub = id, ""
			tt.Frame()
			tt.Advance(2 * time.Second)
			if dark && id != "takeover" {
				continue
			}
			shot(t, tt, "x-"+id+suffix)
		}
		u.home.x, u.home.sub = "routes", "ai"
		tt.Frame()
		tt.Advance(2 * time.Second)
		shot(t, tt, "x-routes-ai"+suffix)
		u.home.x = ""
		u.snap.Takeover.On = false
		u.snap.State = StateOff
		tt.Frame()
		tt.Advance(2 * time.Second)
		shot(t, tt, "off"+suffix)
		if !tt.HasText("未接管") {
			t.Errorf("off board lacks its headline: %q", tt.Texts())
		}
	}
}

// Not taken over is the same board: nothing moves or goes away when the
// last way in is turned off, and the cells still open.
func TestBoardStaysWhenOff(t *testing.T) {
	u := fixtureUI()
	u.snap.Takeover.On, u.snap.State = false, StateOff
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(2 * time.Second)
	for _, s := range []string{"未接管", "模式", "接管", "系统代理", "TUN"} {
		if !tt.HasText(s) {
			t.Errorf("no %q on the board: %q", s, tt.Texts())
		}
	}
	if err := tt.Click("模式"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(2 * time.Second)
	if u.home.x != "mode" {
		t.Errorf("the mode cell did not open: %q", u.home.x)
	}
}

// Only the switch changes a way of taking over: a click on its name opens
// the cell, and a click on the switch stays on the board.
func TestEntrySwitches(t *testing.T) {
	u := fixtureUI()
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(2 * time.Second)
	for _, label := range []string{"使用系统代理", "使用 TUN"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Advance(time.Second)
		if u.home.x != "" {
			t.Fatalf("%s opened cell %q", label, u.home.x)
		}
	}
	if err := tt.Click("系统代理"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(2 * time.Second)
	if u.home.x != "takeover" {
		t.Errorf("the name opened %q", u.home.x)
	}
}

func TestHomeMidMorph(t *testing.T) {
	u := fixtureUI()
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	u.home.x = "takeover"
	tt.Frame()
	tt.Advance(180 * time.Millisecond)
	shot(t, tt, "morph-180ms")
	tt.Advance(160 * time.Millisecond)
	shot(t, tt, "morph-340ms")
}

func TestPasteAnywhere(t *testing.T) {
	u := fixtureUI()
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	tt.SetClipboard("hello")
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if u.page != "home" {
		t.Fatalf("plain text switched the page to %q", u.page)
	}
	tt.SetClipboard(" trojan://pw@example.com:443#东京 \n")
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if u.page != "providers" || u.prov.input != "trojan://pw@example.com:443#东京" {
		t.Fatalf("page %q, input %q", u.page, u.prov.input)
	}
}

func TestHeader(t *testing.T) {
	u := fixtureUI()
	u.snap.State = StateDegraded
	u.snap.Issue = &Issue{Level: "warn", Text: "还没有节点，流量正在直连", Action: "providers", Label: "添加提供商"}
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	shot(t, tt, "header")
	tabs := []string{"代理", "日志", "提供商"}
	var prev ui.Rect
	for i, s := range tabs {
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("no tab %q: %q", s, tt.Texts())
		}
		if r.Y > 50 || (i > 0 && r.X <= prev.X) {
			t.Errorf("tab %q at %+v", s, r)
		}
		prev = r
	}
	if prev.X > 400 {
		t.Errorf("tabs are not on the left: 提供商 at %+v", prev)
	}
	n, ok := tt.Find("还没有节点，流量正在直连")
	if !ok || n.Y > 50 || n.X <= prev.X {
		t.Errorf("the notice is not in the title bar after the tabs: %+v %v", n, ok)
	}
	// A tab switches the page in the same frame: no fade.
	if err := tt.Click("日志"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if u.page != "activity" || !tt.HasText("内核日志") || tt.HasText("此刻速度") {
		t.Errorf("page %q after one frame: %q", u.page, tt.Texts())
	}
	if err := tt.Click("添加提供商"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if u.page != "providers" {
		t.Errorf("the notice's action went to %q", u.page)
	}
	shot(t, tt, "header-providers")
}

// A notice is news, not a state: when the issue behind it is gone, so is it.
func TestNoticeGoesAway(t *testing.T) {
	u := fixtureUI()
	text := "默认：[机场A]香港01不可用，已经切换到[机场B]东京02"
	u.snap.State = StateDegraded
	u.snap.Issue = &Issue{Level: "warn", Text: text, Action: "activity", Label: "查看"}
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if !tt.HasText("[机场A]香港01") {
		t.Fatalf("the notice is not shown: %q", tt.Texts())
	}
	u.snap.State = StateOK
	u.snap.Issue = nil
	tt.Frame()
	tt.Advance(time.Second) // past the exit motion
	if tt.HasText("[机场A]香港01") {
		t.Error("the notice is still there after the issue is gone")
	}
}

// TestSharedPiecesKeepUp: the pieces shared between a cell's views, as
// the exit's name, go with the cell as it moves and get to their places
// as its box does, on the same curve, rather than chasing them.
func TestSharedPiecesKeepUp(t *testing.T) {
	for _, move := range []string{">routes", "routes>", "routes>apps", "hero>speed", ">apps"} {
		from, to, _ := strings.Cut(move, ">")
		u := fixtureUI()
		tt := ui.NewTester(u.view, 1060, 760)
		u.home.x = from
		tt.Frame()
		tt.Advance(2 * time.Second)

		type sample struct {
			at         time.Duration
			copy, here ui.Rect
		}
		var now time.Duration
		seen := map[string][]sample{}
		shareTrace = func(k string, cp, at ui.Rect) { seen[k] = append(seen[k], sample{now, cp, at}) }
		u.home.x = to
		tt.Frame()
		for now = 0; now < 600*time.Millisecond; now += 16 * time.Millisecond {
			tt.Advance(16 * time.Millisecond)
		}
		shareTrace = nil

		gap := func(s sample) float32 { return abs32(s.copy.X-s.here.X) + abs32(s.copy.Y-s.here.Y) }
		moved := 0
		for k, ss := range seen {
			var start float32
			for _, s := range ss {
				if s.copy.W > 0 && s.here.W > 0 {
					start = max(start, gap(s))
				}
			}
			if start < 8 {
				continue
			}
			moved++
			for _, s := range ss {
				// the cell's own box has 13% of its way left at 192 ms
				if s.at == 192*time.Millisecond && gap(s) > .16*start {
					t.Errorf("%s: %s has %.0f of %.0f px left at 192 ms", move, k, gap(s), start)
				}
				if s.at >= 512*time.Millisecond && gap(s) > 1 {
					t.Errorf("%s: %s is %.1f px off at %v", move, k, gap(s), s.at)
				}
			}
		}
		if moved == 0 {
			t.Errorf("%s: no shared piece moved", move)
		}
	}
}

// The expanded map pans as it is dragged, zooms as it is pinched or the
// wheel turns, keeps the place under the pointer where it is, and folds
// up on a click that doesn't drag.
func TestMapPanZoom(t *testing.T) {
	u := fixtureUI()
	tt := ui.NewTester(u.view, 1060, 760)
	u.home.x = "map"
	tt.Frame()
	tt.Advance(2 * time.Second)
	v := &u.home.mapView

	tt.Press(300, 300)
	for i := 1; i <= 10; i++ {
		tt.Move(300+float32(i)*12, 300+float32(i)*6)
	}
	tt.Release(420, 360)
	tt.Advance(100 * time.Millisecond)
	if u.home.x != "map" {
		t.Fatal("a drag folded the map up")
	}
	if v.dlon >= 0 || v.dlat <= 0 {
		t.Errorf("dragging right and down should show what is west and north: dlon %.1f dlat %.1f", v.dlon, v.dlat)
	}

	before := *v
	tt.Pinch(400, 300, 2)
	tt.Advance(100 * time.Millisecond)
	if v.zoom < 1.9 || v.zoom > 2.1 {
		t.Errorf("pinching by 2 zoomed to %.2f", v.zoom)
	}
	tt.Wheel(400, 300, 0, 200)
	tt.Advance(100 * time.Millisecond)
	if v.zoom >= 2 {
		t.Errorf("the wheel turned toward the user should zoom out: %.2f", v.zoom)
	}
	if *v == before {
		t.Error("nothing changed")
	}
	shot(t, tt, "x-map-zoomed")

	tt.ClickAt(400, 300)
	tt.Advance(time.Second)
	if u.home.x != "" {
		t.Error("a click on the map should fold it up")
	}
	if *v != (mapView{}) {
		t.Errorf("the view should reset once folded: %+v", *v)
	}
}

// As the map zooms, you go with it smoothly: one of the land's dots stays
// under you rather than sliding past (they were laid out from 0°, 0°,
// half a world away, so each step shifted them up to a pitch around
// you), and where you are not known you stay put in the view rather than
// ease after it.
func TestMapZoomKeepsYou(t *testing.T) {
	for _, known := range []bool{true, false} {
		u := fixtureUI()
		if !known {
			u.snap.You = nil
		}
		tt := ui.NewTester(u.view, 1060, 760)
		u.home.x = "map"
		tt.Frame()
		tt.Advance(2 * time.Second)
		var x, y, lx, ly float32
		youTrace = func(a, b, c, d float32) { x, y, lx, ly = a, b, c, d }
		tt.Frame()
		x0, y0 := x, y
		for i, s := range []float32{1.2, 1.2, 1.2, 1 / 1.2, 1 / 1.2} {
			tt.Pinch(300, 250, s)
			tt.Advance(16 * time.Millisecond)
			if known && (x != lx || y != ly) {
				t.Errorf("step %d: you at %.1f,%.1f but the land's dots go from %.1f,%.1f", i, x, y, lx, ly)
			}
			if !known && (abs32(x-x0) > .01 || abs32(y-y0) > .01) {
				t.Errorf("step %d: you, not known, moved from %.1f,%.1f to %.1f,%.1f", i, x0, y0, x, y)
			}
		}
		youTrace = nil
	}
}

// Zooming keeps the place under the pointer where it is.
func TestMapZoomAt(t *testing.T) {
	fit := projection{lon0: 90, lon1: 150, lat0: -10, lat1: 50, span: .7}
	var v mapView
	v.pan(fit, 700, 500, 40, -30)
	where := func() (float64, float64) {
		pr := fit.view(v)
		fx, fy := 300/(700*.7), 200/500.0
		return pr.lon0 + fx*(pr.lon1-pr.lon0), pr.lat1 - fy*(pr.lat1-pr.lat0)
	}
	lon, lat := where()
	for _, s := range []float64{2, 1.5, .3, 4} {
		v.zoomAt(fit, 700, 500, 300, 200, s)
		if l2, a2 := where(); math.Abs(l2-lon) > 1e-6 || math.Abs(a2-lat) > 1e-6 {
			t.Fatalf("zooming by %v moved %.3f,%.3f to %.3f,%.3f", s, lon, lat, l2, a2)
		}
	}
}

// TestRegionChips draws the node picker's region chips, 🇹🇼 (which the emoji
// font may not draw) among them.
func TestRegionChips(t *testing.T) {
	for _, dark := range []bool{false, true} {
		u := fixtureUI()
		tw := &Place{Country: "TW", City: "台北", Flag: "🇹🇼", Lat: 25.03, Lon: 121.57}
		u.snap.Nodes = append(u.snap.Nodes, NodeView{Name: "台北 01​‹b", Display: "台北 01", Provider: "b", Type: "Trojan", Delay: 96, Tested: true, Where: Located{Place: tw, Source: "name"}})
		tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{Dark: dark})
		u.home.x, u.home.sub = "routes", "ai"
		tt.Frame()
		tt.Advance(2 * time.Second)
		for _, s := range []string{"TW", "Unknown", "未知"} {
			if tt.HasText(s) {
				goto found
			}
		}
		t.Errorf("chips lack TW / unknown: %q", tt.Texts())
	found:
		shot(t, tt, "region-chips"+map[bool]string{false: "", true: "-dark"}[dark])
	}
}

// TestRouteBackTarget keeps the route detail's ‹ a real target, not just
// the glyph's few pixels, and Escape stepping back one level at a time
// even after the click took the focused ‹ away.
func TestRouteBackTarget(t *testing.T) {
	u := fixtureUI()
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	u.home.x, u.home.sub = "routes", "ai"
	tt.Frame()
	tt.Advance(2 * time.Second)
	r, ok := tt.Find("返回路由")
	if !ok {
		t.Fatalf("no back button: %q", tt.Texts())
	}
	if r.W < 30 || r.H < 30 {
		t.Fatalf("back button is %.0f×%.0f, want at least 30×30", r.W, r.H)
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	shot(t, tt, "x-routes-ai-back-hover")
	// A corner of the target, well off the glyph, still goes back.
	tt.ClickAt(r.X+3, r.Y+r.H-3)
	tt.Frame()
	if u.home.x != "routes" || u.home.sub != "" {
		t.Fatalf("after ‹: x %q sub %q, want the route list", u.home.x, u.home.sub)
	}

	// Off every control: a tooltip under the pointer takes the first Escape.
	tt.Move(1040, 740)
	tt.Advance(time.Second)
	u.home.sub = "ai"
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if u.home.x != "routes" || u.home.sub != "" {
		t.Fatalf("Escape in a route: x %q sub %q, want the route list", u.home.x, u.home.sub)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if u.home.x != "" {
		t.Fatalf("Escape in the route list left x %q", u.home.x)
	}
}

// TestInactiveWindowFades: the main window's chosen tab is the accent
// while the window has the user's input, a gray with ink on it while
// another window has it, and the accent again when it comes back.
func TestInactiveWindowFades(t *testing.T) {
	for _, dark := range []bool{false, true} {
		u := fixtureUI()
		tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{Dark: dark})
		suffix := map[bool]string{false: "", true: "-dark"}[dark]
		tt.Advance(time.Second)
		r, ok := tt.Find("代理")
		if !ok {
			t.Fatalf("no 代理 tab among %q", tt.Texts())
		}
		tab := func() ui.Color {
			img := tt.Image()
			s := float32(img.Bounds().Dx()) / 1060
			c := img.RGBAAt(int((r.X-8)*s), int((r.Y+r.H/2)*s))
			return ui.RGBA(c.R, c.G, c.B, 1)
		}
		p := &lightPal
		if dark {
			p = &darkPal
		}
		near := func(a, b ui.Color) bool {
			d := func(x, y uint8) int { return max(int(x)-int(y), int(y)-int(x)) }
			return d(a.R, b.R)+d(a.G, b.G)+d(a.B, b.B) < 12
		}
		if c := tab(); !near(c, p.accent) {
			t.Errorf("dark %v: active tab %v, not the accent %v", dark, c, p.accent)
		}
		tt.SetWindowActive(false)
		gray := p.paper.Mix(p.ink, inactiveMix(p))
		if c := tab(); !near(c, gray) {
			t.Errorf("dark %v: inactive tab %v, not the gray %v", dark, c, gray)
		}
		shot(t, tt, "home-inactive"+suffix)
		tt.SetWindowActive(true)
		if c := tab(); !near(c, p.accent) {
			t.Errorf("dark %v: tab %v after coming back, not the accent %v", dark, c, p.accent)
		}
	}
}

// TestHomeBackInPlace: coming back to 代理 from another tab, in a window
// that isn't the size the window opens at, the board is in place on its
// first frame. It used to be laid out for 1060×686 on that frame and then
// moved to the window's size: a blank strip at the bottom and a bounce
// (issue 68).
func TestHomeBackInPlace(t *testing.T) {
	u := fixtureUI()
	tt := ui.NewTester(u.view, 1061, 800)
	labels := []string{"模式", "接管", "此刻速度", "谁在用", "刚刚"}
	where := func() (out []ui.Rect) {
		t.Helper()
		for _, s := range labels {
			r, ok := tt.Find(s)
			if !ok {
				t.Fatalf("no %q: %q", s, tt.Texts())
			}
			out = append(out, r)
		}
		return out
	}
	tt.Frame()
	opened := where()
	tt.Advance(2 * time.Second)
	settled := where()
	if !slices.Equal(opened, settled) {
		t.Errorf("the board moved after the window opened:\n%v\n%v", opened, settled)
	}
	if low := settled[2]; low.Y < 800-736+500 {
		t.Fatalf("the board doesn't use the window's height: 此刻速度 at %v", low)
	}
	u.page = "routes"
	tt.Frame()
	tt.Advance(time.Second)
	u.page = "home"
	tt.Frame()
	shot(t, tt, "home-back-first-frame")
	if back := where(); !slices.Equal(back, settled) {
		t.Errorf("back on 代理 the board is out of place on its first frame:\n%v\n%v", back, settled)
	}
}
