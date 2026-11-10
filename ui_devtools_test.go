package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/devtools"
)

func toolsFixture() []devtools.Tool {
	return []devtools.Tool{
		{ID: devtools.Dockerd, State: devtools.Off, Source: devtools.SourceNone, Guard: devtools.GuardNeed, Restart: true, File: "~/.colima/default/colima.yaml"},
		{ID: devtools.Containers, State: devtools.Stale, Source: devtools.SourceBowel, Applied: devtools.AppliedProxy, Guard: devtools.GuardNeed, Port: 7890, File: "~/.docker/config.json"},
		{ID: devtools.Shell, State: devtools.On, Source: devtools.SourceOwn, Value: "http://127.0.0.1:7899", File: "~/.zshenv"},
		{ID: devtools.Langs, State: devtools.On, Source: devtools.SourceFollow},
		{ID: devtools.GitHTTPS, State: devtools.On, Source: devtools.SourceFollow, File: "~/.gitconfig"},
		{ID: devtools.GitSSH, State: devtools.Off, Source: devtools.SourceNone, Guard: devtools.GuardAuto, File: "~/.ssh/config"},
	}
}

// toolsShot saves the frame when BOWEL_TOOL_SHOTS names a directory.
func toolsShot(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("BOWEL_TOOL_SHOTS")
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

func TestToolsInModeCell(t *testing.T) {
	u := fixtureUI()
	u.snap.Tools = toolsFixture()
	tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(time.Second)
	toolsShot(t, tt, "card")
	if !tt.HasText("3 个开发工具未接入流量 ›") {
		t.Error("the mode cell doesn't say how many are missing")
	}

	u.home.x, u.home.toolsList = "mode", true
	tt.Frame()
	tt.Advance(2 * time.Second)
	toolsShot(t, tt, "list")
	for _, s := range []string{"模式与开发工具", "开发工具", "Docker 拉镜像", "不走", "接入", "更新地址", "跟随终端", "改为直连"} {
		if !tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
	for _, s := range []string{"配置位置", "实测"} {
		if tt.HasText(s) {
			t.Errorf("%q should be gone", s)
		}
	}
	if tt.HasText("编辑规则…") {
		t.Error("rules are still here")
	}
}

func TestQuitDialog(t *testing.T) {
	u := fixtureUI()
	u.snap.Tools = toolsFixture()
	u.snap.Tools[0].State, u.snap.Tools[0].Applied, u.snap.Tools[0].Source = devtools.On, devtools.AppliedProxy, devtools.SourceBowel
	if got := len(toolsAtRisk(u.snap.Tools)); got != 2 {
		t.Fatalf("at risk: %d", got)
	}
	u.quitOpen = true
	tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(time.Second)
	toolsShot(t, tt, "quit")
	for _, s := range []string{"退出后，2 个工具会连不上", "撤销 1 项后退出", "仍然退出"} {
		if !tt.HasText(s) {
			t.Errorf("no %q", s)
		}
	}
}

func TestPanelCopiesTerminalCommand(t *testing.T) {
	u := fixtureUI()
	u.snap.Dev.Exports = "export http_proxy=x"
	tt := ui.NewTesterWith(u.panelView, 320, 620, ui.TesterOptions{})
	tt.Advance(time.Second)
	toolsShot(t, tt, "panel")
	if err := tt.Click("复制终端代理命令"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("已复制") {
		t.Errorf("no feedback in %q", tt.Texts())
	}
	tt.Advance(3 * time.Second)
	tt.Frame()
	if !tt.HasText("复制终端代理命令") {
		t.Errorf("the label didn't come back: %q", tt.Texts())
	}
}

// A button that restarts Colima asks twice; the others act on the first click.
func TestColimaButtonAsksTwice(t *testing.T) {
	u := fixtureUI()
	u.snap.Tools = toolsFixture()
	u.home.x, u.home.toolsList = "mode", true
	tt := ui.NewTesterWith(u.view, 1060, 1400, ui.TesterOptions{})
	tt.Advance(2 * time.Second)
	if err := tt.Click("接入"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if u.home.toolArmed != devtools.Dockerd+devtools.KindApply || !tt.HasText("再点一次，会重启 Colima") {
		t.Errorf("armed %q, texts %q", u.home.toolArmed, tt.Texts())
	}
}

func TestShellRowCopies(t *testing.T) {
	u := fixtureUI()
	u.snap.Tools = toolsFixture()
	u.snap.Dev.Exports = "export a=1"
	u.home.x, u.home.toolsList = "mode", true
	tt := ui.NewTesterWith(u.view, 1060, 1400, ui.TesterOptions{})
	tt.Advance(2 * time.Second)
	if !tt.HasText("复制命令") {
		t.Error("no copy button")
	}
}
