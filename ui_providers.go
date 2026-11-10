package main

// Provider: where nodes come from. A head with the count and 全部更新, an
// add bar that takes whatever is pasted (a subscription URL, node links,
// Clash YAML) and says what it will become, and one row per provider:
// usage, expiry, the delays of its nodes, auto-update and its actions.
// A row opens to its nodes, where they leave, and which routes use them.
//
// Design: docs/product-ia.md (§5 Provider) and prototype/index.html
// (providers, provRow).

import (
	"context"
	"fmt"
	"image"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/qr"
)

type providersUI struct {
	input string // the add bar's text
	focus bool   // give the add bar the focus in the next frame
	empty bool   // the list was empty in the last frame

	open    string    // the provider whose nodes show, "" for none
	confirm string    // the provider whose 删除 asks to be pressed again
	confAt  time.Time // when it started asking

	renaming string // the provider being renamed
	newName  string

	popup bool // a select's options are open (they take Escape)

	// The camera's dialog: scanning for a code to add.
	camOpen  bool
	cam      *qr.Camera // open while the dialog shows
	camGen   int        // which opening the camera's goroutine serves
	camFrame *ui.Bitmap // the last frame, shown mirrored
	camW     float32    // its aspect ratio
	camErr   string

	// The share dialog: a provider's code, its link, copy and save.
	shareOpen bool
	shareID   string
	shareText string
	shareCode *image.Gray
	shareBmp  *ui.Bitmap
	shareErr  string

	// detect tells what pasted text is; nil uses the app's (or the
	// provider package's, without an app).
	detect func(string) (string, error)
	// share gives what shares a provider; nil uses the app's.
	share func(id string) (string, error)
}

// provDetect tells what the add bar's text will be added as.
func (u *UI) provDetect(s string) (string, error) {
	switch {
	case u.prov.detect != nil:
		return u.prov.detect(s)
	case u.app != nil:
		return u.app.detectProvider(s)
	}
	k, err := provider.Detect(s)
	return string(k), err
}

// provConfirmFor is how long 删除 asks to be pressed again.
const provConfirmFor = 4 * time.Second

// provKindWord is a provider kind in the interface's language.
func provKindWord(kind string) string {
	switch kind {
	case "subscription":
		return i18n.T("订阅", "Subscription")
	case "link":
		return i18n.T("节点链接", "Node links")
	case "file":
		return i18n.T("本地文件", "Local file")
	}
	return ""
}

var provRowMotion = ui.ElementTransition{Animation: &motion, Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true}}

func (u *UI) providersPage(c *ui.Context, p *pal) {
	pr := &u.prov
	empty := len(u.snap.Providers) == 0
	if empty && !pr.empty {
		pr.focus = true // nothing to list: the add bar is the page
	}
	pr.empty = empty
	if pr.confirm != "" {
		if left := provConfirmFor - c.Now().Sub(pr.confAt); left > 0 {
			c.After(left) // stop asking then
		} else {
			pr.confirm = ""
		}
	}
	u.provPaste(c)
	pr.popup = false
	u.provCameraTick()

	ui.Column(c).Fill().Children(func() {
		u.provHead(c, p)
		u.provAddBar(c, p)
		ui.Scroll(c).Grow(1).Padding(0, 26, 18, 26).Children(func() {
			if empty {
				u.provEmpty(c, p)
				return
			}
			ui.Column(c).Children(func() {
				for _, pv := range u.snap.Providers {
					u.provRow(c, p, pv)
				}
			})
		})
	})
	u.provCameraDialog(c, p)
	u.provShareDialog(c, p)
	// Escape steps back: from renaming, from 确认删除, from an open row.
	// Declared after the rows, and only while there is something to step
	// back from and no select's options are open, which take Escape.
	if !pr.popup && (pr.renaming != "" || pr.confirm != "" || pr.open != "") {
		c.OnWindowShortcut(0, ui.KeyEscape, func() {
			switch {
			case pr.renaming != "":
				pr.renaming = ""
			case pr.confirm != "":
				pr.confirm = ""
			default:
				pr.open = ""
			}
		})
	}
}

