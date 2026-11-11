package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/oboo/moho/internal/i18n"
)

const reverse = "7"

// visWidth is s's width on the screen, its colors not counted.
func visWidth(s string) int { return dispWidth(stripANSI(s)) }

func stripANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			for i += 2; i < len(s) && (s[i] < 0x40 || s[i] > 0x7e); i++ {
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// clip cuts s to w columns, its colors kept and ended.
func clip(s string, w int) string {
	if visWidth(s) <= w {
		return s
	}
	var b strings.Builder
	n, colored := 0, false
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			j = min(j+1, len(s))
			b.WriteString(s[i:j])
			colored = true
			i = j
			continue
		}
		j := i + 1
		for j < len(s) && s[j]&0xc0 == 0x80 {
			j++
		}
		cw := dispWidth(s[i:j])
		if n+cw > w-1 {
			break
		}
		b.WriteString(s[i:j])
		n += cw
		i = j
	}
	b.WriteString("…")
	if colored {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// rows lays a table out in h lines of w columns: its heads, then the rows
// from off on, the one at sel shown as chosen. It returns the lines and
// the off that keeps sel in sight.
func (t *tui) rows(heads []string, rows [][]cell, sel, off, h int) ([]string, int) {
	widths := make([]int, len(heads))
	for i, hd := range heads {
		widths[i] = dispWidth(hd)
	}
	for _, r := range rows {
		for i, cl := range r {
			widths[i] = max(widths[i], dispWidth(cl.text))
		}
	}
	body := max(h-1, 1)
	off = max(min(off, sel), sel-body+1, 0)
	hs := make([]cell, len(heads))
	for i, hd := range heads {
		hs[i] = cell{hd, gray}
	}
	out := []string{t.rowLine(hs, widths, false)}
	for i := off; i < len(rows) && len(out) < h; i++ {
		out = append(out, t.rowLine(rows[i], widths, i == sel && sel >= 0))
	}
	return out, off
}

// The looks of the screen's parts. One thing is the theme's color: where
// the user is (the tab in front, the row in front). Keys are quiet chips.
const (
	chosenGround = "48;5;237" // the row in front: a dark band, its colors kept
	chipStyle    = "1;97;48;5;240"
)

// rowLine is one line of a table. The row in front has a bar of the
// theme's color at its left and a band behind it; without colors, a >.
func (t *tui) rowLine(cells []cell, widths []int, chosen bool) string {
	band := chosen && t.c.color
	on := func(color, text string) string {
		if !band {
			return t.c.paint(color, text)
		}
		switch color {
		case "":
			color = "1;97" // plain text stands out
		case gray:
			color = "37" // gray would sink into the band
		}
		return t.c.paint(color+";"+chosenGround, text)
	}
	var b strings.Builder
	switch {
	case band:
		bar := t.accent(false)
		if bar == "" {
			bar = "97"
		}
		b.WriteString(t.c.paint(bar+";"+chosenGround, "▌"))
	case chosen:
		b.WriteString(">")
	default:
		b.WriteString(" ")
	}
	for i, cl := range cells {
		text := cl.text
		if i < len(cells)-1 { // the last column needs no room after it
			text = pad(text, widths[i]) + "  "
		}
		b.WriteString(on(cl.color, text))
	}
	line := clip(b.String(), t.w)
	if band {
		if d := t.w - visWidth(line); d > 0 {
			line += t.c.paint(chosenGround, strings.Repeat(" ", d))
		}
	}
	return line
}

// accent is the theme color the app uses (the accent setting) as a color
// code for text, or for the ground behind it: "" when there is none
// ("ink") or colors are off. The color itself where the terminal has
// them all, else the nearest of the 216 every color terminal has.
func (t *tui) accent(ground bool) string {
	var r, g, b int
	accent := plain(t.set.Settings["accent"])
	if n, _ := fmt.Sscanf(accent, "#%02x%02x%02x", &r, &g, &b); !t.c.color || n != 3 || len(accent) != 7 {
		return ""
	}
	kind := "38"
	if ground {
		kind = "48"
	}
	if ct := t.c.env.Getenv("COLORTERM"); ct == "truecolor" || ct == "24bit" {
		return fmt.Sprintf("%s;2;%d;%d;%d", kind, r, g, b)
	}
	return fmt.Sprintf("%s;5;%d", kind, 16+36*((r*5+127)/255)+6*((g*5+127)/255)+(b*5+127)/255)
}

// brand is the name at the top left, on the theme color: dark letters on
// a light one, so that it reads on a light terminal as on a dark one.
// Without a theme color it is bold.
func (t *tui) brand() string {
	bg := t.accent(true)
	if bg == "" {
		if t.c.color {
			return t.c.paint(bold, "Bowel")
		}
		return "Bowel"
	}
	var r, g, b int
	fmt.Sscanf(plain(t.set.Settings["accent"]), "#%02x%02x%02x", &r, &g, &b)
	fg := "30"
	if (r*299+g*587+b*114)/1000 < 128 {
		fg = "97"
	}
	return t.c.paint("1;"+fg+";"+bg, " Bowel ")
}

var tabNames = []words{{"概览", "Overview"}, {"连接", "Connections"}, {"日志", "Logs"}, {"规则", "Rules"}, {"Provider", "Providers"}, {"设置", "Settings"}}

// view is the whole screen: t.h lines of at most t.w columns.
func (t *tui) view() []string {
	if t.w < 20 || t.h < 8 {
		return []string{i18n.T("窗口太小", "The window is too small")}
	}
	c := t.c
	dot, word, color := stateCell(t.st.State)
	head := " " + t.brand() + "  " + c.paint(color, dot+" "+word) + "  " + modeName(t.st.Mode) +
		fmt.Sprintf("  ↓ %s/s  ↑ %s/s  ", fmtBytes(t.st.Traffic.DownBps), fmtBytes(t.st.Traffic.UpBps)) +
		c.paint(gray, fmt.Sprintf(i18n.T("%d 个连接", "%d connections"), t.st.Active))
	switch {
	case t.err != nil:
		head = " " + t.brand() + "  " + c.paint(red, t.err.Error())
	case t.st.Issue != nil:
		head += "  " + c.paint(color, t.st.Issue.Text)
	}
	tabs := " "
	for i, n := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, n)
		switch {
		case i != t.tab:
			label = c.paint(gray, label)
		case !c.color:
			label = "[" + label + "]"
		case t.accent(false) != "":
			label = c.paint("1;4;"+t.accent(false), label)
		default:
			label = c.paint("1;4", label)
		}
		tabs += label + "   "
	}
	h := t.h - 6 // the page's lines
	var body []string
	switch {
	case t.help:
		body = t.helpLines()
	case t.choose != nil:
		body = t.chooserLines(h)
	case t.pick != nil:
		body = t.pickerLines(h)
	default:
		body = t.pageLines(h)
	}
	out := []string{clip(head, t.w), "", clip(tabs, t.w), c.paint(gray, strings.Repeat("─", t.w))}
	for i := 0; i < h; i++ {
		if i < len(body) {
			out = append(out, clip(body[i], t.w))
		} else {
			out = append(out, "")
		}
	}
	// The last two lines: what is being typed, asked or said, then the keys.
	return append(out, clip(t.footer(), t.w), clip(t.hintBar(), t.w))
}

