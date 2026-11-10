package main

// 开发工具, inside the 模式 cell: which tools reach the proxy, a switch for
// each, and what quitting will break.

import (
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/devtools"
	"github.com/oboo/moho/internal/i18n"
)

func toolName(id string) string {
	switch id {
	case devtools.Dockerd:
		return i18n.T("Docker 拉镜像", "Docker pulls")
	case devtools.Containers:
		return i18n.T("容器和 docker build", "Containers and docker build")
	case devtools.Shell:
		return i18n.T("终端环境", "Terminal")
	case devtools.Langs:
		return "Go · pip · Homebrew"
	case devtools.GitHTTPS:
		return "Git HTTPS"
	case devtools.GitSSH:
		return "Git SSH"
	}
	return id
}

func toolSub(id string) string {
	switch id {
	case devtools.Dockerd:
		return "Colima · dockerd"
	case devtools.Containers:
		return i18n.T("含里面的 apt、pip、npm", "With the apt, pip and npm inside")
	case devtools.Shell:
		return "zsh"
	case devtools.Langs:
		return i18n.T("读环境变量的工具", "Tools that read the environment")
	case devtools.GitHTTPS:
		return "github.com"
	case devtools.GitSSH:
		return "git@github.com"
	}
	return ""
}

func toolStateWord(s string) string {
	switch s {
	case devtools.On:
		return i18n.T("走 bowel", "Via bowel")
	case devtools.Off:
		return i18n.T("不走", "Not via bowel")
	case devtools.Other:
		return i18n.T("走别的代理", "Another proxy")
	case devtools.Stale:
		return i18n.T("地址过期", "Out of date")
	}
	return i18n.T("未知", "Unknown")
}

func (p *pal) toolStateColor(s string) ui.Color {
	switch s {
	case devtools.On:
		return p.good
	case devtools.Off:
		return p.signal
	case devtools.Other, devtools.Stale:
		return p.warn
	}
	return p.mute
}

func (u *UI) toolWhy(t devtools.Tool) string {
	switch {
	case t.State == devtools.Unknown:
		return i18n.T("读不到有效配置，展开后实测确认", "Couldn't be read. Open it and test")
	case t.Pending:
		return i18n.T("配置已写入，重启 Colima 后生效", "Written. Takes effect when Colima restarts")
	case t.State == devtools.Stale:
		return fmt.Sprintf(i18n.T("指向 %d，bowel 现在用 %d，工具连不上代理", "Points at %d, but bowel now uses %d, so it can't reach the proxy"), t.Port, u.snap.Settings.MixedPort)
	case t.Applied == devtools.AppliedProxy:
		return i18n.T("已写入代理，指向 bowel 当前端口", "Proxy written, pointing at bowel's current port")
	case t.Applied == devtools.AppliedDirect:
		return i18n.T("已单独关闭这个工具的代理，直接连接", "Its proxy is turned off; it connects directly")
	case t.Source == devtools.SourceFollow:
		return i18n.T("继承终端的环境变量，没有单独配置", "Follows the terminal's environment, with no setting of its own")
	case t.State == devtools.Other:
		return fmt.Sprintf(i18n.T("指向 %s，不是 bowel 的端口", "Points at %s, which isn't bowel's port"), t.Value)
	case t.Source == devtools.SourceOwn && t.State == devtools.On:
		return fmt.Sprintf(i18n.T("你自己配的，指向 %s", "Your own setting, pointing at %s"), t.Value)
	case t.Source == devtools.SourceOwn:
		return i18n.T("你自己关掉了代理", "You turned its proxy off yourself")
	}
	return i18n.T("没有配置代理", "No proxy is set")
}

func toolQuitSay(id string) string {
	if id == devtools.Dockerd {
		return i18n.T("docker pull 会失败", "docker pull will fail")
	}
	return i18n.T("新建的容器和 docker build 会失败", "New containers and docker build will fail")
}

func toolsMissing(list []devtools.Tool) []devtools.Tool {
	var out []devtools.Tool
	for _, t := range list {
		if t.State != devtools.On {
			out = append(out, t)
		}
	}
	return out
}

// toolsAtRisk are those that fail without bowel running.
func toolsAtRisk(list []devtools.Tool) []devtools.Tool {
	var out []devtools.Tool
	for _, t := range list {
		if t.Guard == devtools.GuardNeed && t.Applied == devtools.AppliedProxy {
			out = append(out, t)
		}
	}
	return out
}

