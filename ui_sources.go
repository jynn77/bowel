package main

// Download sources, in the expanded 模式 cell under 开发工具: where npm,
// pip, Go and Cargo get their packages, whether that answers, and one
// press to put it right when it does not. Each is a built-in service of a
// route group, so its official address follows that group; using a mirror
// instead is the tool's own setting, which bowel changes on the press.
// See internal/devtools/sources.go.

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/devtools"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

// nested is what belongs to the row above it: set in, with a rule down
// its left side, so that a list inside a list still reads as one.
func nested(c *ui.Context, p *pal, content func()) {
	ui.Column(c).Margin(2, 0, 10, 8).Padding(0, 0, 0, 16).BorderWidth(0, 0, 0, 2).BorderColor(p.hair).Children(content)
}

// sourceRoute is the route group a source's official address goes by.
func (u *UI) sourceRoute(id string) (r routes.Route, ok bool) {
	s := u.shownSettings()
	all := routes.Build(s.Routing, s.Rules)
	for _, g := range all {
		for _, sv := range g.Services {
			if sv.Builtin && sv.ID == id {
				if !sv.Off {
					return g, true
				}
				// Switched off, its traffic falls to the default group.
				for _, d := range all {
					if d.ID == routes.Default {
						return d, true
					}
				}
			}
		}
	}
	return routes.Route{}, false
}

func (u *UI) sourceFix(src devtools.Source) (kind, mirror string) {
	r, _ := u.sourceRoute(src.ID)
	return src.Fix(r.Direct)
}

func sourcesDown(list []devtools.Source) []devtools.Source {
	var out []devtools.Source
	for _, s := range list {
		if s.Down() {
			out = append(out, s)
		}
	}
	return out
}

func msWord(p devtools.Probe, measured bool) string {
	switch {
	case !measured:
		return ""
	case !p.OK:
		return i18n.T("连不上", "No answer")
	}
	return fmt.Sprintf("%d ms", p.Ms)
}

// wayName is one way of reaching a source, as a row names it.
func (u *UI) wayName(src devtools.Source, mirror string) string {
	if mirror != "" {
		return devtools.MirrorHost(mirror)
	}
	r, ok := u.sourceRoute(src.ID)
	switch {
	case !ok:
		return i18n.T("官方源", "Official source")
	case r.Direct:
		return i18n.T("官方源 · 直接连接", "Official source · direct")
	}
	return fmt.Sprintf(i18n.T("官方源 · 走「%s」", "Official source · via %s"), routes.Title(r))
}

// fixWord is what the fix will do, for its tooltip.
func fixWord(kind, mirror string) string {
	switch kind {
	case devtools.FixMirror:
		return fmt.Sprintf(i18n.T("换成镜像 %s", "Switch to the mirror %s"), devtools.MirrorHost(mirror))
	case devtools.FixOfficial:
		return i18n.T("换回官方源", "Go back to the official source")
	case devtools.FixRoute:
		return i18n.T("让它重新走节点：恢复这个服务默认的路由组", "Send it through a node again: put its service back in its default route group")
	}
	return ""
}

// fixSource does what sourceFix found.
func (u *UI) fixSource(src devtools.Source) {
	kind, mirror := u.sourceFix(src)
	switch kind {
	case devtools.FixMirror, devtools.FixOfficial:
		u.do(func() error { return u.app.setSource(src.ID, mirror) })
	case devtools.FixRoute:
		u.changeRouting(func(st *settings.Settings) { st.Rules = st.Routing.RemoveService(st.Rules, src.ID) }, func() { u.app.probeSources() })
	}
}

// modeSourcesLine is the line on the 模式 cell, only when a source is down
// and the tools' line is not already there.
func (u *UI) modeSourcesLine(c *ui.Context, p *pal) {
	down := sourcesDown(u.snap.Sources)
	if len(down) == 0 || len(toolsMissing(u.snap.Tools)) > 0 {
		return
	}
	ui.ButtonBase(c.Key("sources-line")).Margin(6, 0, 0, 0).AlignSelf(ui.Start).OnClick(func() {
		u.home.x, u.home.sub, u.home.srcList = "mode", "", true
	}).Children(func() {
		ui.Text(c, sourcesLine(down)+" ›").FontSize(11.5).FontWeight(600).TextColor(p.warn).NoWrap()
	})
}

func sourcesLine(down []devtools.Source) string {
	if len(down) == 1 {
		return fmt.Sprintf(i18n.T("%s 的下载源连不上", "%s can't reach its source"), down[0].Tool)
	}
	return fmt.Sprintf(i18n.T("%d 个下载源连不上", "%d download sources can't be reached"), len(down))
}

