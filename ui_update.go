package main

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/i18n"
)

// ---- What the update says ----

func updateRestartHint() string {
	return i18n.T("下载完成后 bowel 会自动重启，网络会中断几秒", "bowel restarts once it is downloaded, and the network drops for a few seconds")
}

// updateAction is a link beside the update's words.
type updateAction struct {
	key, label, tip string
	click           func()
}

// updateLine is the update as one line, for the title bar and the panel.
type updateLine struct {
	dot     string // "accent", "signal" or ""
	text    string
	tip     string
	percent int // of the bar, -1 for none
	busy    bool
	actions []updateAction
}

// updateLine is what there is to say about updating, nil for nothing.
func (u *UI) updateLine() *updateLine {
	if up := u.snap.Update; up != nil {
		switch up.Status {
		case updateAvailable:
			return &updateLine{dot: "accent", percent: -1, text: fmt.Sprintf(i18n.T("新版本 %s", "Version %s"), up.Version),
				actions: []updateAction{{"update-start", i18n.T("升级", "Update"), updateRestartHint(), func() { go u.app.startUpdate() }}}}
		case updateDownloading:
			if up.Percent >= 100 {
				return &updateLine{percent: -1, busy: true, text: i18n.T("正在安装…", "Installing…")}
			}
			return &updateLine{percent: up.Percent, text: fmt.Sprintf(i18n.T("下载 %d%%", "Downloading %d%%"), up.Percent),
				actions: []updateAction{{"update-cancel", i18n.T("取消", "Cancel"), "", func() { go u.app.cancelUpdate() }}}}
		case updateRestarting:
			return &updateLine{percent: -1, busy: true, text: i18n.T("正在重启…", "Restarting…")}
		case updateFailed:
			return &updateLine{dot: "signal", percent: -1, text: i18n.T("升级失败", "Update failed"), tip: up.Error,
				actions: []updateAction{{"update-retry", i18n.T("重试", "Retry"), updateRestartHint(), func() { go u.app.startUpdate() }}}}
		}
	}
	just := u.snap.UpdateInfo.Just
	if just == nil {
		return nil
	}
	dismiss := updateAction{"update-dismiss", i18n.T("知道了", "Dismiss"), "", func() { go u.app.dismissUpdated() }}
	if !just.OK {
		return &updateLine{dot: "signal", percent: -1, text: fmt.Sprintf(i18n.T("升级没有完成，现在仍是 %s", "The update did not finish: still on %s"), u.snap.Version),
			actions: []updateAction{dismiss}}
	}
	return &updateLine{dot: "accent", percent: -1, text: fmt.Sprintf(i18n.T("已升级到 %s", "Updated to %s"), just.To),
		actions: []updateAction{{"update-log", i18n.T("查看更新内容", "See what's new"), "", func() {
			u.panelClose()
			u.openChangelog(just.From)
			go u.app.dismissUpdated()
		}}, dismiss}}
}

func updateBar(c *ui.Context, p *pal, percent int) {
	ui.Box(c).Size(56, 3).Radius(p.r(2)).Background(p.hair).Shrink(0).Children(func() {
		ui.Box(c).Size(float32(56*percent)/100, 3).Radius(p.r(2)).Background(p.accent)
	})
}

func (l *updateLine) dotColor(p *pal) ui.Color {
	if l.dot == "signal" {
		return p.signal
	}
	return p.accent
}

// updateNotice is the update in the title bar, before the state.
func (u *UI) updateNotice(c *ui.Context, p *pal) {
	l := u.updateLine()
	if l == nil {
		return
	}
	ui.Row(c.Key("update")).Gap(8).AlignItems(ui.Center).Shrink(0).Margin(0, 10, 0, 0).Children(func() {
		if l.dot != "" {
			ui.Box(c).Size(7, 7).Radius(p.r(4)).Background(l.dotColor(p)).Shrink(0)
		}
		if l.percent >= 0 {
			updateBar(c, p, l.percent)
		}
		t := ui.Text(c, l.text).FontSize(12.5).FontWeight(500).TextColor(p.ink).NoWrap()
		if l.tip != "" {
			t.Tooltip(l.tip)
		}
		shimmer(t, l.busy, p)
		for _, a := range l.actions {
			ui.ButtonBase(c.Key(a.key)).Shrink(0).Padding(5, 6).Margin(-5, -6).OnClick(a.click).Children(func() {
				t := ui.Text(c, a.label).FontSize(12.5).FontWeight(600).Underline().TextColor(p.ink).NoWrap()
				if a.tip != "" {
					t.Tooltip(a.tip)
				}
			})
		}
	})
}