// footer is the line above the keys: what is being typed or asked, what
// is under way, or what just happened.
func (t *tui) footer() string {
	c := t.c
	switch {
	case t.input != nil:
		return " " + c.paint(bold, t.input.label+": ") + string(t.input.buf) + "█  " + c.paint(gray, i18n.T("回车确认 · Esc 取消", "Enter to confirm · Esc to cancel"))
	case t.confirm != nil:
		return " " + c.paint(yellow, t.confirm.msg) + "  " + c.paint(bold, "Y") + c.paint(gray, " / n")
	case t.busy != "":
		return " " + c.paint(gray, t.busy)
	case t.msg != "" && time.Since(t.msgAt) < 6*time.Second:
		if t.msgErr {
			return " " + c.paint(red, "✗ "+t.msg)
		}
		return " " + c.paint(green, "✓ ") + t.msg
	}
	return ""
}

// hint is a key and what it does, for the bar at the bottom.
type hint struct{ key, does string }

const enterKey = "⏎"

// pickerHints and chooserHints are the keys of the two lists.
func pickerHints() []hint {
	return []hint{{"↑↓", i18n.T("选择", "Move")}, {enterKey, i18n.T("使用这个节点", "Use this node")}, {"a", i18n.T("自动", "Auto")}, {"t", i18n.T("测速", "Test")}, {"Esc", i18n.T("返回", "Back")}}
}

func chooserHints() []hint {
	return []hint{{"↑↓", i18n.T("选择", "Move")}, {enterKey, i18n.T("确定", "Choose")}, {"Esc", i18n.T("返回", "Back")}}
}

