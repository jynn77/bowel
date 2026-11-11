package cli

// `bowel tui`: the running bowel on a whole terminal. Pages for what the
// commands show (the state and routes, connections, logs, rules,
// providers), kept current, with keys for what the commands change. Like
// them it is a client of the command socket and holds no logic of the
// product: every page is a tool's answer, every key a tool's call.
//
// One goroutine owns the model and the connection; keys, the clock and
// the terminal's size are what it waits on. A change asks first (y/n),
// since a key is easier to hit by mistake than a command is to type.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/oboo/moho/internal/i18n"
)

// The pages, in the order of their tabs.
const (
	tabHome = iota
	tabConns
	tabLogs
	tabRules
	tabProviders
	tabSettings
	numTabs
)

// key is one key press: a name ("up", "enter", "esc", …), or "rune" and
// the character typed.
type key struct {
	name string
	r    rune
}

// parseKeys reads what a terminal sent as keys.
func parseKeys(b []byte) []key {
	names := map[string]string{"A": "up", "B": "down", "C": "right", "D": "left", "Z": "backtab", "H": "home", "F": "end",
		"5~": "pgup", "6~": "pgdn", "1~": "home", "4~": "end", "7~": "home", "8~": "end"}
	var out []key
	for len(b) > 0 {
		switch c := b[0]; {
		case c == 0x1b && len(b) > 2 && (b[1] == '[' || b[1] == 'O'):
			i := 2
			for i < len(b) && (b[i] < 0x40 || b[i] > 0x7e) {
				i++
			}
			if i >= len(b) {
				return out
			}
			if n := names[string(b[2:i+1])]; n != "" {
				out = append(out, key{name: n})
			}
			b = b[i+1:]
			continue
		case c == 0x1b:
			out = append(out, key{name: "esc"})
		case c == '\r' || c == '\n':
			out = append(out, key{name: "enter"})
		case c == '\t':
			out = append(out, key{name: "tab"})
		case c == 0x7f || c == 0x08:
			out = append(out, key{name: "backspace"})
		case c == 0x03:
			out = append(out, key{name: "ctrl-c"})
		case c < 0x20:
		default:
			r, size := utf8.DecodeRune(b)
			out = append(out, key{name: "rune", r: r})
			b = b[size:]
			continue
		}
		b = b[1:]
	}
	return out
}

// prompt is a line being typed at the bottom; ask a question waiting for y.
type prompt struct {
	label  string
	buf    []rune
	submit func(string)
}

type ask struct {
	msg string
	do  func()
}

// picker is the list of nodes to choose one from for a route.
type picker struct {
	route     route
	current   string
	nodes     []node
	providers map[string]string
	sel, off  int
}

// chooser is a short list to take one of: a setting's values, the modes.
type chooser struct {
	title   string
	options []string // what is sent
	labels  []string // what is shown
	current int      // the one in use, -1 for none
	sel     int
	done    func(option string)
}

type tui struct {
	c    *Ctx
	w, h int
	tab  int
	// What the pages show, as last read.
	st      status
	conns   []conn
	total   int
	logs    []logLine
	lastLog int64
	rules   []rule
	sets    int
	provs   []providerView
	set     settingsView
	setKeys []string // the settings the page lists, sorted
	// err is why the last read failed: bowel is gone, most likely.
	err error

	sel, off [numTabs]int
	logUp    int // lines scrolled up from the newest; 0 follows
	filter   string
	level    string
	pick     *picker
	choose   *chooser
	input    *prompt
	confirm  *ask
	help     bool
	busy     string
	msg      string
	msgErr   bool
	msgAt    time.Time
	ticks    int
	quit     bool
	// draw shows the screen now: set while it runs, nil in tests.
	draw func()
}