// panelUpdate is the update in the menu bar panel, under the issue.
func (u *UI) panelUpdate(c *ui.Context, p *pal) {
	l := u.updateLine()
	if l == nil {
		return
	}
	col := p.accent
	if l.dot == "signal" {
		col = p.signal
	}
	ui.Row(c.Key("update")).Margin(0, 10, 10, 10).Padding(8, 10).Radius(p.r(8)).Background(col.Alpha(.11)).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(3, 14).Radius(p.r(2)).Background(col)
		shimmer(ui.Text(c, l.text).FontSize(12).TextColor(p.ink).Grow(1).Basis(0), l.busy, p)
		if l.percent >= 0 {
			updateBar(c, p, l.percent)
		}
		for _, a := range l.actions {
			ui.ButtonBase(c.Key(a.key)).Label(a.label).OnClick(a.click).Children(func() {
				ui.Text(c, a.label).FontSize(12).FontWeight(650).Underline().NoWrap().TextColor(p.ink)
			})
		}
	})
}

// updateRow is 软件更新 on the 关于 page: where the user can look for a new
// version and install it without waiting for the reminder.
func (u *UI) updateRow(c *ui.Context, p *pal) {
	info, up := u.snap.UpdateInfo, u.snap.Update
	title := i18n.T("软件更新", "Software update")
	current := fmt.Sprintf(i18n.T("当前版本 %s", "Version %s"), u.snap.Version)
	log := func() {
		btn(c.Key("changelog"), p, i18n.T("更新内容", "What's new")).OnClick(func() { u.openChangelog("") })
	}
	check := func() {
		shimmer(btn(c.Key("update-check"), p, i18n.T("检查更新", "Check for updates")).OnClick(func() {
			if u.app != nil {
				go func() {
					if err := u.app.checkUpdateNow(); err != nil {
						u.settingsToast(fmt.Sprintf(i18n.T("检查更新失败：%s", "Could not check for updates: %s"), firstLine(err.Error())))
					}
				}()
			}
		}), info.Checking, p)
	}
	start := func(label string) {
		btn(c.Key("update-start"), p, label).OnClick(func() { go u.app.startUpdate() })
	}
	switch {
	case !info.Can:
		optRow(c, p, title, i18n.T("这个版本不能自己升级：开发版，或由系统的包管理器负责更新", "This copy cannot update itself: it is a development build, or its package manager updates it"), log)
	case up == nil:
		desc := current
		if info.Checking {
			desc = i18n.T("正在检查…", "Checking…")
		} else if info.Latest {
			desc = fmt.Sprintf(i18n.T("已是最新版本 %s", "%s is the latest version"), u.snap.Version)
		}
		optRow(c, p, title, desc, func() { log(); check() })
	case up.Status == updateAvailable:
		optRow(c, p, title, fmt.Sprintf(i18n.T("新版本 %s", "Version %s"), up.Version)+"，"+updateRestartHint(), func() { log(); start(i18n.T("升级", "Update")) })
	case up.Status == updateDownloading && up.Percent < 100:
		optRow(c, p, title, fmt.Sprintf(i18n.T("下载 %d%%", "Downloading %d%%"), up.Percent), func() {
			updateBar(c, p, up.Percent)
			btn(c.Key("update-cancel"), p, i18n.T("取消", "Cancel")).OnClick(func() { go u.app.cancelUpdate() })
		})
	case up.Status == updateFailed:
		optRow(c, p, title, i18n.T("升级失败", "Update failed")+"："+firstLine(up.Error), func() { log(); start(i18n.T("重试", "Retry")) })
	case up.Status == updateRestarting:
		optRow(c, p, title, i18n.T("正在重启…", "Restarting…"), func() {})
	default:
		optRow(c, p, title, i18n.T("正在安装…", "Installing…"), func() {})
	}
}

// ---- The changelog ----

//go:embed CHANGELOG.md
var changelogText string

// changelogEntry is one version's section of CHANGELOG.md.
type changelogEntry struct {
	Version, Date string
	Items         []string
}

var changelogHeading = regexp.MustCompile(`^## \[?v?(\d+\.\d+\.\d+[^\]\s]*)\]?(?:\s+-\s+(\S+))?`)

// parseChangelog reads the versions, newest first as the file has them.
// Sections that are not a version, as Unreleased, are no one's to read yet.
func parseChangelog(text string) []changelogEntry {
	var all []changelogEntry
	in := false
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			m := changelogHeading.FindStringSubmatch(line)
			in = m != nil
			if in {
				all = append(all, changelogEntry{Version: m[1], Date: m[2]})
			}
			continue
		}
		if item, ok := strings.CutPrefix(line, "- "); ok && in {
			all[len(all)-1].Items = append(all[len(all)-1].Items, item)
		}
	}
	return all
}