// pageHints are a page's own keys: those that can be used now, or with
// all set every one it has, for the help.
func (t *tui) pageHints(tab int, all bool) []hint {
	switch tab {
	case tabHome:
		return []hint{{enterKey, i18n.T("切换开关 / 选节点", "Toggle / choose node")}, {"a", i18n.T("加订阅", "Add subscription")}, {"m", i18n.T("模式", "Mode")}}
	case tabConns:
		own := []hint{{"/", i18n.T("过滤", "Filter")}}
		if all || t.filter != "" {
			own = append(own, hint{"Esc", i18n.T("清除过滤", "Clear the filter")})
		}
		return own
	case tabLogs:
		return []hint{{"v", i18n.T("级别", "Level")}, {"↑↓", i18n.T("回看", "Look back")}, {"G", i18n.T("最新", "Newest")}}
	// Only what can be done now: with nothing listed, there is only adding.
	case tabRules:
		own := []hint{{"a", i18n.T("添加", "Add")}}
		if all || len(t.rules) > 0 {
			own = append(own, hint{"d", i18n.T("删除", "Remove")})
		}
		return own
	case tabProviders:
		own := []hint{{"a", i18n.T("添加", "Add")}}
		if all || len(t.provs) > 0 {
			own = append(own, hint{"u", i18n.T("更新", "Update")}, hint{"U", i18n.T("全部更新", "Update all")}, hint{"e", i18n.T("启用/停用", "On/off")}, hint{"d", i18n.T("删除", "Remove")})
		}
		return own
	case tabSettings:
		return []hint{{enterKey, i18n.T("修改", "Change")}}
	}
	return nil
}

// hints are the keys of what is in front, then the ones that always work.
func (t *tui) hints() []hint {
	switch {
	case t.choose != nil:
		return chooserHints()
	case t.pick != nil:
		return pickerHints()
	}
	return append(t.pageHints(t.tab, false), hint{"?", i18n.T("帮助", "Help")}, hint{"q", i18n.T("退出", "Quit")}) // the tabs show their own numbers
}

// chips are hints on a line: each key as a chip, what it does beside it.
// Without colors the keys stand in brackets.
func (t *tui) chips(hs []hint) string {
	var b strings.Builder
	for _, h := range hs {
		if t.c.color {
			b.WriteString(t.c.paint(chipStyle, " "+h.key+" "))
		} else {
			b.WriteString("[" + h.key + "]")
		}
		b.WriteString(" " + t.c.paint(gray, h.does) + "   ")
	}
	return strings.TrimRight(b.String(), " ")
}

// hintBar is the last line: the keys of what is in front. On a narrow
// terminal whole hints go, the last of the page's first: help and quit
// stay, and ? lists the rest.
func (t *tui) hintBar() string {
	hs := t.hints()
	for visWidth(" "+t.chips(hs)) > t.w && len(hs) > 2 {
		hs = append(hs[:len(hs)-3:len(hs)-3], hs[len(hs)-2:]...)
	}
	return " " + t.chips(hs)
}

func (t *tui) chooserLines(h int) []string {
	ch := t.choose
	out := []string{"  " + t.c.paint(bold, ch.title), ""}
	widths := []int{1, 0}
	for _, l := range ch.labels {
		widths[1] = max(widths[1], dispWidth(l))
	}
	for i, l := range ch.labels {
		mark := t2(" ")
		if i == ch.current {
			mark = cell{"●", green}
		}
		out = append(out, t.rowLine([]cell{mark, t2(pad(l, widths[1]))}, widths, i == ch.sel))
	}
	return out
}

// helpLines lists every key: the ones that work everywhere, then each
// page's, as the bar at the bottom shows them.
func (t *tui) helpLines() []string {
	type section struct {
		name string
		keys []hint
	}
	secs := []section{
		{i18n.T("通用", "Anywhere"), []hint{{"1-6", i18n.T("切换页面", "Go to a page")}, {"Tab", i18n.T("下一页", "Next page")}, {"↑↓", i18n.T("选择", "Move")}, {"PgUp PgDn", i18n.T("翻页", "A page up or down")}}},
		{"", []hint{{"g G", i18n.T("到头尾", "First or last")}, {"r", i18n.T("刷新", "Refresh")}, {"q", i18n.T("退出，bowel 继续运行", "Quit; bowel keeps running")}}},
	}
	for i, n := range tabNames {
		secs = append(secs, section{n.String(), t.pageHints(i, true)})
		if i == tabHome {
			secs = append(secs, section{i18n.T("选节点", "Choosing a node"), pickerHints()[1:]})
		}
	}
	width := 0
	for _, sec := range secs {
		width = max(width, dispWidth(sec.name))
	}
	out := []string{"  " + t.c.paint(bold, i18n.T("按键", "Keys")), ""}
	for i, sec := range secs {
		if i == 2 {
			out = append(out, "")
		}
		out = append(out, "  "+pad(sec.name, width)+"   "+t.chips(sec.keys))
	}
	return append(out, "", "  "+t.c.paint(gray, i18n.T("会改变网络的操作会先问 Y/n，回车即确认。按任意键返回。", "What changes the network asks Y/n first; Enter is yes. Any key goes back.")))
}