// provPaste is ⌘V anywhere on the page, outside a text field: into the add
// bar, or, for a picture, a QR code copied or screenshotted.
func (u *UI) provPaste(c *ui.Context) {
	pr := &u.prov
	if c.FocusTakesText() || !c.Shortcut(ui.Cmd, ui.KeyV) {
		return
	}
	if s := strings.TrimSpace(c.ReadClipboard()); s != "" {
		pr.input, pr.focus = s, true
	} else if u.app != nil {
		u.provQR(func(context.Context) (string, error) {
			s, ok, err := u.app.qrClipboard()
			if !ok && err == nil {
				return "", nil
			}
			return s, err
		})
	}
}

// ---- Head ----

func (u *UI) provHead(c *ui.Context, p *pal) {
	nodes, warn, subs := 0, 0, 0
	for _, pv := range u.snap.Providers {
		nodes += pv.Nodes
		if pv.Warn != "" || pv.Error != "" {
			warn++
		}
		if pv.Kind == "subscription" && pv.Enabled {
			subs++
		}
	}
	title := fmt.Sprintf(i18n.T("%d 个来源", "%d sources"), len(u.snap.Providers))
	sub := nodesCount(nodes)
	if warn > 0 {
		sub += fmt.Sprintf(i18n.T(" · %d 个需要关注", " · %d need attention"), warn)
	}
	pageHead(c, p, i18n.T("提供商", "Provider"), title, sub, true, func() {
		if subs > 0 {
			busy := u.pending("providers")
			label := i18n.T("全部更新", "Update all")
			if busy {
				label = fmt.Sprintf(i18n.T("正在更新 %d 个订阅…", "Updating %d subscriptions…"), subs)
			}
			b := ui.ButtonBase(c).Padding(5, 11).Radius(p.r(7)).Border(1, p.hair).Label(i18n.T("全部更新", "Update all"))
			if b.Hovered() {
				b.BorderColor(p.ink)
			}
			b.OnClick(func() {
				if u.app != nil && !busy {
					u.do(func() error { return u.app.updateAll(bg()) })
				}
			}).Children(func() {
				shimmer(ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(p.ink), busy, p)
			})
		}
	})
}

// ---- Add bar ----

func (u *UI) provAddBar(c *ui.Context, p *pal) {
	ui.Column(c).Gap(8).Margin(0, 26).Padding(12, 0, 14, 0).BorderWidth(0, 0, 1, 0).BorderColor(p.hair).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
			in := u.provField(c, p, u.provAdd).Lines(1, 5).Grow(1).Basis(0).Padding(9, 12).Radius(p.r(10)).
				Background(p.card).Border(1, p.hair).FontSize(13)
			if in.Focused() {
				in.BorderColor(p.ink)
			}
			u.provScanBtn(c, p)
			u.provFileBtn(c, p, u.toast)
			u.provAddBtn(c, p, func() { u.provAdd(u.prov.input) })
		})
		u.provDetectLine(c, p)
	})
}

// provField is the text area that takes what is pasted, for the add bar
// and the first run to dress. Enter submits it to add.
func (u *UI) provField(c *ui.Context, p *pal, add func(string)) ui.Element {
	pr := &u.prov
	in := ui.TextAreaBase(c.Key("add"), &pr.input).
		Placeholder(i18n.T("粘贴订阅链接、节点链接或 Clash 配置", "Paste a subscription link, node links or a Clash config")).
		Label(i18n.T("添加提供商", "Add provider")).
		OnTextKey(func(k ui.TextKey) bool {
			if k.Key == ui.KeyEnter && k.Mods == 0 {
				add(k.Text())
				return true
			}
			return false // Shift+Enter breaks the line
		})
	if pr.focus {
		in.Focus()
		pr.focus = false
	}
	return in
}

// provFileBtn is 选择文件…; fail hears what went wrong.
func (u *UI) provFileBtn(c *ui.Context, p *pal, fail func(string)) {
	adding := u.pending("add")
	u.provBtn(c, p, i18n.T("选择文件…", "Choose file…"), false).Height(36).OnClick(func() {
		if u.app == nil || adding {
			return
		}
		go func() {
			pv, err := u.app.addProviderFile(bg())
			switch {
			case err != nil:
				fail(err.Error())
			case pv != nil && u.win != nil:
				id := pv.ID
				u.win.Update(func() { u.prov.open = id })
			}
		}()
	})
}