func runTUI(c *Ctx) error {
	in, out := os.Stdin, os.Stdout
	if !c.env.TTY || c.json || !IsTerminal(in) {
		return &usageError{path: c.path, msg: i18n.T("bowel tui 要在终端里运行；脚本里请用带 --json 的命令", "bowel tui needs a terminal; in scripts, use the commands with --json")}
	}
	t := &tui{c: c}
	if _, err := c.get("get_status", nil, &t.st); err != nil { // before the screen is taken
		return err
	}
	restore, err := termRaw(in, out)
	if err != nil {
		return err
	}
	fmt.Fprint(out, "\x1b[?1049h\x1b[?25l") // the other screen, no cursor
	defer func() {
		fmt.Fprint(out, "\x1b[?25h\x1b[?1049l")
		restore()
	}()
	c.color = c.env.Getenv("NO_COLOR") == "" && !c.has("no-color")
	t.draw = func() {
		t.w, t.h = termSize(out)
		fmt.Fprint(out, "\x1b[H"+strings.Join(t.view(), "\x1b[K\r\n")+"\x1b[K\x1b[J")
	}
	keys := make(chan []byte)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := in.Read(buf)
			if err != nil {
				close(keys)
				return
			}
			keys <- slices.Clone(buf[:n])
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	defer signal.Stop(sig)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	t.poll(true)
	t.draw()
	for !t.quit {
		select {
		case b, ok := <-keys:
			if !ok {
				return nil
			}
			for _, k := range parseKeys(b) {
				t.handle(k)
			}
		case <-tick.C:
			t.poll(false)
		case <-sig:
			return nil
		case <-c.env.Context.Done():
			return nil
		}
		t.draw()
	}
	return nil
}

// read runs a tool for a page. A failure is kept for the top line, and the
// connection is dropped so that the next read dials again.
func (t *tui) read(tool string, args, v any) bool {
	_, err := t.c.get(tool, args, v)
	if err != nil {
		t.err = err
		if t.c.cl != nil {
			t.c.cl.close()
			t.c.cl = nil
		}
		return false
	}
	t.err = nil
	return true
}

// poll reads what the page in front shows; everything, when all is set.
func (t *tui) poll(all bool) {
	t.ticks++
	if !t.read("get_status", nil, &t.st) {
		return
	}
	slow := all || t.ticks%5 == 0
	if p := t.pick; p != nil && (slow || t.ticks%2 == 0) {
		t.loadNodes(false)
	}
	if all || t.tab == tabConns {
		args := map[string]any{"limit": 200}
		if t.filter != "" {
			args["filter"] = t.filter
		}
		var out struct {
			Connections []conn `json:"connections"`
			Total       int    `json:"total"`
		}
		if t.read("list_connections", args, &out) {
			t.conns, t.total = out.Connections, out.Total
		}
	}
	if all || t.tab == tabLogs {
		var out struct {
			Logs []logLine `json:"logs"`
			Last int64     `json:"last"`
		}
		if t.read("tail_logs", map[string]any{"after": t.lastLog, "limit": 500}, &out) {
			if t.logUp > 0 { // scrolled up: what is on screen stays there
				t.logUp += len(t.shownLogs(out.Logs))
			}
			t.logs, t.lastLog = append(t.logs, out.Logs...), out.Last
			if n := len(t.logs); n > 2000 {
				t.logs = t.logs[n-2000:]
			}
		}
	}
	if all || (t.tab == tabRules && slow) {
		var out struct {
			Rules    []rule            `json:"rules"`
			RuleSets []json.RawMessage `json:"ruleSets"`
		}
		if t.read("list_rules", nil, &out) {
			t.rules, t.sets = out.Rules, len(out.RuleSets)
		}
	}
	if all || (t.tab == tabProviders && slow) {
		var out struct {
			Providers []providerView `json:"providers"`
		}
		if t.read("list_providers", nil, &out) {
			t.provs = out.Providers
		}
	}
	if all || (t.tab == tabSettings && slow) {
		t.loadSettings()
	}
	t.clamp()
}

// loadSettings reads the settings the page lists: those that can be
// changed by key, and the policy for agents.
func (t *tui) loadSettings() {
	var out settingsView
	if !t.read("get_settings", nil, &out) {
		return
	}
	keys := slices.Clone(out.Editable)
	if _, ok := out.Settings["mcpPolicy"]; ok {
		keys = append(keys, "mcpPolicy")
	}
	slices.Sort(keys)
	t.set, t.setKeys = out, keys
}

// toggle is one of the overview's switches.
type toggle struct {
	name  string
	on    bool
	state cell
	about string
	tool  string
	args  func(on bool) map[string]any
}