// changelogSince is the versions down to from, which it includes. Without
// from, or when the file does not know it, it is all of them.
func changelogSince(all []changelogEntry, from string) []changelogEntry {
	for i, e := range all {
		if e.Version == from {
			return all[:i+1]
		}
	}
	return all
}

// changelogUI is the 更新内容 window.
type changelogUI struct {
	win atomic.Pointer[mygo.Window]
	// from is the version the list goes back to, "" for all of them.
	from string
}

// openChangelog shows the changes since from, or all of them. On the main
// thread.
func (u *UI) openChangelog(from string) {
	u.clog.from = from
	if u.app == nil {
		return
	}
	if w := u.clog.win.Load(); w != nil {
		w.Invalidate()
		w.Show()
		w.Focus()
		return
	}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: i18n.T("更新内容", "What's new"), Width: 560, Height: 620, MinWidth: 420, MinHeight: 360,
		TitleBarStyle: mygo.TitleBarHidden, StateKey: "changelog",
		TrafficLightPosition: &mygo.Point{X: 18, Y: 18},
		BackgroundColor:      "light-dark(#f6f4ef, #171615)",
		DisableMinimize:      true, DisableFullScreen: true,
		Content: ui.View(u.changelogView),
	})
	u.clog.win.Store(w)
	w.OnClosed(func() {
		u.clog.win.CompareAndSwap(w, nil)
		u.app.syncDock()
	})
	u.app.syncDock()
	w.Focus()
	mygo.App.Focus()
	// A first frame with this much new text can come out with its glyphs
	// missing, and nothing here changes to draw it again.
	time.AfterFunc(100*time.Millisecond, w.Invalidate)
}

func (u *UI) changelogView(c *ui.Context) {
	useLook(u.shownSettings())
	p := windowPal(c, palette(c))
	applyTheme(c, p, p.font != sans)
	c.OnShortcut(ui.Cmd, ui.KeyW, func() {
		if w := u.clog.win.Load(); w != nil {
			w.Close()
		}
	})
	all := parseChangelog(changelogText)
	list := changelogSince(all, u.clog.from)
	ui.Column(c).Absolute().Fill().Background(p.paper).Children(func() {
		ui.Row(c).Height(max(c.TitleBar().Height, 50)).Shrink(0).Padding(0, 26, 0, 92).Gap(12).AlignItems(ui.Center).DragWindow().Children(func() {
			disp(ui.Text(c, i18n.T("更新内容", "What's new")), p).FontSize(17).FontWeight(700).TextColor(p.ink).Grow(1)
			if len(list) < len(all) {
				ui.ButtonBase(c.Key("all")).Shrink(0).OnClick(func() { u.clog.from = "" }).Children(func() {
					ui.Text(c, i18n.T("查看全部版本", "All versions")).FontSize(12.5).FontWeight(600).Underline().TextColor(p.ink).NoWrap()
				})
			}
		})
		ui.Scroll(c.Key("list")).Grow(1).Padding(6, 26, 24, 26).Children(func() {
			ui.Column(c).Gap(24).Children(func() {
				for _, e := range list {
					u.changelogEntry(c, p, e)
				}
			})
		})
	})
}

func (u *UI) changelogEntry(c *ui.Context, p *pal, e changelogEntry) {
	ui.Column(c.Key(e.Version)).Gap(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			disp(ui.Text(c, e.Version), p).FontSize(17).FontWeight(700).TextColor(p.ink)
			if e.Version == u.snap.Version {
				changelogTag(c, p, i18n.T("当前版本", "Current"), true)
			}
			if e.Version == u.snap.UpdateInfo.From {
				changelogTag(c, p, i18n.T("升级前版本", "Before the update"), false)
			}
			ui.Spacer(c)
			ui.Text(c, e.Date).FontSize(12).FontFeatures("tnum").TextColor(p.mute)
		})
		for _, item := range e.Items {
			ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
				ui.Text(c, "•").FontSize(13).TextColor(p.mute)
				ui.Text(c, item).FontSize(13).LineHeight(1.5).TextColor(p.ink2).Grow(1).Basis(0)
			})
		}
	})
}

func changelogTag(c *ui.Context, p *pal, label string, now bool) {
	bg, col := p.hair2, p.ink2
	if now {
		bg, col = p.accent, p.onAccent
	}
	ui.Box(c).Padding(2, 8).Radius(p.r(999)).Background(bg).Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(650).TextColor(col).NoWrap()
	})
}