// modeToolsLine is the line on the 模式 cell.
func (u *UI) modeToolsLine(c *ui.Context, p *pal) {
	n := len(toolsMissing(u.snap.Tools))
	if n == 0 {
		return
	}
	ui.ButtonBase(c.Key("tools-line")).Margin(6, 0, 0, 0).AlignSelf(ui.Start).OnClick(func() {
		u.home.x, u.home.sub, u.home.toolsList = "mode", "", true
	}).Children(func() {
		ui.Text(c, fmt.Sprintf(i18n.T("%d 个开发工具未接入流量 ›", "%d developer tools aren't on the proxy ›"), n)).FontSize(11.5).FontWeight(600).TextColor(p.warn).NoWrap()
	})
}

// toolsSection is the section of the expanded 模式 cell: a tool to a row,
// with what it is doing and what can be done about it.
func (u *UI) toolsSection(c *ui.Context, p *pal) {
	h := &u.home
	if u.app != nil && c.Now().Sub(h.toolsAt) > time.Minute {
		h.toolsAt = c.Now()
		go u.app.refreshTools()
	}
	list, miss := u.snap.Tools, toolsMissing(u.snap.Tools)
	line, col := i18n.T("开发工具都已接入流量", "All developer tools are on the proxy"), p.ink
	if len(miss) > 0 {
		line, col = fmt.Sprintf(i18n.T("%d 个开发工具未接入流量", "%d developer tools aren't on the proxy"), len(miss)), p.warn
	}
	label := i18n.T("查看 ▾", "Show ▾")
	if h.toolsList {
		label = i18n.T("收起 ▴", "Hide ▴")
	}
	section(c, p, i18n.T("开发工具", "Developer tools"), "", func() {
		ui.Text(c, line).FontSize(13).FontWeight(600).TextColor(col)
	}, func() {
		ui.Row(c).Gap(8).Children(func() {
			u.toolsUndoAll(c, p)
			btn(c.Key("tools-toggle"), p, label).OnClick(func() { h.toolsList = !h.toolsList })
		})
	})
	if !h.toolsList {
		return
	}
	for _, t := range list {
		u.toolRow(c, p, t)
	}
}

func (u *UI) toolsUndoAll(c *ui.Context, p *pal) {
	h := &u.home
	var mine int
	restarts := false
	for _, t := range u.snap.Tools {
		if t.Source == devtools.SourceBowel {
			mine++
			restarts = restarts || t.Restart
		}
	}
	if mine == 0 {
		return
	}
	label := fmt.Sprintf(i18n.T("全部撤销 %d", "Undo all %d"), mine)
	if h.toolsArmed {
		label = i18n.T("再点一次，会重启 Colima", "Press again; Colima will restart")
	}
	shimmer(btn(c.Key("tools-undo"), p, label).OnClick(func() {
		if restarts && !h.toolsArmed {
			h.toolsArmed = true
			return
		}
		h.toolsArmed = false
		u.do(u.app.undoAllTools)
	}), u.pending("tools"), p)
}

func (u *UI) toolRow(c *ui.Context, p *pal, t devtools.Tool) {
	ui.Row(c.Key(t.ID)).Gap(14).Padding(10, 0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.hair2).Children(func() {
		ui.Column(c).Width(220).Shrink(0).Gap(1).Children(func() {
			disp(ui.Text(c, toolName(t.ID)), p).FontSize(15).FontWeight(700).TextColor(p.ink).MaxLines(1).Ellipsis("…")
			ui.Text(c, toolSub(t.ID)).FontSize(11.5).TextColor(p.mute).MaxLines(1).Ellipsis("…")
		})
		ui.Text(c, toolStateWord(t.State)).FontSize(12.5).FontWeight(600).TextColor(p.toolStateColor(t.State)).NoWrap().Grow(1).Tooltip(u.toolWhy(t))
		ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() { u.toolActions(c, p, t) })
	})
}