// toggles are the switches of the overview, as they are now.
func (t *tui) toggles() []toggle {
	tk := t.st.Takeover
	// An entry as it is: off when not wanted, waiting while the takeover
	// is off, else what was measured.
	state := func(e entry) cell {
		switch {
		case !e.Wanted:
			return entryCell("off", "")
		case !tk.On:
			return entryCell("paused", "")
		}
		return entryCell(e.Status, e.Detail)
	}
	plain := func(on bool) cell {
		if on {
			return entryCell("on", "")
		}
		return entryCell("off", "")
	}
	lan := plain(tk.Lan)
	if tk.Lan && tk.LanAddr != "" {
		lan.text += " " + tk.LanAddr
	}
	changes := func(key string) func(bool) map[string]any {
		return func(on bool) map[string]any { return map[string]any{"changes": map[string]any{key: on}} }
	}
	return []toggle{
		{i18n.T("接管", "Takeover"), tk.On, plain(tk.On), i18n.T("总开关：关闭后系统代理恢复原样", "The main switch"),
			"set_takeover", func(on bool) map[string]any { return map[string]any{"enabled": on} }},
		{i18n.T("系统代理", "System proxy"), tk.System.Wanted, state(tk.System), i18n.T("遵循系统代理的应用走 bowel", "For apps that follow the system proxy"),
			"set_takeover", func(on bool) map[string]any { return map[string]any{"systemProxy": on} }},
		{"TUN", tk.Tun.Wanted, state(tk.Tun), i18n.T("接管全部流量，包括不理会系统代理的应用；首次需要管理员授权", "All traffic; asks for an administrator once"),
			"set_takeover", func(on bool) map[string]any { return map[string]any{"tun": on} }},
		{i18n.T("网关模式", "Gateway mode"), tk.Gateway.Wanted, state(tk.Gateway), i18n.T("让局域网其他设备经这台电脑上网", "Other LAN devices go out through this computer"),
			"set_gateway", func(on bool) map[string]any { return map[string]any{"enabled": on} }},
		{i18n.T("允许局域网", "Allow LAN"), tk.Lan, lan, i18n.T("局域网里的设备可以使用代理端口", "LAN devices may use the proxy port"),
			"update_settings", changes("allowLan")},
	}
}

// flip asks, then turns a switch the other way.
func (t *tui) flip(sw toggle) {
	q, done := fmt.Sprintf(i18n.T("打开%s？", "Turn %s on?"), sw.name), fmt.Sprintf(i18n.T("%s已打开", "%s is on"), sw.name)
	if sw.on {
		q, done = fmt.Sprintf(i18n.T("关闭%s？", "Turn %s off?"), sw.name), fmt.Sprintf(i18n.T("%s已关闭", "%s is off"), sw.name)
	}
	t.confirm = &ask{q, func() { t.call(i18n.T("正在应用…", "Applying…"), done, sw.tool, sw.args(!sw.on)) }}
}

// addProvider opens the line a provider is typed on.
func (t *tui) addProvider() {
	t.input = &prompt{label: i18n.T("添加 Provider（订阅地址、节点链接或文件）", "Add a Provider (subscription URL, node link or file)"), submit: func(s string) {
		args := map[string]any{"source": s}
		if !strings.Contains(s, "://") {
			if fi, err := os.Stat(s); err == nil && !fi.IsDir() {
				if abs, err := filepath.Abs(s); err == nil {
					args = map[string]any{"file": abs}
				}
			}
		}
		t.call(i18n.T("正在添加 Provider…", "Adding the Provider…"), i18n.T("已添加", "Added"), "provider_add", args)
	}}
}

