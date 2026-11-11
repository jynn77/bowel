package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Issue 77: with takeover off, an application pointed at the mixed port
// still passes traffic. The traffic views follow what was observed, and
// say "nothing passes" only when nothing was.

// observedCase is one thing bowel saw while not taken over.
type observedCase struct {
	name string
	// seen: traffic was observed, now or a moment ago.
	seen bool
	set  func(u *UI)
}

var observedCases = []observedCase{
	{"active", true, func(u *UI) {
		now := Tick{Down: 48000, Up: 3000, ByRoute: map[string]int64{"default": 48000}, Active: 1, TodayDown: 9 << 20, TodayUp: 1 << 20,
			Apps:   []AppRate{{App: "curl", Host: "example.com", Route: "default", Down: 48000, Conns: 1}},
			Events: []Event{{Time: time.Now(), Text: "curl → example.com", Route: "default", Node: "香港 03 IPLC"}}}
		for i := 0; i < 30; i++ {
			u.home.pushTick(now)
		}
		u.tick = now
		u.act.apply([]Conn{{ID: "m1", App: "curl", Host: "example.com", Port: 443, Network: "tcp", Route: "default", Node: "香港 03 IPLC",
			Down: 1 << 20, DownRate: 48000, Start: time.Now().Add(-20 * time.Second), Status: "active"}}, nil)
	}},
	{"history", true, func(u *UI) {
		past := Tick{Down: 48000, Up: 3000, ByRoute: map[string]int64{"default": 48000}, Active: 1}
		for i := 0; i < 30; i++ {
			u.home.pushTick(past)
		}
		// Now: no connection and no bytes; the event and the closed
		// connection are what is left.
		u.tick = Tick{ByRoute: map[string]int64{}, TodayDown: 9 << 20, TodayUp: 1 << 20,
			Events: []Event{{Time: time.Now().Add(-40 * time.Second), Text: "curl → example.com", Route: "default", Node: "香港 03 IPLC"}}}
		for i := 0; i < 10; i++ {
			u.home.pushTick(u.tick)
		}
		u.act.apply([]Conn{{ID: "m1", App: "curl", Host: "example.com", Port: 443, Network: "tcp", Route: "default", Node: "香港 03 IPLC",
			Down: 1 << 20, Start: time.Now().Add(-60 * time.Second), End: time.Now().Add(-40 * time.Second), Status: "closed"}}, nil)
	}},
	{"nothing", false, func(u *UI) {
		u.tick = Tick{ByRoute: map[string]int64{}}
		for i := 0; i < 30; i++ {
			u.home.pushTick(u.tick)
		}
		u.act.apply(nil, nil)
	}},
}

// observedUI is the fixture not taken over, with nothing seen yet.
func observedUI(c observedCase) *UI {
	u := fixtureUI()
	u.snap.Takeover.On, u.snap.State, u.snap.Issue = false, StateOff, nil
	u.tick, u.home.hist = Tick{}, nil
	c.set(u)
	return u
}

// inked counts the pixels of r that are not the color of its top left
// corner: what is drawn there.
func inked(tt *ui.Tester, r ui.Rect) int {
	img := tt.Image()
	scale := float32(img.Bounds().Dx()) / 1060
	x0, y0, x1, y1 := int(r.X*scale), int(r.Y*scale), int((r.X+r.W)*scale), int((r.Y+r.H)*scale)
	bg := img.RGBAAt(x0, y0)
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if img.RGBAAt(x, y) != bg {
				n++
			}
		}
	}
	return n
}

func TestHomeOffFollowsObserved(t *testing.T) {
	const idle, direct = "没有经过 bowel 的流量", "应用直接连接互联网，除非代理指向 bowel 的端口。"
	for _, c := range observedCases {
		t.Run(c.name, func(t *testing.T) {
			u := observedUI(c)
			tt := ui.NewTester(u.view, 1060, 760)
			tt.Advance(2 * time.Second)
			shot(t, tt, "off-"+c.name)
			// The status and its guidance stay, and say nothing false.
			for _, s := range []string{"未接管", direct} {
				if !hasTextPart(tt, s) {
					t.Errorf("no %q: %q", s, tt.Texts())
				}
			}
			if hasTextPart(tt, "流量不经过 bowel") {
				t.Errorf("the board says no traffic passes: %q", tt.Texts())
			}
			if got := hasTextPart(tt, idle); got == c.seen {
				t.Errorf("idle wording shown = %v with traffic seen = %v: %q", got, c.seen, tt.Texts())
			}
			if got := hasTextPart(tt, "curl → example.com"); got != c.seen {
				t.Errorf("event shown = %v, want %v", got, c.seen)
			}
			if want := map[string]string{"active": "1 个连接", "history": "0 个连接", "nothing": "0 个连接"}[c.name]; !tt.HasText(want) {
				t.Errorf("no %q: %q", want, tt.Texts())
			}
			// The graph: the strip above the speed cell's last line.
			foot, ok := tt.Find("颜色 = 路由")
			head, ok2 := tt.Find("此刻速度")
			if !ok || !ok2 {
				t.Fatalf("no speed cell: %q", tt.Texts())
			}
			bars := ui.Rect{X: head.X, Y: foot.Y - 46, W: foot.X + foot.W - head.X, H: 42}
			n := inked(tt, bars)
			// A flat line is one row of pixels; bars fill far more.
			if line := int(bars.W) * 4; c.seen && n <= line || !c.seen && n > line {
				t.Errorf("graph has %d drawn pixels with traffic seen = %v", n, c.seen)
			}
		})
	}
}