// provAddBtn is 添加, which shimmers while it adds.
func (u *UI) provAddBtn(c *ui.Context, p *pal, click func()) {
	adding := u.pending("add")
	label := i18n.T("添加", "Add")
	if adding {
		label = i18n.T("添加中…", "Adding…")
	}
	b := ui.ButtonBase(c).Height(36).MinWidth(84).Padding(0, 18).Justify(ui.Center).Radius(p.r(999)).Background(p.accent).Label(i18n.T("添加", "Add"))
	if strings.TrimSpace(u.prov.input) == "" && !adding {
		b.Opacity(.35)
	}
	b.OnClick(click).Children(func() {
		shimmer(ui.Text(c, label).FontSize(12.5).FontWeight(600).TextColor(p.onAccent), adding, p).TextColor(p.onAccent)
	})
}

// provDetectLine says what the add bar's text will be added as.
func (u *UI) provDetectLine(c *ui.Context, p *pal) {
	text := strings.TrimSpace(u.prov.input)
	ui.Row(c).Gap(6).AlignItems(ui.Baseline).Children(func() {
		if text == "" {
			return // the placeholder says what goes in
		}
		kind, err := u.provDetect(text)
		if err != nil {
			ui.Text(c, err.Error()).FontSize(12).TextColor(p.warn)
			return
		}
		ui.Text(c, i18n.T("将添加为", "Will add as")).FontSize(12).TextColor(p.mute)
		ui.Text(c, provKindWord(kind)).FontSize(12).FontWeight(700).TextColor(p.ink)
		ui.Text(c, "· "+provDetectDetail(kind, text)).FontSize(12).TextColor(p.mute).MaxLines(1).Shrink(1)
	})
}

func provDetectDetail(kind, text string) string {
	switch kind {
	case "subscription":
		host := text
		if _, rest, ok := strings.Cut(host, "://"); ok {
			host = rest
		}
		host, _, _ = strings.Cut(host, "/")
		return fmt.Sprintf(i18n.T("下载 %s 的节点，之后按设定自动更新", "Downloads the nodes from %s, then updates them on schedule"), host)
	case "link":
		n, schemes := 0, []string{}
		for _, l := range strings.Split(text, "\n") {
			l = strings.TrimSpace(l)
			s, _, ok := strings.Cut(l, "://")
			if !ok || l == "" {
				continue
			}
			n++
			s = strings.ToLower(s)
			if !slices.Contains(schemes, s) {
				schemes = append(schemes, s)
			}
		}
		return nodesCount(n) + fmt.Sprintf(i18n.T("（%s）", " (%s)"), strings.Join(schemes, " · "))
	case "file":
		return i18n.T("Clash / mihomo 配置里的 proxies", "The proxies in a Clash / mihomo config")
	}
	return ""
}

func (u *UI) provAdd(input string) { u.provSubmit(input, u.toast) }

// provSubmit adds what was pasted; fail hears what went wrong.
func (u *UI) provSubmit(input string, fail func(string)) {
	input = strings.TrimSpace(input)
	if input == "" || u.app == nil || u.pending("add") {
		return
	}
	go func() {
		pv, err := u.app.addProvider(bg(), input)
		if err != nil {
			fail(err.Error())
			return
		}
		if u.win != nil {
			u.win.Update(func() {
				if strings.TrimSpace(u.prov.input) == input {
					u.prov.input = ""
				}
				u.prov.open = pv.ID
			})
		}
	}()
}

// ---- Rows ----

func (u *UI) provRow(c *ui.Context, p *pal, pv ProviderView) {
	pr := &u.prov
	open := pr.open == pv.ID
	busy := u.pending("provider:"+pv.ID) || u.pending("providers") && pv.Kind == "subscription" && pv.Enabled
	var nodes []NodeView
	for _, n := range u.snap.Nodes {
		if n.Provider == pv.ID {
			nodes = append(nodes, n)
		}
	}
	ui.Column(c.Key(pv.ID)).Transition(provRowMotion).Children(func() {
		row := ui.ButtonBase(c.Key("row")).Row().Gap(18).Padding(14, 8).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(p.hair2).Transition(ui.ElementTransition{Colors: true, Duration: 160 * time.Millisecond})
		switch {
		case open:
			row.Background(p.paper2).BorderColor(ui.Color{})
		case row.Hovered():
			row.Background(p.hover)
		}
		if !pv.Enabled {
			row.Opacity(.45)
		}
		row.OnClick(func() {
			if pr.open == pv.ID {
				pr.open = ""
			} else {
				pr.open = pv.ID
			}
		}).Children(func() {
			u.provName(c, p, pv, busy, len(nodes))
			provUsage(c, p, pv)
			provHealth(c, p, nodes)
			u.provAuto(c, p, pv)
			u.provOps(c, p, pv, busy, open)
		})
		if open {
			ui.Column(c.Key("nodes")).Background(p.paper2).Padding(0, 18, 12, 18).BorderWidth(0, 0, 1, 0).BorderColor(p.hair).
				Transition(provRowMotion).Children(func() { u.provNodes(c, p, pv, nodes) })
		}
	})
}