// editSetting changes the setting in front: a switch is turned, one of a
// few values goes to the next, anything else is typed.
func (t *tui) editSetting() {
	if t.sel[tabSettings] >= len(t.setKeys) {
		return
	}
	key := t.setKeys[t.sel[tabSettings]]
	old := t.set.Settings[key]
	send := func(val string) {
		t.act(i18n.T("正在应用…", "Applying…"), key+" → "+val, func() error {
			_, err := t.c.sendSetting(key, val, old)
			if err == nil && key == "language" { // this screen speaks it too
				i18n.Set(val)
			}
			return err
		})
	}
	// One of a few values: a list to take one from.
	if ch := t.set.Choices[key]; len(ch) > 0 {
		labels := make([]string, len(ch))
		for i, v := range ch {
			switch {
			case key == "language":
				labels[i] = pad(i18n.Names[v], 10) + v
			case v == "":
				labels[i] = i18n.T("（不设置）", "(none)")
			default:
				labels[i] = v
			}
		}
		cur := slices.Index(ch, plain(old))
		if cur < 0 && string(old) == "false" { // an old way to write off
			cur = slices.Index(ch, "off")
		}
		t.choose = &chooser{title: key, options: ch, labels: labels, current: cur, sel: max(cur, 0), done: func(v string) {
			if v == "" {
				v = `""`
			}
			send(v)
		}}
		return
	}
	switch string(old) {
	case "true":
		t.confirm = &ask{fmt.Sprintf(i18n.T("把 %s 改为 %s？", "Change %s to %s?"), key, "false"), func() { send("false") }}
	case "false":
		t.confirm = &ask{fmt.Sprintf(i18n.T("把 %s 改为 %s？", "Change %s to %s?"), key, "true"), func() { send("true") }}
	default:
		t.input = &prompt{label: key, buf: []rune(plain(old)), submit: send}
	}
}

// loadNodes reads the picker's nodes, measuring them first when test is set.
func (t *tui) loadNodes(test bool) error {
	p := t.pick
	var out struct {
		Current   string            `json:"current"`
		Nodes     []node            `json:"nodes"`
		Providers map[string]string `json:"providers"`
		Error     string            `json:"error"`
	}
	if _, err := t.c.get("list_nodes", map[string]any{"route": p.route.ID, "test": test}, &out); err != nil {
		return err
	}
	p.current, p.nodes, p.providers = out.Current, out.Nodes, out.Providers
	p.sel = min(p.sel, max(len(p.nodes)-1, 0))
	if out.Error != "" {
		return errors.New(out.Error)
	}
	return nil
}

// shownLogs are the lines the level filter lets through.
func (t *tui) shownLogs(all []logLine) []logLine {
	if t.level == "" {
		return all
	}
	rank := map[string]int{"debug": 0, "info": 1, "warning": 2, "error": 3}
	var out []logLine
	for _, l := range all {
		if rank[l.Level] >= rank[t.level] {
			out = append(out, l)
		}
	}
	return out
}

// count is how many rows the page in front has to choose among.
func (t *tui) count() int {
	switch t.tab {
	case tabHome:
		return len(t.toggles()) + len(t.st.Routes)
	case tabSettings:
		return len(t.setKeys)
	case tabConns:
		return len(t.conns)
	case tabRules:
		return len(t.rules)
	case tabProviders:
		return len(t.provs)
	}
	return 0
}

func (t *tui) clamp() {
	t.sel[t.tab] = max(min(t.sel[t.tab], t.count()-1), 0)
	t.logUp = max(min(t.logUp, len(t.shownLogs(t.logs))-1), 0)
}

func (t *tui) say(msg string, isErr bool) { t.msg, t.msgErr, t.msgAt = msg, isErr, time.Now() }

// act does a change: it says what is under way, calls the tool, says how
// it went and reads the pages again.
func (t *tui) act(doing, done string, fn func() error) {
	t.busy = doing
	if t.draw != nil {
		t.draw()
	}
	err := fn()
	t.busy = ""
	if err != nil {
		t.say(err.Error(), true)
	} else {
		t.say(done, false)
	}
	t.poll(true)
}

// call is act for one tool whose answer is not needed.
func (t *tui) call(doing, done, tool string, args any) {
	t.act(doing, done, func() error {
		_, err := t.c.call(tool, args)
		return err
	})
}

func (t *tui) move(d int) {
	switch {
	case t.choose != nil:
		t.choose.sel = max(min(t.choose.sel+d, len(t.choose.options)-1), 0)
	case t.pick != nil:
		t.pick.sel = max(min(t.pick.sel+d, len(t.pick.nodes)-1), 0)
	case t.tab == tabLogs:
		t.logUp -= d
	default:
		t.sel[t.tab] += d
	}
	t.clamp()
}

func (t *tui) page() int { return max(t.h-8, 1) }

