package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/width"

	"github.com/oboo/moho/internal/i18n"
)

// Colors, as SGR codes.
const (
	red    = "31"
	green  = "32"
	yellow = "33"
	gray   = "90"
	bold   = "1"
)

// paint is s in a color, or s as it is when colors are off.
func (c *Ctx) paint(code, s string) string {
	if !c.color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// dispWidth is how many columns s takes in a terminal: Chinese, Japanese
// and most emoji take two, a flag's two letters one each.
func dispWidth(s string) int {
	n := 0
	for _, r := range s {
		switch {
		case r == 0x200d || r == 0xfe0f || unicode.Is(unicode.Mn, r):
		case r >= 0x1f1e6 && r <= 0x1f1ff: // regional indicators: a flag is two
			n++
		case r >= 0x1f300 && r <= 0x1faff:
			n += 2
		default:
			switch width.LookupRune(r).Kind() {
			case width.EastAsianWide, width.EastAsianFullwidth:
				n += 2
			default:
				n++
			}
		}
	}
	return n
}

func pad(s string, w int) string {
	if d := w - dispWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// cell is a table's cell: its text, and the color it is shown in.
type cell struct{ text, color string }

func t(s string) cell { return cell{text: s} }

// table prints rows under heads, each column as wide as its widest cell.
func (c *Ctx) table(heads []string, rows [][]cell) {
	widths := make([]int, len(heads))
	for i, h := range heads {
		widths[i] = dispWidth(h)
	}
	for _, r := range rows {
		for i, cl := range r {
			widths[i] = max(widths[i], dispWidth(cl.text))
		}
	}
	line := func(cells []cell) {
		var b strings.Builder
		for i, cl := range cells {
			text := cl.text
			if i < len(cells)-1 {
				text = pad(text, widths[i])
			}
			b.WriteString(c.paint(cl.color, text))
			if i < len(cells)-1 {
				b.WriteString("  ")
			}
		}
		fmt.Fprintln(c.out, strings.TrimRight(b.String(), " "))
	}
	hs := make([]cell, len(heads))
	for i, h := range heads {
		hs[i] = cell{h, gray}
	}
	line(hs)
	for _, r := range rows {
		line(r)
	}
}

// printJSON prints raw indented; compact, on one line, for a stream.
func (c *Ctx) printJSON(raw json.RawMessage, compact bool) {
	var b bytes.Buffer
	if compact {
		if json.Compact(&b, raw) != nil {
			b.Write(raw)
		}
	} else if json.Indent(&b, raw, "", "  ") != nil {
		b.Write(raw)
	}
	fmt.Fprintln(c.out, b.String())
}

// done reports a change that went through: in words, or with --json as
// the tool's answer with "ok": true.
func (c *Ctx) done(raw json.RawMessage, msg string) {
	if !c.json {
		fmt.Fprintln(c.out, c.paint(green, "✓ ")+msg)
		return
	}
	out := map[string]any{}
	if len(raw) > 0 && raw[0] == '{' {
		_ = json.Unmarshal(raw, &out)
	} else if len(raw) > 0 {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			out["message"] = s
		}
	}
	out["ok"] = true
	b, _ := json.Marshal(out)
	c.printJSON(b, false)
}

// warn prints a line the user should see, beside the answer.
func (c *Ctx) warn(msg string) {
	fmt.Fprintln(c.errw, c.paint(yellow, "! ")+msg)
}

// spin shows that something slow is under way, on a terminal only, and
// returns what takes it away.
func (c *Ctx) spin(msg string) (stop func()) {
	if !c.env.TTY || c.json {
		return func() {}
	}
	done, gone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(gone)
		frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
		tick := time.NewTicker(80 * time.Millisecond)
		defer tick.Stop()
		for i := 0; ; i++ {
			fmt.Fprintf(c.errw, "\r%s %s", c.paint(gray, string(frames[i%len(frames)])), msg)
			select {
			case <-done:
				fmt.Fprint(c.errw, "\r\x1b[2K")
				return
			case <-tick.C:
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done); <-gone }) }
}

func fmtBytes(b int64) string {
	v := float64(b)
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1024
		i++
	}
	switch {
	case i == 0:
		return fmt.Sprintf("%d B", b)
	case v < 10:
		return fmt.Sprintf("%.2f %s", v, units[i])
	case v < 100:
		return fmt.Sprintf("%.1f %s", v, units[i])
	}
	return fmt.Sprintf("%.0f %s", v, units[i])
}

// delayCell is a node's delay: green when fast, yellow, red when slow or
// failed, and a dash when it was never tested.
func delayCell(ms *int) cell {
	switch {
	case ms == nil:
		return cell{"—", gray}
	case *ms == 0:
		return cell{i18n.T("超时", "timeout"), red}
	case *ms < 200:
		return cell{fmt.Sprintf("%d ms", *ms), green}
	case *ms < 500:
		return cell{fmt.Sprintf("%d ms", *ms), yellow}
	}
	return cell{fmt.Sprintf("%d ms", *ms), red}
}

// stateCell is the one light: its dot's color and its word.
func stateCell(state string) (dot, word, color string) {
	switch state {
	case "ok":
		return "●", i18n.T("正常", "OK"), green
	case "degraded":
		return "●", i18n.T("降级", "Degraded"), yellow
	case "error":
		return "●", i18n.T("异常", "Error"), red
	}
	return "○", i18n.T("未接管", "Not taking over"), gray
}

func onOff(on bool) string {
	if on {
		return i18n.T("开", "on")
	}
	return i18n.T("关", "off")
}

// entryCell is one way in (the system proxy, TUN, the gateway) as it is.
func entryCell(status, detail string) cell {
	switch status {
	case "on":
		return cell{i18n.T("开", "on"), green}
	case "off":
		return cell{i18n.T("关", "off"), gray}
	case "paused":
		return cell{i18n.T("已暂停", "paused"), gray}
	case "checking":
		return cell{i18n.T("检查中", "checking"), gray}
	case "hijacked":
		return cell{i18n.T("被其他程序改掉了", "changed by another program"), yellow}
	case "install":
		return cell{i18n.T("需要授权", "needs authorization"), yellow}
	}
	text := i18n.T("出错", "error")
	if detail != "" {
		text += " (" + detail + ")"
	}
	return cell{text, red}
}

func modeName(m string) string {
	switch m {
	case "rule":
		return i18n.T("规则", "Rule")
	case "global":
		return i18n.T("全局", "Global")
	case "direct":
		return i18n.T("直连", "Direct")
	}
	return m
}

// modeAbout says what a mode does, in a few words.
func modeAbout(m string) string {
	switch m {
	case "rule":
		return i18n.T("按规则把流量分到不同路由", "Sends traffic to routes by rule")
	case "global":
		return i18n.T("所有流量都走节点", "All traffic goes through a node")
	case "direct":
		return i18n.T("所有流量直接连接", "All traffic connects directly")
	}
	return ""
}

func clock(tm time.Time) string { return tm.Local().Format("15:04:05") }

func date(tm time.Time) string {
	if tm.IsZero() {
		return "—"
	}
	return tm.Local().Format("2006-01-02 15:04")
}