func (u *UI) provName(c *ui.Context, p *pal, pv ProviderView, busy bool, shown int) {
	ui.Column(c).Gap(3).Grow(1).Basis(0).MinWidth(0).Children(func() {
		shimmer(disp(ui.Text(c, pv.Name), p).FontSize(18).FontWeight(700).TextColor(p.ink).MaxLines(1), busy, p)
		src := provKindWord(pv.Kind)
		if pv.Source != "" {
			src += " · " + pv.Source
		}
		ui.Text(c, src).FontSize(11.5).TextColor(p.mute).MaxLines(1)
		var meta string
		switch {
		case busy:
			meta = i18n.T("正在更新…", "Updating…")
		default:
			count := max(pv.Nodes, shown)
			meta = nodesCount(count)
			if pv.Kind == "subscription" || !pv.UpdatedAt.IsZero() {
				meta += i18n.T(" · 更新于 ", " · updated ") + ago(pv.UpdatedAt)
			}
			if !pv.Enabled {
				meta += i18n.T(" · 已停用", " · disabled")
			}
		}
		shimmer(ui.Text(c, meta).FontSize(11.5).TextColor(p.mute).MaxLines(1), busy, p)
		if pv.Error != "" {
			ui.Text(c, pv.Error).FontSize(11.5).TextColor(p.signal).MaxLines(2)
		}
		u.provRouting(c, p, pv)
	})
}

// provUsage is the quota: used of total, a bar, and the expiry.
func provUsage(c *ui.Context, p *pal, pv ProviderView) {
	ui.Column(c).Width(172).Gap(5).Children(func() {
		us := pv.Usage
		if us == nil || us.Total <= 0 && us.Expire <= 0 {
			ui.Text(c, i18n.T("不限流量 · 不过期", "Unlimited · never expires")).FontSize(11).TextColor(p.mute)
			return
		}
		col, strong := p.mute, p.ink
		if pv.Warn == "quota" || pv.Warn == "expire" {
			col, strong = p.warn, p.warn
		}
		used := us.Upload + us.Download
		if us.Total > 0 {
			ui.Row(c).Gap(4).AlignItems(ui.Baseline).Children(func() {
				ui.Text(c, i18n.T("已用", "Used")).FontSize(11).TextColor(col)
				ui.Text(c, fmtBytes(used)).FontSize(11).FontWeight(700).TextColor(strong)
				ui.Text(c, "/ "+fmtBytes(us.Total)).FontSize(11).TextColor(col)
			})
			frac := min(1, float32(used)/float32(us.Total))
			bar := p.ink
			if pv.Warn == "quota" {
				bar = p.warn
			}
			ui.Box(c).Height(3).Radius(p.r(2)).Background(p.hair).Children(func() {
				ui.Box(c).WidthPercent(frac * 100).FillHeight().Radius(p.r(2)).Background(bar)
			})
		} else {
			ui.Text(c, i18n.T("不限流量", "Unlimited")).FontSize(11).TextColor(col)
		}
		exp := i18n.T("不过期", "Never expires")
		ecol := p.mute
		if us.Expire > 0 {
			exp = provExpiry(time.Unix(us.Expire, 0))
			if pv.Warn == "expire" {
				ecol = p.warn
			}
		}
		ui.Text(c, exp).FontSize(11).TextColor(ecol)
	})
}

func provExpiry(t time.Time) string {
	d := time.Until(t)
	switch {
	case d <= 0:
		return i18n.T("已过期", "Expired")
	case d < 24*time.Hour:
		return i18n.T("今天到期", "Expires today")
	case d < 60*24*time.Hour:
		return fmt.Sprintf(i18n.T("%d 天后到期", "Expires in %d days"), int((d+12*time.Hour).Hours()/24))
	}
	return fmt.Sprintf(i18n.T("%s 到期", "Expires %s"), t.Format("2006-01-02"))
}