// handle takes one key.
func (t *tui) handle(k key) {
	if k.name == "ctrl-c" {
		t.quit = true
		return
	}
	// A line being typed, then a question, take every key.
	if in := t.input; in != nil {
		switch k.name {
		case "esc":
			t.input = nil
		case "enter":
			t.input = nil
			if s := strings.TrimSpace(string(in.buf)); s != "" {
				in.submit(s)
			}
		case "backspace":
			if len(in.buf) > 0 {
				in.buf = in.buf[:len(in.buf)-1]
			}
		case "rune":
			in.buf = append(in.buf, k.r)
		}
		return
	}
	if q := t.confirm; q != nil {
		t.confirm = nil
		if k.name == "enter" || k.name == "rune" && (k.r == 'y' || k.r == 'Y') { // yes is the default
			q.do()
		} else {
			t.say(i18n.T("已取消", "Canceled"), false)
		}
		return
	}
	if t.help {
		t.help = false
		return
	}
	switch k.name {
	case "up":
		t.move(-1)
		return
	case "down":
		t.move(1)
		return
	case "pgup":
		t.move(-t.page())
		return
	case "pgdn":
		t.move(t.page())
		return
	case "home":
		t.move(-1 << 20)
		return
	case "end":
		t.move(1 << 20)
		return
	}
	if k.name == "rune" {
		switch k.r {
		case 'k':
			t.move(-1)
			return
		case 'j':
			t.move(1)
			return
		case 'g':
			t.move(-1 << 20)
			return
		case 'G':
			t.move(1 << 20)
			return
		case '?':
			t.help = true
			return
		}
	}
	if ch := t.choose; ch != nil {
		switch {
		case k.name == "enter" || k.name == "rune" && k.r == ' ':
			t.choose = nil
			if ch.sel != ch.current {
				ch.done(ch.options[ch.sel])
			}
		case k.name == "esc" || k.name == "left" || k.name == "rune" && (k.r == 'q' || k.r == 'h'):
			t.choose = nil
		}
		return
	}
	if t.pick != nil {
		t.handlePicker(k)
		return
	}
	switch k.name {
	case "tab", "right":
		t.setTab((t.tab + 1) % numTabs)
		return
	case "backtab", "left":
		t.setTab((t.tab + numTabs - 1) % numTabs)
		return
	case "esc":
		if t.tab == tabConns && t.filter != "" {
			t.filter = ""
			t.poll(false)
		}
		return
	case "enter":
		t.enter()
		return
	case "rune":
	default:
		return
	}
	switch r := k.r; {
	case r == 'q':
		t.quit = true
	case r >= '1' && r < '1'+numTabs:
		t.setTab(int(r - '1'))
	case r == 'l':
		t.setTab((t.tab + 1) % numTabs)
	case r == 'h':
		t.setTab((t.tab + numTabs - 1) % numTabs)
	case r == 'r':
		t.poll(true)
	case r == ' ':
		t.enter()
	default:
		t.handlePage(r)
	}
}

// enter is Enter or the space bar on the row in front.
func (t *tui) enter() {
	switch t.tab {
	case tabHome:
		if sws := t.toggles(); t.sel[tabHome] < len(sws) {
			t.flip(sws[t.sel[tabHome]])
		} else {
			t.openPicker()
		}
	case tabSettings:
		t.editSetting()
	}
}

func (t *tui) setTab(i int) {
	t.tab = i
	t.poll(true)
}

func (t *tui) openPicker() {
	i := t.sel[tabHome] - len(t.toggles())
	if i < 0 || i >= len(t.st.Routes) {
		return
	}
	r := t.st.Routes[i]
	if r.Direct {
		t.say(i18n.T("这条路由不使用节点", "This route uses no node"), true)
		return
	}
	t.pick = &picker{route: r}
	if err := t.loadNodes(false); err != nil {
		t.say(err.Error(), true)
	}
	for i, n := range t.pick.nodes {
		if n.Name == t.pick.current {
			t.pick.sel = i
		}
	}
}

func (t *tui) handlePicker(k key) {
	p := t.pick
	use := func(nodeName, label string) {
		t.pick = nil
		t.call(i18n.T("正在切换节点…", "Switching the node…"), fmt.Sprintf(i18n.T("%s 现在使用 %s", "%s now uses %s"), p.route.Name, label),
			"select_node", map[string]any{"route": p.route.ID, "node": nodeName})
	}
	switch {
	case k.name == "esc" || k.name == "left" || k.name == "rune" && (k.r == 'q' || k.r == 'h'):
		t.pick = nil
	case k.name == "enter" && p.sel < len(p.nodes):
		use(p.nodes[p.sel].Name, p.nodes[p.sel].Label)
	case k.name == "rune" && k.r == 'a':
		use("auto", i18n.T("自动", "Auto"))
	case k.name == "rune" && k.r == 't':
		t.act(i18n.T("正在测速…", "Testing…"), i18n.T("测速完成", "Tested"), func() error { return t.loadNodes(true) })
	}
}

