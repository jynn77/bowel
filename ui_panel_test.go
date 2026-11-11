package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// panelTester renders the panel alone; a route's list makes it taller than
// 560, which the flyout follows (FitContent).
func panelTester(u *UI, height int, dark bool) *ui.Tester {
	tt := ui.NewTesterWith(u.panelView, 320, height, ui.TesterOptions{Dark: dark})
	tt.Advance(time.Second)
	return tt
}

func wantTexts(t *testing.T, tt *ui.Tester, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if !tt.HasText(s) {
			t.Errorf("panel lacks %q: %q", s, tt.Texts())
		}
	}
}

func TestPanelNormal(t *testing.T) {
	u := fixtureUI()
	tt := panelTester(u, 640, false)
	shot(t, tt, "panel")
	// The nodes, the switch and the mode are all on the first level.
	wantTexts(t, tt, "正常", "接管", "节点", "自动", "最快 + 故障转移", "当前", "香港 03 IPLC", "41 ms", "全部节点…", "8 个",
		"规则", "全局", "直连", "打开 bowel", "⌘O", "退出 bowel", "⌘Q")
	// No routes in the panel.
	for _, s := range []string{"路由", "AI 服务", "流媒体", "开发"} {
		if tt.HasText(s) {
			t.Errorf("panel shows route text %q: %q", s, tt.Texts())
		}
	}
}

// TestPanelStill: nothing in the panel moves, so it asks for no frames
// after the first, nor after a hover.
func TestPanelStill(t *testing.T) {
	u := fixtureUI()
	tt := ui.NewTesterWith(u.panelView, 320, 640, ui.TesterOptions{})
	tt.Frame()
	if tt.FramePending() {
		t.Error("panel animates as it opens")
	}
	r, ok := tt.Find("香港 03 IPLC")
	if !ok {
		t.Fatal("no node row")
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if tt.FramePending() {
		t.Error("panel animates on hover")
	}
}

func TestPanelPending(t *testing.T) {
	u := fixtureUI()
	u.snap.Pending = []string{"node:default", "mode"}
	tt := panelTester(u, 640, false)
	shot(t, tt, "panel-pending")
	wantTexts(t, tt, "切换中…")
	tt.Frame()
	if tt.FramePending() {
		t.Error("pending panel animates")
	}
}

func TestPanelOff(t *testing.T) {
	u := fixtureUI()
	u.snap.Takeover.On = false
	u.snap.State = StateOff
	u.snap.Issue = nil
	u.tick, u.home.hist = Tick{}, nil // nothing observed
	tt := panelTester(u, 560, false)
	shot(t, tt, "panel-off")
	wantTexts(t, tt, "未接管", "流量不经过 bowel", "打开 bowel")
	if tt.HasText("今日") || tt.HasText("全部节点…") {
		t.Errorf("off panel shows the speed or the nodes: %q", tt.Texts())
	}
}

func TestPanelDirect(t *testing.T) {
	u := fixtureUI()
	u.snap.Mode = "direct"
	tt := panelTester(u, 560, false)
	wantTexts(t, tt, "直连模式：不经过任何节点")
	if tt.HasText("全部节点…") {
		t.Errorf("direct panel lists nodes: %q", tt.Texts())
	}
}

func TestPanelDegraded(t *testing.T) {
	u := fixtureUI()
	u.snap.State = StateDegraded
	u.snap.Issue = &Issue{Level: "warn", Text: "系统代理被其他程序改掉了，部分应用的流量没有经过 bowel。", Action: "retake", Label: "重新接管"}
	tt := panelTester(u, 720, false)
	shot(t, tt, "panel-degraded")
	wantTexts(t, tt, "降级", "重新接管")
}

func TestPanelDark(t *testing.T) {
	u := fixtureUI()
	u.snap.Mode = "global"
	tt := panelTester(u, 640, true)
	shot(t, tt, "panel-dark")
	wantTexts(t, tt, "· 全局模式", "自动", "当前")
}