// provHealth is one bar per node, fastest first (taller is faster, a
// failed node a full bar in the signal color), over how many answered.
// Untested nodes get no bars: a row of stubs says nothing.
func provHealth(c *ui.Context, p *pal, nodes []NodeView) {
	var ns []NodeView
	ok := 0
	for _, n := range nodes {
		if n.Tested {
			ns = append(ns, n)
			if n.Delay > 0 {
				ok++
			}
		}
	}
	rank := func(n NodeView) int {
		if n.Delay <= 0 {
			return 1 << 19
		}
		return n.Delay
	}
	slices.SortStableFunc(ns, func(a, b NodeView) int { return rank(a) - rank(b) })
	const most = 40
	tested := len(ns)
	if len(ns) > most {
		ns = ns[:most]
	}
	ui.Column(c).Width(124).Gap(5).Tooltip(i18n.T("各节点延迟，从快到慢", "Each node's delay, fastest first")).Children(func() {
		switch {
		case len(nodes) == 0:
			ui.Text(c, "—").FontSize(11).TextColor(p.faint)
			return
		case tested == 0:
			ui.Text(c, i18n.T("未测速", "Not tested")).FontSize(11).TextColor(p.faint)
			return
		}
		ui.Row(c).Height(16).Gap(2).AlignItems(ui.End).Children(func() {
			for i, n := range ns {
				h := float32(16)
				if n.Delay > 0 {
					h = max(3, 16-float32(n.Delay)/15)
				}
				b := ui.Box(c.Key(i)).Height(h).Radius(p.r(1)).Background(delayColor(p, n.Delay, true).Alpha(.8))
				if len(ns) <= 16 {
					b.Width(5)
				} else {
					b.Grow(1).Basis(0)
				}
			}
		})
		col := p.mute
		if ok == 0 {
			col = p.signal
		}
		ui.Text(c, fmt.Sprintf(i18n.T("%d/%d 可用", "%d/%d up"), ok, tested)).FontSize(11).TextColor(col).NoWrap()
	})
}

// provAutoHours are the auto-update intervals offered, in hours; 0 is off.
var provAutoHours = []int{0, 6, 12, 24}

func provAutoWord(h int) string {
	if h <= 0 {
		return i18n.T("关闭", "Off")
	}
	return fmt.Sprintf(i18n.T("每 %d 小时", "Every %d h"), h)
}

// provAuto is the auto-update interval of a subscription.
func (u *UI) provAuto(c *ui.Context, p *pal, pv ProviderView) {
	ui.Row(c).Width(176).Justify(ui.End).Children(func() {
		if pv.Kind != "subscription" {
			return
		}
		cur := provAutoWord(pv.AutoUpdate)
		hours := provAutoHours
		if !slices.Contains(hours, max(0, pv.AutoUpdate)) {
			hours = append(slices.Clone(hours), pv.AutoUpdate)
		}
		opts := make([]string, len(hours))
		for i, h := range hours {
			opts[i] = provAutoWord(h)
		}
		sel := ui.SelectBase(c.Key("auto"), &cur)
		sel.Trigger.Row().Gap(5).Padding(5, 9).Radius(p.r(7)).Border(1, p.hair2).AlignItems(ui.Center).Label(i18n.T("自动更新", "Auto update")).Children(func() {
			ui.Text(c, i18n.T("自动更新", "Auto-update")).FontSize(11.5).TextColor(p.mute).NoWrap()
			ui.Text(c, cur).FontSize(12).FontWeight(600).TextColor(p.ink).NoWrap()
			ui.Text(c, "▾").FontSize(9).TextColor(p.mute)
		})
		if sel.Open() {
			u.prov.popup = true
		}
		if sel.Trigger.Hovered() || sel.Open() {
			sel.Trigger.BorderColor(p.ink)
		}
		sel.Popup(func(panel ui.Element) {
			panel.Margin(4, 0, 0, 0).Padding(4).Radius(p.r(9)).Background(p.sheet).Border(1, p.hair).
				Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, .14)).MinWidth(130)
			for _, o := range opts {
				it := sel.Item(o).Padding(5, 10).Radius(p.r(6))
				if it.Highlighted() {
					it.Background(p.hair2)
				}
				it.Children(func() {
					w := ui.Text(c, o).FontSize(12).TextColor(p.ink)
					if o == provAutoWord(pv.AutoUpdate) {
						w.FontWeight(700)
					}
				})
			}
		})
		if sel.Trigger.Changed() {
			h := 0
			if i := slices.Index(opts, cur); i >= 0 {
				h = hours[i]
			}
			id := pv.ID
			if u.app != nil {
				u.do(func() error { return u.app.setAutoUpdate(id, h) })
			}
		}
	})
}