// handlePage takes a key that belongs to the page in front.
func (t *tui) handlePage(r rune) {
	switch t.tab {
	case tabHome:
		switch r {
		case 'm':
			labels := make([]string, len(modes))
			for i, m := range modes {
				labels[i] = modeName(m) + "  " + modeAbout(m)
			}
			cur := slices.Index(modes, t.st.Mode)
			t.choose = &chooser{title: i18n.T("代理模式", "Proxy mode"), options: modes, labels: labels, current: cur, sel: max(cur, 0), done: func(m string) {
				t.call(i18n.T("正在切换模式…", "Switching the mode…"), fmt.Sprintf(i18n.T("代理模式：%s", "The proxy mode is %s"), modeName(m)),
					"set_mode", map[string]any{"mode": m})
			}}
		case 'o':
			t.flip(t.toggles()[0])
		case 'a':
			t.addProvider()
		}
	case tabConns:
		if r == '/' {
			t.input = &prompt{label: i18n.T("过滤", "Filter"), buf: []rune(t.filter), submit: func(s string) {
				t.filter = s
				t.sel[tabConns] = 0
				t.poll(false)
			}}
		}
	case tabLogs:
		if r == 'v' {
			order := append([]string{""}, levels[1:]...) // all, info, warning, error
			t.level = order[(slices.Index(order, t.level)+1)%len(order)]
			t.logUp = 0
		}
	case tabRules:
		switch {
		case r == 'a':
			t.input = &prompt{label: i18n.T("添加规则（地址 路由）", "Add a rule (target route)"), submit: func(s string) {
				f := strings.Fields(s)
				if len(f) != 2 {
					t.say(i18n.T("要写成：地址 路由，例如 openai.com ai", "Write it as: target route, as openai.com ai"), true)
					return
				}
				t.call(i18n.T("正在添加规则…", "Adding the rule…"), fmt.Sprintf(i18n.T("%s 现在走 %s", "%s now goes to %s"), f[0], f[1]),
					"add_rule", map[string]any{"target": f[0], "route": f[1]})
			}}
		case r == 'd' && t.sel[tabRules] < len(t.rules):
			ru := t.rules[t.sel[tabRules]]
			t.confirm = &ask{fmt.Sprintf(i18n.T("删除规则 %s？", "Remove the rule for %s?"), ru.Value), func() {
				t.call(i18n.T("正在删除规则…", "Removing the rule…"), fmt.Sprintf(i18n.T("已删除规则 %s", "Removed the rule for %s"), ru.Value),
					"remove_rule", map[string]any{"target": ru.Value, "kind": ru.Kind})
			}}
		}
	case tabProviders:
		var p *providerView
		if t.sel[tabProviders] < len(t.provs) {
			p = &t.provs[t.sel[tabProviders]]
		}
		updating, updated := i18n.T("正在更新订阅…", "Updating subscriptions…"), i18n.T("已更新", "Updated")
		switch {
		case r == 'a':
			t.addProvider()
		case r == 'U':
			t.call(updating, updated, "update_providers", map[string]any{})
		case p == nil:
		case r == 'u':
			t.call(updating, updated, "update_providers", map[string]any{"id": p.ID})
		case r == 'e':
			done := fmt.Sprintf(i18n.T("已启用 %s", "Turned %s on"), p.Name)
			if p.Enabled {
				done = fmt.Sprintf(i18n.T("已停用 %s", "Turned %s off"), p.Name)
			}
			t.call(i18n.T("正在应用…", "Applying…"), done, "provider_enable", map[string]any{"id": p.ID, "enabled": !p.Enabled})
		case r == 'd':
			id, name := p.ID, p.Name
			t.confirm = &ask{fmt.Sprintf(i18n.T("删除 %s 和它的节点？", "Remove %s and its nodes?"), name), func() {
				t.call(i18n.T("正在删除…", "Removing…"), fmt.Sprintf(i18n.T("已删除 %s", "Removed %s"), name), "provider_remove", map[string]any{"id": id})
			}}
		}
	}
}