func TestActivityOffFollowsObserved(t *testing.T) {
	const gone, wont = "流量现在不经过 bowel", "新的连接不会经过这里"
	for _, c := range observedCases {
		t.Run(c.name, func(t *testing.T) {
			u := observedUI(c)
			u.page = "activity"
			tt := ui.NewTester(u.view, 1060, 760)
			tt.Advance(time.Second)
			shot(t, tt, "activity-off-"+c.name)
			if hasTextPart(tt, wont) {
				t.Errorf("Log says new connections won't show: %q", tt.Texts())
			}
			if got := hasTextPart(tt, gone); got == c.seen {
				t.Errorf("%q shown = %v with traffic seen = %v: %q", gone, got, c.seen, tt.Texts())
			}
			// Not taken over is still said, with the way to turn it on.
			if !tt.HasText("去代理开启 →") {
				t.Errorf("no way to takeover: %q", tt.Texts())
			}
			// The active one is listed; the closed one is counted under its
			// own filter, the table showing the active ones first.
			if got := hasTextPart(tt, "example.com"); got != (c.name == "active") {
				t.Errorf("connection listed = %v: %q", got, tt.Texts())
			}
			if got := tt.HasText("已关闭 1"); got != (c.name == "history") {
				t.Errorf("closed connection counted = %v: %q", got, tt.Texts())
			}
			if c.seen && !hasTextPart(tt, "bowel 现在没有接管流量，只有代理指向 bowel 端口的应用会出现在这里。") {
				t.Errorf("no note above the connections: %q", tt.Texts())
			}
			if want := map[string]string{"active": "1 个连接", "history": "0 个连接", "nothing": "0 个连接"}[c.name]; !tt.HasText(want) {
				t.Errorf("no %q: %q", want, tt.Texts())
			}
		})
	}
}

// Before the first fetch of connections the head tells the monitor's
// count, taken over or not, and the page does not say nothing passes.
func TestActivityOffBeforeFetch(t *testing.T) {
	u := observedUI(observedCases[0])
	u.page = "activity"
	u.act.apply(nil, nil)
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if !tt.HasText("1 个连接") {
		t.Errorf("head ignores the monitor's count: %q", tt.Texts())
	}
	if hasTextPart(tt, "流量现在不经过 bowel") {
		t.Errorf("Log says no traffic passes with a connection active: %q", tt.Texts())
	}
}

func TestPanelOffFollowsObserved(t *testing.T) {
	const none, some = "流量不经过 bowel", "只有代理指向 bowel 端口的应用经过 bowel"
	for _, c := range observedCases {
		t.Run(c.name, func(t *testing.T) {
			u := observedUI(c)
			tt := panelTester(u, 640, false)
			shot(t, tt, "panel-off-"+c.name)
			wantTexts(t, tt, "未接管", "打开 bowel")
			if got := tt.HasText(none); got == c.seen {
				t.Errorf("%q shown = %v with traffic seen = %v: %q", none, got, c.seen, tt.Texts())
			}
			if got := tt.HasText(some); got != c.seen {
				t.Errorf("%q shown = %v, want %v: %q", some, got, c.seen, tt.Texts())
			}
			// The nodes: traffic through the port leaves through the one chosen.
			for _, s := range []string{"自动", "香港 03 IPLC", "全部节点…"} {
				if got := tt.HasText(s); got != c.seen {
					t.Errorf("%q shown = %v, want %v: %q", s, got, c.seen, tt.Texts())
				}
			}
			// The rates and today's total.
			if got := hasTextPart(tt, "今日"); got != c.seen {
				t.Errorf("speed shown = %v, want %v: %q", got, c.seen, tt.Texts())
			}
		})
	}
}