// sourcesSection is the section of the expanded 模式 cell: one sentence,
// the fix when one is needed, and a source to a row when asked for.
func (u *UI) sourcesSection(c *ui.Context, p *pal) {
	h := &u.home
	list := u.snap.Sources
	if len(list) == 0 {
		return
	}
	// A source can be changed in a terminal too: look again while shown.
	if u.app != nil && c.Now().Sub(h.srcAt) > 3*time.Second {
		h.srcAt = c.Now()
		go func() {
			u.app.refreshSources()
			u.app.probeSourcesIfOlder(time.Minute)
		}()
	}
	down := sourcesDown(list)
	var fixable []devtools.Source
	for _, s := range down {
		if kind, _ := u.sourceFix(s); kind != "" {
			fixable = append(fixable, s)
		}
	}
	line, col := fmt.Sprintf(i18n.T("%d 个下载源都通", "All %d download sources answer"), len(list)), p.ink
	if len(down) > 0 {
		line, col = sourcesLine(down), p.warn
	}
	label := i18n.T("查看", "Show")
	if h.srcList {
		label = i18n.T("收起", "Hide")
	}
	section(c, p, i18n.T("依赖源", "Download sources"), "", func() {
		ui.Text(c, line).FontSize(13).FontWeight(600).TextColor(col)
	}, func() {
		ui.Row(c).Gap(8).Children(func() {
			if len(fixable) > 0 {
				pill(c.Key("sources-fix"), p, i18n.T("一键修复", "Fix"), true).OnClick(func() {
					for _, s := range fixable {
						u.fixSource(s)
					}
				})
			}
			btnChevron(c.Key("sources-toggle"), p, label, h.srcList).OnClick(func() { h.srcList = !h.srcList })
		})
	})
	if !h.srcList {
		return
	}
	nested(c, p, func() {
		for _, s := range list {
			u.sourceRow(c, p, s)
		}
	})
}

// sourceRow is one tool: what it downloads from now and how that answers,
// the fix when it does not, and the other ways only when asked for.
func (u *UI) sourceRow(c *ui.Context, p *pal, src devtools.Source) {
	h := &u.home
	open := h.srcOpen == src.ID
	busy := u.pending("source:" + src.ID)
	now, measured := src.ProbeOf(src.Mirror)
	ui.Column(c.Key(src.ID)).BorderWidth(0, 0, 1, 0).BorderColor(p.hair2).Children(func() {
		ui.Row(c).Gap(14).Padding(10, 0).AlignItems(ui.Center).Children(func() {
			disp(ui.Text(c, src.Tool), p).FontSize(15).FontWeight(700).TextColor(p.ink).Width(86).Shrink(0)
			say, col := u.wayName(src, src.Mirror), p.ink2
			if src.Mirror != "" {
				say = fmt.Sprintf(i18n.T("镜像 %s", "Mirror %s"), say)
			}
			if w := msWord(now, measured); w != "" {
				say += " · " + w
			}
			if src.Down() {
				col = p.signal
			}
			if busy {
				say = i18n.T("正在切换…", "Switching…")
			}
			shimmer(ui.Text(c, say).FontSize(12.5).FontWeight(600).TextColor(col).SingleLine().Grow(1).Basis(0).MinWidth(0), busy, p)
			if busy {
				return
			}
			if kind, m := u.sourceFix(src); kind != "" {
				btn(c.Key("fix"), p, i18n.T("修复", "Fix")).Tooltip(fixWord(kind, m)).OnClick(func() { u.fixSource(src) })
			}
			label := i18n.T("换源", "Change")
			if open {
				label = i18n.T("收起", "Hide")
			}
			btnChevron(c.Key("ways"), p, label, open).OnClick(func() {
				if open {
					h.srcOpen = ""
				} else {
					h.srcOpen = src.ID
				}
			})
		})
		if !open {
			return
		}
		nested(c, p, func() {
			for _, m := range append([]string{""}, src.Mirrors...) {
				u.sourceWay(c, p, src, m, busy)
			}
		})
	})
}

// sourceWay is one way of reaching a source: pressing it uses it.
func (u *UI) sourceWay(c *ui.Context, p *pal, src devtools.Source, mirror string, busy bool) {
	key := mirror
	if key == "" {
		key = "official"
	}
	pr, measured := src.ProbeOf(mirror)
	ui.Row(c.Key(key)).Gap(12).MinHeight(36).Padding(4, 0).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(p.hair2).Children(func() {
		name := ui.Text(c, u.wayName(src, mirror)).FontSize(12.5).TextColor(p.ink).SingleLine().Grow(1).Basis(0).MinWidth(0)
		if mirror != "" {
			name.Font(actMono)
		}
		if w := msWord(pr, measured); w != "" {
			col := p.mute
			if !pr.OK {
				col = p.signal
			}
			ui.Text(c, w).FontSize(12).TextColor(col).NoWrap()
		}
		switch {
		case mirror == src.Mirror:
			ui.Text(c, i18n.T("使用中", "In use")).FontSize(12).FontWeight(600).TextColor(p.good).NoWrap().Padding(0, 11)
		case !busy:
			btn(c, p, i18n.T("用这个", "Use this")).OnClick(func() {
				u.do(func() error { return u.app.setSource(src.ID, mirror) })
			})
		}
	})
}