func (t *tui) pickerLines(h int) []string {
	p := t.pick
	title := "  " + t.c.paint(bold, fmt.Sprintf(i18n.T("为「%s」选择节点", "Choose a node for %s"), p.route.Name))
	if len(p.nodes) == 0 {
		return []string{title, "", "  " + i18n.T("还没有节点。先在 Provider 页添加一个 Provider。", "No nodes yet. Add a Provider on the Providers page first.")}
	}
	var rows [][]cell
	for _, n := range p.nodes {
		mark := t2(" ")
		if n.Name == p.current {
			mark = cell{"●", green}
		}
		prov := p.providers[n.Provider]
		if prov == "" {
			prov = n.Provider
		}
		rows = append(rows, []cell{mark, t2(n.Label), delayCell(n.DelayMs), t2(n.Type), t2(prov), {n.Where, gray}})
	}
	lines, off := t.rows([]string{" ", i18n.T("节点", "Node"), i18n.T("延迟", "Delay"), i18n.T("类型", "Kind"), "Provider", i18n.T("位置", "Location")}, rows, p.sel, p.off, h-2)
	p.off = off
	return append([]string{title, ""}, lines...)
}

// pageLines are the lines of the page in front.
func (t *tui) pageLines(h int) []string {
	c := t.c
	var out []string
	table := func(heads []string, rows [][]cell, empty string, room int) {
		if len(rows) == 0 {
			out = append(out, "  "+c.paint(gray, empty))
			return
		}
		lines, off := t.rows(heads, rows, t.sel[t.tab], t.off[t.tab], room)
		t.off[t.tab] = off
		out = append(out, lines...)
	}
	switch t.tab {
	case tabHome:
		// The top line has the state, the mode and the rates: here, the rest.
		facts := c.statusFacts(t.st)
		today := fmt.Sprintf("%s ↓ %s ↑ %s", i18n.T("今日", "today"), fmtBytes(t.st.Traffic.TodayDown), fmtBytes(t.st.Traffic.TodayUp))
		out = append(out, "  "+c.paint(gray, facts[0][1]+" · "+today))
		if t.st.Providers == 0 {
			out = append(out, "", "  "+c.paint(yellow, i18n.T("还没有订阅，流量都在直连。按 a 添加订阅地址。", "No subscription yet, so traffic goes direct. Press a to add one.")))
		}
		out = append(out, "")
		sws := t.toggles()
		sel := t.sel[tabHome]
		var rows [][]cell
		for _, sw := range sws {
			box := cell{"○", gray}
			if sw.on {
				box = cell{"●", green}
			}
			rows = append(rows, []cell{box, t2(sw.name), sw.state, {sw.about, gray}})
		}
		swSel := -1
		if sel < len(sws) {
			swSel = sel
		}
		lines, _ := t.rows([]string{" ", i18n.T("开关", "Switch"), i18n.T("状态", "State"), i18n.T("说明", "About")}, rows, swSel, 0, len(rows)+1)
		out = append(out, lines...)
		out = append(out, "")
		rows = nil
		for _, r := range t.st.Routes {
			rules := ""
			if r.UserRules > 0 {
				rules = fmt.Sprint(r.UserRules)
			}
			rows = append(rows, []cell{t2(r.Name), c.nodeText(r), t2(rules), {r.Summary, gray}})
		}
		lines, off := t.rows([]string{i18n.T("路由", "Route"), i18n.T("节点", "Node"), i18n.T("我的规则", "My rules"), i18n.T("说明", "About")}, rows, sel-len(sws), t.off[tabHome], max(h-len(out), 2))
		t.off[tabHome] = off
		out = append(out, lines...)
	case tabConns:
		if t.filter != "" {
			out = append(out, "  "+c.paint(gray, i18n.T("过滤", "Filter")+": ")+t.filter, "")
		}
		var rows [][]cell
		for _, cn := range t.conns {
			cells := connCells(cn)
			if cn.Status != "active" { // what has ended steps back
				for i := range cells[:len(cells)-1] {
					cells[i].color = gray
				}
			}
			rows = append(rows, cells)
		}
		table([]string{i18n.T("时间", "Time"), i18n.T("应用", "App"), i18n.T("目标", "Host"), i18n.T("路由", "Route"), i18n.T("节点", "Node"), i18n.T("下载", "Down"), i18n.T("上传", "Up"), i18n.T("状态", "State")},
			rows, i18n.T("没有连接。", "No connections."), h-len(out))
	case tabLogs:
		level := t.level
		if level == "" {
			level = i18n.T("全部", "all")
		}
		state := i18n.T("跟随最新", "following")
		if t.logUp > 0 {
			state = c.paint(yellow, i18n.T("已暂停滚动，按 G 回到最新", "scrolling paused; G goes to the newest"))
		}
		out = append(out, "  "+c.paint(gray, i18n.T("级别", "Level")+": ")+level+c.paint(gray, " · ")+state, "")
		logs := t.shownLogs(t.logs)
		end := len(logs) - t.logUp
		start := max(end-(h-2), 0)
		for _, l := range logs[start:max(end, start)] {
			color := gray
			switch l.Level {
			case "warning":
				color = yellow
			case "error":
				color = red
			case "info":
				color = green
			}
			out = append(out, " "+c.paint(gray, clock(l.Time))+" "+c.paint(color, pad(l.Level, 7))+" "+l.Text)
		}
		if len(logs) == 0 {
			out = append(out, "  "+c.paint(gray, i18n.T("还没有日志。", "No log lines yet.")))
		}
	case tabRules:
		var rows [][]cell
		for _, r := range t.rules {
			rows = append(rows, []cell{t2(r.Kind), t2(r.Value), t2(r.Route), {r.Matches, gray}})
		}
		room := h
		if t.sets > 0 {
			room -= 2
		}
		table([]string{i18n.T("类型", "Kind"), i18n.T("地址", "Target"), i18n.T("路由", "Route"), i18n.T("匹配", "Matches")},
			rows, i18n.T("还没有自己的规则。按 a 添加。", "No rules of your own yet. Press a to add one."), room)
		if t.sets > 0 {
			out = append(out, "", "  "+c.paint(gray, fmt.Sprintf(i18n.T("另有 %d 个导入的规则集，排在你的规则之后。", "Also %d imported rule sets, after your rules."), t.sets)))
		}
	case tabProviders:
		var rows [][]cell
		for _, p := range t.provs {
			state := cell{i18n.T("启用", "on"), green}
			switch {
			case p.Error != "":
				state = cell{p.Error, red}
			case !p.Enabled:
				state = cell{i18n.T("停用", "off"), gray}
			}
			usage, expire := "", ""
			if u := p.Usage; u != nil {
				if u.Total > 0 {
					usage = fmtBytes(u.Upload+u.Download) + " / " + fmtBytes(u.Total)
				}
				if u.Expire > 0 {
					expire = time.Unix(u.Expire, 0).Local().Format("2006-01-02")
				}
			}
			rows = append(rows, []cell{t2(p.Name), t2(p.Kind), t2(fmt.Sprint(p.Nodes)), t2(usage), t2(expire), t2(date(p.UpdatedAt)), state})
		}
		table([]string{i18n.T("名称", "Name"), i18n.T("类型", "Kind"), i18n.T("节点", "Nodes"), i18n.T("流量", "Traffic"), i18n.T("到期", "Expires"), i18n.T("更新于", "Updated"), i18n.T("状态", "State")},
			rows, i18n.T("还没有 Provider。按 a 添加。", "No Providers yet. Press a to add one."), h)
	case tabSettings:
		var rows [][]cell
		for _, k := range t.setKeys {
			v := plain(t.set.Settings[k])
			if dispWidth(v) > 48 {
				v = clip(v, 48)
			}
			val := t2(v)
			switch v {
			case "true":
				val.color = green
			case "false":
				val.color = gray
			}
			rows = append(rows, []cell{t2(k), val, {strings.Join(t.set.Choices[k], " | "), gray}})
		}
		table([]string{i18n.T("设置", "Setting"), i18n.T("值", "Value"), i18n.T("可选值", "Choices")}, rows, "", h)
	}
	return out
}

// t2 is t, where a *tui is called t.
func t2(s string) cell { return cell{text: s} }