// provOps is the row's one action — 更新 a subscription, 启用 a
// disabled source — and the chevron. 停用 and 删除 sit in the open row.
func (u *UI) provOps(c *ui.Context, p *pal, pv ProviderView, busy, open bool) {
	id := pv.ID
	ui.Row(c).Width(86).Gap(10).Justify(ui.End).AlignItems(ui.Center).Children(func() {
		switch {
		case !pv.Enabled:
			u.provBtn(c.Key("toggle"), p, i18n.T("启用", "Enable"), false).OnClick(func() {
				if u.app != nil {
					u.do(func() error { return u.app.setProviderEnabled(id, true) })
				}
			})
		case pv.Kind == "subscription":
			u.provBtn(c.Key("update"), p, i18n.T("更新", "Update"), false).OnClick(func() {
				if u.app != nil && !busy {
					u.do(func() error { return u.app.updateProvider(bg(), id) })
				}
			})
		}
		chev := "▾"
		if open {
			chev = "▴"
		}
		ui.Text(c, chev).FontSize(11).TextColor(p.mute).Width(10)
	})
}

// provManage is 停用 (or 启用) and 删除, which asks to be pressed again.
func (u *UI) provManage(c *ui.Context, p *pal, pv ProviderView) {
	pr := &u.prov
	id := pv.ID
	toggle := i18n.T("停用", "Disable")
	if !pv.Enabled {
		toggle = i18n.T("启用", "Enable")
	}
	on := !pv.Enabled
	u.provBtn(c.Key("toggle-btn"), p, toggle, false).OnClick(func() {
		if pv.Routing.InUse {
			u.toast(i18n.T("这份配置的路由已停用，现在用的是 bowel 路由。重新启用后恢复", "This config's routing has stopped and bowel's is in use. It comes back when you enable the Provider"))
		}
		if u.app != nil {
			u.do(func() error { return u.app.setProviderEnabled(id, on) })
		}
	})
	asking := pr.confirm == id
	del := i18n.T("删除", "Delete")
	if asking {
		del = i18n.T("确认删除", "Confirm delete")
	}
	u.provBtn(c.Key("delete"), p, del, asking).OnClick(func() {
		if !asking {
			pr.confirm, pr.confAt = id, c.Now()
			return
		}
		pr.confirm = ""
		if pr.open == id {
			pr.open = ""
		}
		if pv.Routing.InUse {
			u.toast(i18n.T("已改回 bowel 路由", "Back on bowel's routing"))
		}
		if u.app != nil {
			u.do(func() error { return u.app.deleteProvider(id) })
		}
	})
}

// provBtn is btn, or a danger one asking to be pressed again.
func (u *UI) provBtn(c *ui.Context, p *pal, label string, danger bool) ui.Element {
	b := ui.ButtonBase(c).Padding(5, 11).Radius(p.r(7)).Border(1, p.hair).Label(label)
	col := p.ink
	switch {
	case danger:
		b.Background(p.signal).BorderColor(p.signal)
		col = ui.Hex("#ffffff")
	case b.Hovered():
		b.BorderColor(p.ink)
	}
	return b.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(col).NoWrap() })
}

// ---- A provider's nodes ----