// toolActions are the row's buttons, or why there are none.
func (u *UI) toolActions(c *ui.Context, p *pal, t devtools.Tool) {
	if u.pending("tool:" + t.ID) {
		word := i18n.T("正在写入…", "Writing…")
		if t.Restart {
			word = i18n.T("正在重启 Colima…", "Restarting Colima…")
		}
		shimmer(ui.Text(c, word).FontSize(12).TextColor(p.ink2).NoWrap(), true, p)
		return
	}
	if t.ID == devtools.Shell {
		btn(c.Key("copy"), p, i18n.T("复制命令", "Copy command")).Tooltip(i18n.T("只影响贴入的那个终端窗口", "Only the terminal window you paste it into")).OnClick(func() {
			c.WriteClipboard(u.snap.Dev.Exports)
			u.toast(i18n.T("已复制代理命令", "Proxy command copied"))
		})
	}
	var acted bool
	action := func(kind, label string) {
		if kind == "" {
			return
		}
		acted = true
		u.toolButton(c, p, t, kind, label)
	}
	action(t.ProxyAction(), map[string]string{
		devtools.KindApply:  i18n.T("接入", "Connect"),
		devtools.KindUpdate: i18n.T("更新地址", "Update address"),
		devtools.KindUndo:   i18n.T("恢复代理", "Restore proxy"),
	}[t.ProxyAction()])
	action(t.DirectAction(), map[string]string{
		devtools.KindUndo:   i18n.T("撤销", "Undo"),
		devtools.KindDirect: i18n.T("改为直连", "Make direct"),
	}[t.DirectAction()])
	if acted || t.ID == devtools.Shell {
		return
	}
	switch t.Source {
	case devtools.SourceOwn:
		ui.Text(c, i18n.T("你自己配的", "Your own setting")).FontSize(12).TextColor(p.mute).NoWrap()
	case devtools.SourceFollow:
		ui.Text(c, i18n.T("跟随终端", "Follows the terminal")).FontSize(12).TextColor(p.mute).NoWrap()
	}
}

// toolButton does its change on the click, except one that restarts Colima
// and so stops the containers, which asks for a second click.
func (u *UI) toolButton(c *ui.Context, p *pal, t devtools.Tool, kind, label string) {
	h := &u.home
	armed := t.Restart && h.toolArmed == t.ID+kind
	if armed {
		label = i18n.T("再点一次，会重启 Colima", "Press again; Colima will restart")
	}
	b := btn(c.Key(kind), p, label).OnClick(func() {
		if t.Restart && !armed {
			h.toolArmed = t.ID + kind
			return
		}
		h.toolArmed = ""
		u.do(func() error { return u.app.changeTool(t.ID, kind) })
	})
	switch {
	case kind != devtools.KindApply:
	case t.Guard == devtools.GuardNeed:
		b.Tooltip(i18n.T("bowel 不运行时，这个工具会连不上代理", "When bowel isn't running, this tool can't reach the proxy"))
	case t.Guard == devtools.GuardAuto:
		b.Tooltip(i18n.T("bowel 不运行时自动直连，不受影响", "When bowel isn't running, it connects directly"))
	}
}

// askQuit quits, first saying what will stop working if anything will.
// Only bowel's own quit commands come here: the system's, which a logout
// also uses, must not be held up.
func (u *UI) askQuit() {
	if len(toolsAtRisk(u.snap.Tools)) == 0 {
		mygo.App.Quit()
		return
	}
	u.panelShow(func() { u.quitOpen = true })
}

func (u *UI) quitDialog(c *ui.Context, p *pal) {
	if !u.quitOpen {
		return
	}
	risk := toolsAtRisk(u.snap.Tools)
	var can []devtools.Tool
	for _, t := range risk {
		if !t.Restart {
			can = append(can, t)
		}
	}
	ui.Modal(c.Key("quit-tools"), &u.quitOpen, func() {
		ui.Column(c).Gap(10).Width(420).Children(func() {
			disp(ui.Text(c, fmt.Sprintf(i18n.T("退出后，%d 个工具会连不上", "After quitting, %d tools won't connect"), len(risk))), p).FontSize(18).FontWeight(700).TextColor(p.ink)
			for _, t := range risk {
				say := toolQuitSay(t.ID)
				if t.Restart {
					say += i18n.T("。撤销要重启 Colima，不在退出时处理", ". Undoing restarts Colima, so it isn't done on the way out")
				}
				ui.Column(c.Key(t.ID)).Gap(1).Children(func() {
					ui.Text(c, toolName(t.ID)).FontSize(12.5).FontWeight(600).TextColor(p.ink)
					ui.Text(c, say).FontSize(12.5).TextColor(p.mute)
				})
			}
			ui.Row(c).Gap(8).Margin(4, 0, 0, 0).Children(func() {
				if len(can) > 0 {
					pill(c.Key("undo-quit"), p, fmt.Sprintf(i18n.T("撤销 %d 项后退出", "Undo %d and quit"), len(can)), true).OnClick(func() {
						u.quitOpen = false
						u.do(func() error {
							defer mygo.App.Quit()
							for _, t := range can {
								if err := u.app.changeTool(t.ID, devtools.KindUndo); err != nil {
									return err
								}
							}
							return nil
						})
					})
				}
				btn(c.Key("force-quit"), p, i18n.T("仍然退出", "Quit anyway")).OnClick(func() { u.quitOpen = false; mygo.App.Quit() })
				btn(c.Key("cancel-quit"), p, i18n.T("取消", "Cancel")).OnClick(func() { u.quitOpen = false })
			})
		})
	})
}