func (u *UI) provNodes(c *ui.Context, p *pal, pv ProviderView, nodes []NodeView) {
	pr := &u.prov
	cols := func(cells ...func()) {
		widths := []float32{22, 0, 0, 0, 100, 0, 64, 64}
		grow := []float32{0, 1.2, .9, 1.1, 0, 1, 0, 0}
		for i, cell := range cells {
			e := ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0)
			if widths[i] > 0 {
				e.Width(widths[i])
			} else {
				e.Grow(grow[i]).Basis(0)
			}
			if i >= len(cells)-2 {
				e.Justify(ui.End)
			}
			e.Children(cell)
		}
	}
	head := func(s string) func() {
		return func() { ui.Text(c, s).FontSize(10.5).FontWeight(600).LetterSpacing(.8).TextColor(p.mute) }
	}
	ui.Row(c).Gap(12).Padding(9, 0, 7, 0).BorderWidth(0, 0, 1, 0).BorderColor(p.hair2).Children(func() {
		cols(func() {}, head(i18n.T("节点", "Node")), head(i18n.T("出口位置", "Exit location")), head(i18n.T("地址", "Address")), head(i18n.T("协议", "Protocol")), head(i18n.T("被哪些路由使用", "Used by routes")), head("Ping"), head("URL"))
	})
	if len(nodes) == 0 {
		msg := i18n.T("这个来源还没有节点。", "This source has no nodes yet.")
		if !pv.Enabled {
			msg = i18n.T("已停用：启用后它的节点会出现在这里。", "Disabled: enable it and its nodes show up here.")
		} else if pv.Error != "" {
			msg = i18n.T("没有拿到节点：", "Couldn't get nodes: ") + pv.Error
		}
		ui.Text(c, msg).FontSize(12).TextColor(p.mute).Padding(10, 0)
	}
	for _, n := range nodes {
		ui.Row(c.Key(n.Name)).Gap(12).Padding(7, 0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.hair2).Children(func() {
			cols(
				func() { flagBadge(c, p, nodeFlag(n), 11) },
				func() { ui.Text(c, n.Display).FontSize(12.5).FontWeight(700).TextColor(p.ink).MaxLines(1) },
				func() {
					ui.Text(c, placeName(n)).FontSize(12).TextColor(p.ink2).MaxLines(1).Shrink(1)
					locTag(c, p, n.Where.Source)
				},
				func() {
					// The server as the subscription gave it, so a node can be
					// told apart by where it lands.
					addr := n.Address
					if addr == "" {
						addr = "—"
					}
					ui.Text(c, addr).Font("SF Mono, Menlo").FontSize(11.5).TextColor(p.mute).MaxLines(1).Shrink(1)
				},
				func() { ui.Text(c, n.Type).FontSize(12).TextColor(p.mute).MaxLines(1) },
				func() {
					used := false
					for _, r := range u.snap.Routes {
						if r.Node == "" || r.Node != n.Name {
							continue
						}
						used = true
						ui.Row(c.Key(r.ID)).Gap(5).AlignItems(ui.Center).Children(func() {
							swatch(c, p.routeColor(r.ID), 8)
							ui.Text(c, r.Name).FontSize(12).TextColor(p.ink).NoWrap()
						})
					}
					if !used {
						ui.Text(c, "—").FontSize(12).TextColor(p.faint)
					}
				},
				func() {
					ui.Text(c, fmtMs(n.Ping, n.Ping > 0)).FontSize(12).FontWeight(600).TextColor(delayColor(p, n.Ping, n.Ping > 0)).NoWrap()
				},
				func() {
					ui.Text(c, fmtDelay(n)).FontSize(12).FontWeight(600).TextColor(delayColor(p, n.Delay, n.Tested)).NoWrap()
				},
			)
		})
	}
	if more := pv.Nodes - len(nodes); more > 0 && len(nodes) > 0 {
		ui.Row(c).Gap(12).Padding(7, 0).BorderWidth(0, 0, 1, 0).BorderColor(p.hair2).Children(func() {
			ui.Box(c).Width(22)
			ui.Text(c, fmt.Sprintf(i18n.T("还有 %d 个节点…", "%d more nodes…"), more)).FontSize(12).TextColor(p.mute)
		})
	}
	// The source, and the name.
	ui.Row(c).Gap(8).Padding(10, 0, 0, 0).AlignItems(ui.Center).Children(func() {
		ui.Text(c, i18n.T("来源", "Source")).FontSize(12).TextColor(p.mute)
		src := pv.Source
		if src == "" {
			src = map[string]string{"link": i18n.T("粘贴的节点链接", "Pasted node links"), "file": i18n.T("本地文件", "Local file")}[pv.Kind]
		}
		ui.Text(c, src).Font("SF Mono, Menlo").FontSize(11.5).TextColor(p.ink2).MaxLines(1).Grow(1).Basis(0).MinWidth(0)
		if pr.renaming == pv.ID {
			in := ui.TextInput(c.Key("rename"), &pr.newName).Width(220).Padding(4, 9).Radius(p.r(7)).Background(p.card).
				Border(1, p.ink).FontSize(12.5).Label(i18n.T("名称", "Name")).AutoFocus()
			// Enter keeps the name, as moving the focus away does; Escape
			// (the page's) goes back.
			if in.Submitted() || in.FocusLeft() && pr.renaming == pv.ID {
				u.provRename(pv.ID, pr.newName)
			}
			ui.Text(c, i18n.T("回车保存 · Esc 取消", "Enter to save · Esc to cancel")).FontSize(11).TextColor(p.mute).NoWrap()
			return
		}
		if pv.Kind == "subscription" || pv.Kind == "link" {
			u.provBtn(c.Key("copy-btn"), p, provCopyWord(pv.Kind), false).OnClick(func() {
				text, err := u.provShareText(pv.ID)
				if err != nil {
					u.toasts = append(u.toasts, err.Error())
					return
				}
				c.WriteClipboard(text)
				u.toasts = append(u.toasts, i18n.T("已复制", "Copied ")+provCopyWhat(pv.Kind))
			})
			u.provBtn(c.Key("qr-btn"), p, i18n.T("二维码", "QR code"), false).OnClick(func() { u.provShareOpen(pv.ID) })
		}
		u.provBtn(c.Key("rename-btn"), p, i18n.T("重命名", "Rename"), false).OnClick(func() {
			pr.renaming, pr.newName = pv.ID, pv.Name
		})
		ui.Box(c).Width(1).Height(18).Margin(0, 4).Background(p.hair)
		u.provManage(c, p, pv)
	})
}

func (u *UI) provRename(id, name string) {
	u.prov.renaming = ""
	name = strings.TrimSpace(name)
	if name == "" || name == u.providerName(id) || u.app == nil {
		return
	}
	u.do(func() error { return u.app.renameProvider(id, name) })
}

// ---- Empty ----

func (u *UI) provEmpty(c *ui.Context, p *pal) {
	ui.Column(c).Gap(6).Padding(30, 0, 0, 0).MaxWidth(720).Children(func() {
		disp(ui.Text(c, i18n.T("还没有节点来源", "No node sources yet")), p).FontSize(26).FontWeight(700).TextColor(p.ink)
		ui.Text(c, i18n.T("粘贴到上面，回车添加。", "Paste one above and press Enter to add it.")).FontSize(13).TextColor(p.mute)
		ui.Column(c).Margin(18, 0, 0, 0).Children(func() {
			for _, k := range [][3]string{
				{i18n.T("订阅", "Subscription"), i18n.T("机场的订阅链接，自动更新", "A service's subscription link, updated automatically"), "https://sub.example.com/api/v1/client/subscribe?token=…"},
				{i18n.T("节点链接", "Node links"), i18n.T("一行一个", "One per line"), i18n.T("trojan://password@hk.example.com:443#香港 01", "trojan://password@hk.example.com:443#Hong Kong 01")},
				{i18n.T("本地文件", "Local file"), i18n.T("Clash 配置，点「选择文件…」", "A Clash config; click “Choose file…”"), "~/Downloads/clash.yaml"},
			} {
				ui.Row(c).Gap(18).Padding(14, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.hair2).AlignItems(ui.Start).Children(func() {
					disp(ui.Text(c, k[0]), p).FontSize(18).FontWeight(700).TextColor(p.ink).Width(110)
					ui.Column(c).Gap(6).Grow(1).Basis(0).Children(func() {
						ui.Text(c, k[1]).FontSize(12.5).TextColor(p.ink2)
						ui.Text(c, k[2]).Font("SF Mono, Menlo").FontSize(11.5).TextColor(p.mute)
					})
				})
			}
		})
	})
}

// nodesCount is "n 个节点", or "n node(s)" in English.
func nodesCount(n int) string {
	if !i18n.En() {
		return fmt.Sprintf(i18n.T("%d 个节点", "%d nodes"), n)
	}
	if n == 1 {
		return "1 node"
	}
	return fmt.Sprintf("%d nodes", n)
}
