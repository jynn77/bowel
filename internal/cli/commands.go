package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/oboo/moho/internal/i18n"
)

var (
	onOffChoices = []string{"on", "off"}
	modes        = []string{"rule", "global", "direct"}
	levels       = []string{"debug", "info", "warning", "error"}
	ruleKinds    = []string{"suffix", "domain", "keyword", "ip"}
	shells       = []string{"bash", "zsh", "fish", "powershell"}
)

// tree is every command.
func tree() *Command {
	local := func(name string) func(c *Ctx) error {
		return func(c *Ctx) error {
			run := c.env.Local[name]
			if run == nil {
				return errors.New(name + ": not available")
			}
			return run(c.args)
		}
	}
	return &Command{
		Name:  "bowel",
		Short: words{"bowel：mihomo 代理客户端。不带命令时打开应用，带命令时操作运行中的 bowel。", "bowel, a client for the mihomo proxy core. Without a command it opens the app; with one it drives the running bowel."},
		Examples: []string{
			"bowel status",
			"bowel on",
			"bowel mode global",
			"bowel route use ai auto",
			"bowel logs -f --level warning",
			"bowel daemon",
		},
		Sub: []*Command{
			{Name: "status", Short: words{"查看状态：状态灯、模式、接管、各路由的节点和流量", "Show the state: the light, the mode, the takeover, each route's node and the traffic"},
				Examples: []string{"bowel status", "bowel status --json | jq .state"}, Run: runStatus},
			{Name: "on", Short: words{"开始接管这台电脑的流量（系统代理）", "Start taking over this computer's traffic (the system proxy)"},
				Examples: []string{"bowel on"}, Run: func(c *Ctx) error { return runTakeover(c, true) }},
			{Name: "off", Short: words{"停止接管，系统代理恢复原样", "Stop taking over; the system proxy goes back as it was"},
				Examples: []string{"bowel off"}, Run: func(c *Ctx) error { return runTakeover(c, false) }},
			{Name: "mode", Args: "[rule|global|direct]", Max: 1, Choices: modes,
				Short:    words{"查看或切换代理模式", "Show or switch the proxy mode"},
				Examples: []string{"bowel mode", "bowel mode global", "bowel mode rule"}, Run: runMode},
			{Name: "routes", Short: words{"列出路由，以及每条路由现在用的节点", "List the routes and the node each uses now"},
				Examples: []string{"bowel routes", "bowel routes --json"}, Run: runRoutes},
			{Name: "route", Short: words{"操作一条路由", "Work on one route"}, Sub: []*Command{
				{Name: "use", Args: i18n.T("<路由> <节点|auto>", "<route> <node|auto>"), Min: 2, Max: 2,
					Short:    words{"让一条路由使用某个节点，auto 为自动选最快的", "Make a route use a node; auto picks the fastest"},
					Examples: []string{"bowel route use ai auto", `bowel route use default "🇯🇵 Tokyo 02"`}, Run: runRouteUse},
			}},
			{Name: "nodes", Short: words{"列出节点和延迟", "List the nodes and their delays"},
				Flags: []Flag{
					{Name: "route", Short: "r", Kind: "string", Help: words{"按这条路由的测速目标显示延迟（默认 default）", "Show delays on this route's test target (default: default)"}},
					{Name: "test", Short: "t", Kind: "bool", Help: words{"先重新测速", "Test the delays first"}},
				},
				Examples: []string{"bowel nodes", "bowel nodes --test", "bowel nodes --route ai --test --json"}, Run: runNodes},
			{Name: "providers", Aliases: []string{"provider"}, Default: "list", Short: words{"管理 Provider（订阅、链接、文件）", "Manage Providers (subscriptions, links, files)"}, Sub: []*Command{
				{Name: "list", Aliases: []string{"ls"}, Short: words{"列出 Provider", "List the Providers"}, Examples: []string{"bowel providers list"}, Run: runProviders},
				{Name: "add", Args: i18n.T("<订阅地址|节点链接|文件>", "<subscription URL|node link|file>"), Min: 1, Max: 1,
					Short:    words{"添加 Provider", "Add a Provider"},
					Examples: []string{"bowel providers add https://example.com/sub?token=…", "bowel providers add ./nodes.yaml"}, Run: runProviderAdd},
				{Name: "update", Args: "[id]", Max: 1, Short: words{"重新下载订阅：指定一个，或全部", "Download subscriptions again: one, or all"},
					Examples: []string{"bowel providers update", "bowel providers update a1b2c3"}, Run: runProviderUpdate},
				{Name: "remove", Aliases: []string{"rm"}, Args: "<id>", Min: 1, Max: 1, Short: words{"删除 Provider 和它的节点", "Remove a Provider and its nodes"},
					Examples: []string{"bowel providers remove a1b2c3"}, Run: runProviderRemove},
				{Name: "enable", Args: "<id>", Min: 1, Max: 1, Short: words{"启用 Provider", "Turn a Provider on"},
					Examples: []string{"bowel providers enable a1b2c3"}, Run: func(c *Ctx) error { return runProviderEnable(c, true) }},
				{Name: "disable", Args: "<id>", Min: 1, Max: 1, Short: words{"停用 Provider，不再使用它的节点", "Turn a Provider off: its nodes are not used"},
					Examples: []string{"bowel providers disable a1b2c3"}, Run: func(c *Ctx) error { return runProviderEnable(c, false) }},
			}},
			{Name: "rules", Aliases: []string{"rule"}, Default: "list", Short: words{"管理你自己的规则", "Manage your own rules"}, Sub: []*Command{
				{Name: "list", Aliases: []string{"ls"}, Short: words{"列出你的规则，按生效顺序", "List your rules, in the order they apply"}, Examples: []string{"bowel rules list"}, Run: runRules},
				{Name: "add", Args: i18n.T("<地址> <路由>", "<target> <route>"), Min: 2, Max: 2,
					Short:    words{"让某个域名或 IP 走指定路由", "Send a domain or IP to a route"},
					Flags:    []Flag{{Name: "kind", Kind: "string", Choices: ruleKinds, Help: words{"规则类型，不写时自动判断", "The kind of rule; guessed when left out"}}},
					Examples: []string{"bowel rules add openai.com ai", "bowel rules add 10.0.0.0/8 direct", "bowel rules add example.com default --kind domain"}, Run: runRuleAdd},
				{Name: "remove", Aliases: []string{"rm"}, Args: i18n.T("<地址>", "<target>"), Min: 1, Max: 1,
					Short:    words{"删除一条规则", "Remove a rule"},
					Flags:    []Flag{{Name: "kind", Kind: "string", Choices: ruleKinds, Help: words{"规则类型，不写时自动判断", "The kind of rule; guessed when left out"}}},
					Examples: []string{"bowel rules remove openai.com"}, Run: runRuleRemove},
				{Name: "test", Args: i18n.T("<地址>", "<target>"), Min: 1, Max: 1,
					Short:    words{"查看某个地址会走哪条路由、由哪条规则决定", "Show which route an address would take, and the rule that decides"},
					Examples: []string{"bowel rules test api.openai.com", "bowel rules test https://github.com/foo --json"}, Run: runRuleTest},
			}},
			{Name: "conns", Aliases: []string{"connections"}, Short: words{"列出最近的连接", "List recent connections"},
				Flags: []Flag{
					{Name: "watch", Short: "w", Kind: "bool", Help: words{"持续显示新连接和结束的连接", "Keep showing connections as they open and end"}},
					{Name: "filter", Kind: "string", Help: words{"只看域名或应用名包含它的连接", "Only connections whose host or app contains this"}},
					{Name: "status", Kind: "string", Choices: []string{"active", "closed", "failed", "blocked"}, Help: words{"只看这种状态的连接", "Only connections in this state"}},
					{Name: "limit", Short: "n", Kind: "int", Help: words{"最多显示多少条（默认 30）", "At most this many (default 30)"}},
				},
				Examples: []string{"bowel conns", "bowel conns --filter github --status active", "bowel conns --watch --json"}, Run: runConns},
			{Name: "logs", Short: words{"查看内核日志", "Show the core's logs"},
				Flags: []Flag{
					{Name: "follow", Short: "f", Kind: "bool", Help: words{"持续输出新日志", "Keep printing new lines"}},
					{Name: "lines", Short: "n", Kind: "int", Help: words{"先显示最近多少行（默认 50）", "Start with this many recent lines (default 50)"}},
					{Name: "level", Kind: "string", Choices: levels, Help: words{"只看这个级别及更严重的", "Only this level and worse"}},
				},
				Examples: []string{"bowel logs", "bowel logs -f", "bowel logs -f --level warning --json"}, Run: runLogs},
			{Name: "settings", Default: "list", Short: words{"查看和修改设置", "Show and change the settings"}, Sub: []*Command{
				{Name: "list", Aliases: []string{"ls"}, Short: words{"列出所有设置", "List every setting"}, Examples: []string{"bowel settings list"}, Run: runSettingsList},
				{Name: "get", Args: "<key>", Min: 1, Max: 1, Short: words{"读取一项设置", "Read one setting"}, Examples: []string{"bowel settings get mixedPort"}, Run: runSettingsGet},
				{Name: "set", Args: i18n.T("<key> <值>", "<key> <value>"), Min: 2, Max: 2, Short: words{"修改一项设置；列表写成 JSON", "Change one setting; write a list as JSON"},
					Examples: []string{"bowel settings set logLevel debug", "bowel settings set allowLan true", `bowel settings set bypass '["localhost","*.lan"]'`, "bowel settings set mcpPolicy deny", `bowel settings set extraPorts '[{"port":7901,"route":"ai"}]'`}, Run: runSettingsSet},
			}},
			configCommand(),
			{Name: "tun", Args: "<on|off>", Min: 1, Max: 1, Choices: onOffChoices, Short: words{"打开或关闭 TUN（接管全部流量）", "Turn TUN on or off (it takes all traffic)"},
				Examples: []string{"bowel tun on", "bowel tun off"}, Run: runTun},
			{Name: "gateway", Args: "<on|off>", Min: 1, Max: 1, Choices: onOffChoices, Short: words{"打开或关闭网关模式（让局域网其他设备经这台电脑上网）", "Turn gateway mode on or off (other devices on the LAN go out through this computer)"},
				Examples: []string{"bowel gateway on", "bowel gateway off"}, Run: runGateway},
			{Name: "dns", Short: words{"DNS 相关操作", "Things to do with DNS"}, Sub: []*Command{
				{Name: "flush", Short: words{"清空内核的 DNS 缓存和 fake-ip 映射", "Clear the core's DNS cache and fake-ip mappings"}, Examples: []string{"bowel dns flush"}, Run: runDNSFlush},
			}},
			{Name: "tui", Aliases: []string{"ui"}, Short: words{"全屏界面：在终端里查看状态、选节点、看连接和日志、管理规则和 Provider", "The full-screen interface: the state, nodes, connections, logs, rules and Providers in a terminal"},
				Examples: []string{"bowel tui", "ssh server -t bowel tui"}, Run: runTUI},
			{Name: "daemon", Raw: true, Short: words{"无窗口运行 bowel（前台运行，适合 systemd、launchd 和 SSH）", "Run bowel without a window (in the foreground; for systemd, launchd and SSH)"},
				Examples: []string{"bowel daemon", "BOWEL_DATA_DIR=/var/lib/bowel bowel daemon"}, Run: local("daemon")},
			{Name: "stop", Short: words{"退出运行中的 bowel（应用或 daemon）", "Quit the running bowel (the app or the daemon)"}, Examples: []string{"bowel stop"}, Run: runStop},
			{Name: "mcp", Raw: true, Short: words{"给 agent 用的 MCP 服务（stdio）", "The MCP server for agents (stdio)"},
				Examples: []string{"claude mcp add --scope user bowel -- bowel mcp"}, Run: local("mcp")},
			{Name: "devtools", Raw: true, Args: "off", Short: words{"撤销 bowel 写进开发者工具的配置", "Undo what bowel wrote into developer tools"},
				Examples: []string{"bowel devtools off"}, Run: local("devtools")},
			{Name: "completion", Args: "<bash|zsh|fish|powershell>", Min: 1, Max: 1, Choices: shells, Short: words{"输出 shell 补全脚本", "Print a shell's completion script"},
				Examples: []string{"bowel completion zsh > ~/.zfunc/_bowel", "bowel completion bash > /etc/bash_completion.d/bowel", "bowel completion fish > ~/.config/fish/completions/bowel.fish"}, Run: runCompletion},
			{Name: "version", Short: words{"显示版本", "Show the version"}, Examples: []string{"bowel version"}, Run: runVersion},
		},
	}
}

// ---- Views of what the tools answer ----

type entry struct {
	Wanted bool   `json:"wanted"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type takeover struct {
	On      bool   `json:"on"`
	System  entry  `json:"system"`
	Tun     entry  `json:"tun"`
	Port    int    `json:"port"`
	Lan     bool   `json:"lan"`
	LanAddr string `json:"lanAddr"`
	Gateway entry  `json:"gateway"`
}

type route struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Direct    bool   `json:"direct"`
	Reject    bool   `json:"reject"`
	Auto      bool   `json:"auto"`
	Node      string `json:"node"`
	NodeLabel string `json:"nodeLabel"`
	UserRules int    `json:"userRules"`
}

type node struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Provider string `json:"provider"`
	Type     string `json:"type"`
	DelayMs  *int   `json:"delayMs"`
	Where    string `json:"where"`
}

type status struct {
	State string `json:"state"`
	Issue *struct {
		Text   string `json:"text"`
		Detail string `json:"detail"`
	} `json:"issue"`
	Core struct {
		State   string `json:"state"`
		Version string `json:"version"`
		Error   string `json:"error"`
	} `json:"core"`
	Mode      string   `json:"mode"`
	Takeover  takeover `json:"takeover"`
	Routes    []route  `json:"routes"`
	Nodes     int      `json:"nodes"`
	Providers int      `json:"providers"`
	Active    int      `json:"activeConnections"`
	Traffic   struct {
		DownBps   int64 `json:"downBps"`
		UpBps     int64 `json:"upBps"`
		TodayDown int64 `json:"todayDown"`
		TodayUp   int64 `json:"todayUp"`
	} `json:"traffic"`
	Version  string `json:"version"`
	Headless bool   `json:"headless"`
}

// nodeText is where a route goes, in a few words.
func (c *Ctx) nodeText(r route) cell {
	switch {
	case r.Reject:
		return cell{i18n.T("拒绝", "Reject"), gray}
	case r.Direct:
		return cell{i18n.T("直连", "Direct"), gray}
	case r.NodeLabel == "":
		return cell{"—", gray}
	case r.Auto:
		return t(r.NodeLabel + " (" + i18n.T("自动", "Auto") + ")")
	}
	return t(r.NodeLabel)
}

// statusFacts are the lines under the light: the version, the mode, the
// takeover and the traffic, each as its name and what it is now.
func (c *Ctx) statusFacts(st status) [][]string {
	ver := "bowel"
	if st.Version != "" {
		ver += " " + st.Version
	}
	coreText := "mihomo " + st.Core.Version
	if st.Core.State != "running" {
		coreText = i18n.T("内核", "core") + " " + st.Core.State
		if st.Core.Error != "" {
			coreText += ": " + st.Core.Error
		}
	}
	ver += " · " + coreText
	if st.Headless {
		ver += " · " + i18n.T("无窗口运行", "no window")
	}
	tk := st.Takeover
	// An entry that is not wanted is off, whatever turning it on would take.
	shown := func(e entry) cell {
		if !e.Wanted {
			return entryCell("off", "")
		}
		return entryCell(e.Status, e.Detail)
	}
	sys, tun := shown(tk.System), shown(tk.Tun)
	take := i18n.T("系统代理", "system proxy") + " " + c.paint(sys.color, sys.text) + " · TUN " + c.paint(tun.color, tun.text) +
		fmt.Sprintf(" · %s %d", i18n.T("端口", "port"), tk.Port)
	if tk.Lan && tk.LanAddr != "" {
		take += " · " + i18n.T("局域网", "LAN") + " " + tk.LanAddr
	}
	if tk.Gateway.Wanted {
		gw := entryCell(tk.Gateway.Status, tk.Gateway.Detail)
		take += " · " + i18n.T("网关", "gateway") + " " + c.paint(gw.color, gw.text)
	}
	traffic := fmt.Sprintf("↓ %s/s  ↑ %s/s · %s ↓ %s ↑ %s · %s", fmtBytes(st.Traffic.DownBps), fmtBytes(st.Traffic.UpBps),
		i18n.T("今日", "today"), fmtBytes(st.Traffic.TodayDown), fmtBytes(st.Traffic.TodayUp),
		fmt.Sprintf(i18n.T("%d 个连接", "%d connections"), st.Active))
	return [][]string{
		{i18n.T("版本", "Version"), ver},
		{i18n.T("模式", "Mode"), modeName(st.Mode)},
		{i18n.T("接管", "Takeover"), take},
		{i18n.T("流量", "Traffic"), traffic},
	}
}

func runStatus(c *Ctx) error {
	var st status
	raw, err := c.get("get_status", nil, &st)
	if err != nil {
		return err
	}
	if c.json {
		c.printJSON(raw, false)
		return nil
	}
	dot, word, color := stateCell(st.State)
	head := c.paint(color, dot+" "+word)
	if st.Issue != nil {
		head += "  " + st.Issue.Text
	}
	fmt.Fprintln(c.out, head)
	if st.Issue != nil && st.Issue.Detail != "" {
		fmt.Fprintln(c.out, "  "+c.paint(gray, st.Issue.Detail))
	}
	rows := c.statusFacts(st)
	for i, r := range st.Routes {
		label := ""
		if i == 0 {
			label = i18n.T("路由", "Routes")
		}
		n := c.nodeText(r)
		rows = append(rows, []string{label, pad(r.Name, routeWidth(st.Routes)) + "  " + c.paint(n.color, n.text)})
	}
	width := 0
	for _, r := range rows {
		width = max(width, dispWidth(r[0]))
	}
	for _, r := range rows {
		fmt.Fprintln(c.out, "  "+c.paint(gray, pad(r[0], width))+"  "+r[1])
	}
	return nil
}

func routeWidth(rs []route) int {
	w := 0
	for _, r := range rs {
		w = max(w, dispWidth(r.Name))
	}
	return w
}

// takeoverNotes warns of an entry that is wanted and is not on.
func (c *Ctx) takeoverNotes(tk takeover) {
	for _, e := range []struct {
		name string
		e    entry
	}{{i18n.T("系统代理", "system proxy"), tk.System}, {"TUN", tk.Tun}, {i18n.T("网关", "gateway"), tk.Gateway}} {
		if !e.e.Wanted || !tk.On || e.e.Status == "on" || e.e.Status == "checking" || e.e.Status == "off" {
			continue
		}
		cl := entryCell(e.e.Status, e.e.Detail)
		c.warn(e.name + ": " + cl.text)
	}
}

func runTakeover(c *Ctx, on bool) error {
	var out struct {
		Takeover takeover `json:"takeover"`
	}
	raw, err := c.get("set_takeover", map[string]any{"enabled": on}, &out)
	if err != nil {
		return err
	}
	if raw[0] != '{' { // nothing to change
		if on {
			c.done(raw, i18n.T("已经在接管了", "Already taking over"))
		} else {
			c.done(raw, i18n.T("本来就没有接管", "Not taking over already"))
		}
		return nil
	}
	if on {
		c.done(raw, fmt.Sprintf(i18n.T("已开始接管，代理端口 %d", "Taking over; the proxy port is %d"), out.Takeover.Port))
		if !c.json {
			c.takeoverNotes(out.Takeover)
		}
	} else {
		c.done(raw, i18n.T("已停止接管，系统代理已恢复", "Stopped taking over; the system proxy is back as it was"))
	}
	return nil
}

func runMode(c *Ctx) error {
	if len(c.args) == 0 {
		var st status
		if _, err := c.get("get_status", nil, &st); err != nil {
			return err
		}
		if c.json {
			b, _ := json.Marshal(map[string]string{"mode": st.Mode})
			c.printJSON(b, false)
		} else {
			fmt.Fprintln(c.out, st.Mode)
		}
		return nil
	}
	raw, err := c.call("set_mode", map[string]any{"mode": c.args[0]})
	if err != nil {
		return err
	}
	if c.json {
		b, _ := json.Marshal(map[string]any{"mode": c.args[0]})
		raw = b
	}
	c.done(raw, fmt.Sprintf(i18n.T("代理模式：%s", "The proxy mode is %s"), modeName(c.args[0])))
	return nil
}

func runRoutes(c *Ctx) error {
	var out struct {
		Routes   []route `json:"routes"`
		Imported *struct {
			Provider string `json:"provider"`
			Groups   []struct {
				Name       string `json:"name"`
				Kind       string `json:"kind"`
				Now        string `json:"now"`
				Selectable bool   `json:"selectable"`
			} `json:"groups"`
		} `json:"imported"`
	}
	raw, err := c.get("list_routes", nil, &out)
	if err != nil {
		return err
	}
	if c.json {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(raw, &m)
		delete(m, "nodes") // `bowel nodes` has them
		b, _ := json.Marshal(m)
		c.printJSON(b, false)
		return nil
	}
	var rows [][]cell
	for _, r := range out.Routes {
		rules := ""
		if r.UserRules > 0 {
			rules = fmt.Sprint(r.UserRules)
		}
		rows = append(rows, []cell{t(r.ID), t(r.Name), c.nodeText(r), t(rules), {r.Summary, gray}})
	}
	c.table([]string{"ID", i18n.T("路由", "Route"), i18n.T("节点", "Node"), i18n.T("我的规则", "My rules"), i18n.T("说明", "About")}, rows)
	if im := out.Imported; im != nil {
		fmt.Fprintln(c.out)
		fmt.Fprintf(c.out, i18n.T("正在使用 %s 自带的路由组：", "Using %s's own groups:")+"\n", im.Provider)
		rows = nil
		for _, g := range im.Groups {
			sel := ""
			if g.Selectable {
				sel = i18n.T("可选", "selectable")
			}
			rows = append(rows, []cell{t(g.Name), t(g.Kind), t(g.Now), {sel, gray}})
		}
		c.table([]string{i18n.T("路由组", "Group"), i18n.T("类型", "Kind"), i18n.T("当前", "Now"), ""}, rows)
	}
	return nil
}

func runRouteUse(c *Ctx) error {
	raw, err := c.call("select_node", map[string]any{"route": c.args[0], "node": c.args[1]})
	if err != nil {
		return err
	}
	c.done(raw, fmt.Sprintf(i18n.T("%s 现在使用 %s", "%s now uses %s"), c.args[0], c.args[1]))
	return nil
}

func runNodes(c *Ctx) error {
	rt := c.flag("route")
	if rt == "" {
		rt = "default"
	}
	var out struct {
		Route     string            `json:"route"`
		Current   string            `json:"current"`
		Nodes     []node            `json:"nodes"`
		Providers map[string]string `json:"providers"`
		Error     string            `json:"error"`
	}
	stop := func() {}
	if c.has("test") {
		stop = c.spin(i18n.T("正在测速…", "Testing…"))
	}
	raw, err := c.get("list_nodes", map[string]any{"route": rt, "test": c.has("test")}, &out)
	stop()
	if err != nil {
		return err
	}
	if out.Error != "" && !c.json {
		c.warn(out.Error)
	}
	if c.json {
		c.printJSON(raw, false)
		return nil
	}
	if len(out.Nodes) == 0 {
		fmt.Fprintln(c.out, i18n.T("还没有节点。用 `bowel providers add <订阅地址>` 添加一个 Provider。", "No nodes yet. Add a Provider with `bowel providers add <subscription URL>`."))
		return nil
	}
	var rows [][]cell
	for _, n := range out.Nodes {
		mark := cell{" ", ""}
		if n.Name == out.Current {
			mark = cell{"●", green}
		}
		prov := out.Providers[n.Provider]
		if prov == "" {
			prov = n.Provider
		}
		rows = append(rows, []cell{mark, t(n.Label), delayCell(n.DelayMs), t(n.Type), t(prov), {n.Where, gray}})
	}
	c.table([]string{" ", i18n.T("节点", "Node"), i18n.T("延迟", "Delay"), i18n.T("类型", "Kind"), "Provider", i18n.T("位置", "Location")}, rows)
	return nil
}

type providerView struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Source     string    `json:"source"`
	Enabled    bool      `json:"enabled"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Error      string    `json:"error"`
	Nodes      int       `json:"nodes"`
	AutoUpdate int       `json:"autoUpdate"`
	Usage      *struct {
		Upload   int64 `json:"upload"`
		Download int64 `json:"download"`
		Total    int64 `json:"total"`
		Expire   int64 `json:"expire"`
	} `json:"usage"`
}

func (c *Ctx) providerTable(ps []providerView) {
	if len(ps) == 0 {
		fmt.Fprintln(c.out, i18n.T("还没有 Provider。用 `bowel providers add <订阅地址>` 添加。", "No Providers yet. Add one with `bowel providers add <subscription URL>`."))
		return
	}
	var rows [][]cell
	for _, p := range ps {
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
		rows = append(rows, []cell{t(p.ID), t(p.Name), t(p.Kind), t(fmt.Sprint(p.Nodes)), t(usage), t(expire), t(date(p.UpdatedAt)), state})
	}
	c.table([]string{"ID", i18n.T("名称", "Name"), i18n.T("类型", "Kind"), i18n.T("节点", "Nodes"), i18n.T("流量", "Traffic"),
		i18n.T("到期", "Expires"), i18n.T("更新于", "Updated"), i18n.T("状态", "State")}, rows)
}

func runProviders(c *Ctx) error {
	var out struct {
		Providers []providerView `json:"providers"`
	}
	raw, err := c.get("list_providers", nil, &out)
	if err != nil {
		return err
	}
	if c.json {
		c.printJSON(raw, false)
		return nil
	}
	c.providerTable(out.Providers)
	return nil
}

func runProviderAdd(c *Ctx) error {
	args := map[string]any{"source": c.args[0]}
	// A file is named as this shell sees it; the app may run elsewhere.
	if !strings.Contains(c.args[0], "://") {
		if fi, err := os.Stat(c.args[0]); err == nil && !fi.IsDir() {
			abs, err := filepath.Abs(c.args[0])
			if err != nil {
				return err
			}
			args = map[string]any{"file": abs}
		}
	}
	var out struct {
		Provider providerView `json:"provider"`
	}
	stop := c.spin(i18n.T("正在添加 Provider…", "Adding the Provider…"))
	raw, err := c.get("provider_add", args, &out)
	stop()
	if err != nil {
		return err
	}
	c.done(raw, fmt.Sprintf(i18n.T("已添加 %s（id %s）", "Added %s (id %s)"), out.Provider.Name, out.Provider.ID))
	return nil
}

// providerID is the id of the provider s names, by id or by name.
func (c *Ctx) providerID(s string) (string, error) {
	var out struct {
		Providers []providerView `json:"providers"`
	}
	if _, err := c.get("list_providers", nil, &out); err != nil {
		return "", err
	}
	var hits []string
	for _, p := range out.Providers {
		if p.ID == s {
			return p.ID, nil
		}
		if strings.EqualFold(p.Name, s) {
			hits = append(hits, p.ID)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	return "", fmt.Errorf(i18n.T("没有这个 Provider: %s", "No such Provider: %s"), s)
}

func runProviderUpdate(c *Ctx) error {
	args := map[string]any{}
	if len(c.args) == 1 {
		id, err := c.providerID(c.args[0])
		if err != nil {
			return err
		}
		args["id"] = id
	}
	var out struct {
		Providers []providerView `json:"providers"`
	}
	stop := c.spin(i18n.T("正在更新订阅…", "Updating subscriptions…"))
	raw, err := c.get("update_providers", args, &out)
	stop()
	if err != nil {
		return err
	}
	c.done(raw, i18n.T("已更新", "Updated"))
	if !c.json {
		c.providerTable(out.Providers)
	}
	return nil
}

func runProviderRemove(c *Ctx) error {
	var out struct {
		Removed providerView `json:"removed"`
	}
	raw, err := c.get("provider_remove", map[string]any{"id": c.args[0]}, &out)
	if err != nil {
		return err
	}
	c.done(raw, fmt.Sprintf(i18n.T("已删除 %s", "Removed %s"), out.Removed.Name))
	return nil
}

func runProviderEnable(c *Ctx, on bool) error {
	raw, err := c.call("provider_enable", map[string]any{"id": c.args[0], "enabled": on})
	if err != nil {
		return err
	}
	if on {
		c.done(raw, fmt.Sprintf(i18n.T("已启用 %s", "Turned %s on"), c.args[0]))
	} else {
		c.done(raw, fmt.Sprintf(i18n.T("已停用 %s", "Turned %s off"), c.args[0]))
	}
	return nil
}

type rule struct {
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Route   string `json:"route"`
	Mihomo  string `json:"mihomo"`
	Matches string `json:"matches"`
}

func runRules(c *Ctx) error {
	var out struct {
		Rules    []rule            `json:"rules"`
		RuleSets []json.RawMessage `json:"ruleSets"`
	}
	raw, err := c.get("list_rules", nil, &out)
	if err != nil {
		return err
	}
	if c.json {
		c.printJSON(raw, false)
		return nil
	}
	if len(out.Rules) == 0 {
		fmt.Fprintln(c.out, i18n.T("还没有自己的规则。用 `bowel rules add <地址> <路由>` 添加。", "No rules of your own yet. Add one with `bowel rules add <target> <route>`."))
	} else {
		var rows [][]cell
		for _, r := range out.Rules {
			rows = append(rows, []cell{t(r.Kind), t(r.Value), t(r.Route), {r.Matches, gray}})
		}
		c.table([]string{i18n.T("类型", "Kind"), i18n.T("地址", "Target"), i18n.T("路由", "Route"), i18n.T("匹配", "Matches")}, rows)
	}
	if n := len(out.RuleSets); n > 0 {
		fmt.Fprintln(c.out, c.paint(gray, fmt.Sprintf(i18n.T("另有 %d 个导入的规则集，排在你的规则之后。", "Also %d imported rule sets, after your rules."), n)))
	}
	return nil
}

func runRuleAdd(c *Ctx) error {
	args := map[string]any{"target": c.args[0], "route": c.args[1]}
	if k := c.flag("kind"); k != "" {
		args["kind"] = k
	}
	var out struct {
		Rule   rule `json:"rule"`
		Closed int  `json:"closedConnections"`
	}
	raw, err := c.get("add_rule", args, &out)
	if err != nil {
		return err
	}
	if raw[0] != '{' {
		c.done(raw, i18n.T("这条规则已经有了", "That rule is there already"))
		return nil
	}
	msg := fmt.Sprintf(i18n.T("%s 现在走 %s", "%s now goes to %s"), out.Rule.Value, out.Rule.Route)
	if out.Closed > 0 {
		msg += fmt.Sprintf(i18n.T("，已断开 %d 个旧连接", "; closed %d old connections"), out.Closed)
	}
	c.done(raw, msg)
	return nil
}

func runRuleRemove(c *Ctx) error {
	args := map[string]any{"target": c.args[0]}
	if k := c.flag("kind"); k != "" {
		args["kind"] = k
	}
	var out struct {
		Removed rule `json:"removed"`
	}
	raw, err := c.get("remove_rule", args, &out)
	if err != nil {
		return err
	}
	c.done(raw, fmt.Sprintf(i18n.T("已删除规则 %s", "Removed the rule for %s"), out.Removed.Value))
	return nil
}

func runRuleTest(c *Ctx) error {
	var out struct {
		Host     string `json:"host"`
		Sure     bool   `json:"sure"`
		UserRule bool   `json:"userRule"`
		Route    string `json:"route"`
		Rule     string `json:"rule"`
		Note     string `json:"note"`
	}
	raw, err := c.get("explain", map[string]any{"target": c.args[0]}, &out)
	if err != nil {
		return err
	}
	if c.json {
		c.printJSON(raw, false)
		return nil
	}
	if !out.Sure {
		host := out.Host
		if host == "" {
			host = c.args[0]
		}
		fmt.Fprintln(c.out, host+" → "+c.paint(yellow, i18n.T("不确定", "not sure")))
		fmt.Fprintln(c.out, "  "+c.paint(gray, i18n.T("没有明确匹配的规则：由 GeoSite/GeoIP 数据决定，否则走默认路由。用 `bowel conns --filter <域名>` 看实际走向。",
			"No rule matches for sure: GeoSite/GeoIP data decides, else the default route. See what happens with `bowel conns --filter <host>`.")))
		return nil
	}
	fmt.Fprintln(c.out, out.Host+" → "+c.paint(bold, out.Route))
	whose := i18n.T("内置规则", "built-in rule")
	if out.UserRule {
		whose = i18n.T("你的规则", "your rule")
	}
	fmt.Fprintln(c.out, "  "+c.paint(gray, whose+": "+out.Rule))
	return nil
}

type conn struct {
	ID     string    `json:"id"`
	App    string    `json:"app"`
	Host   string    `json:"host"`
	IP     string    `json:"ip"`
	Port   int       `json:"port"`
	Route  string    `json:"route"`
	Node   string    `json:"node"`
	Down   int64     `json:"down"`
	Up     int64     `json:"up"`
	Start  time.Time `json:"start"`
	Status string    `json:"status"`
	Error  string    `json:"error"`
}

func connCells(cn conn) []cell {
	host := cn.Host
	if host == "" {
		host = cn.IP
	}
	if cn.Port != 0 {
		host += fmt.Sprintf(":%d", cn.Port)
	}
	st := cell{cn.Status, gray}
	switch cn.Status {
	case "active":
		st.color = green
	case "failed":
		st.color = red
	case "blocked":
		st.color = yellow
	}
	nodeName := cn.Node
	if nodeName == "" {
		nodeName = "—"
	}
	return []cell{{clock(cn.Start), gray}, t(cn.App), t(host), t(cn.Route), t(nodeName), t("↓" + fmtBytes(cn.Down)), t("↑" + fmtBytes(cn.Up)), st}
}

func runConns(c *Ctx) error {
	args := map[string]any{"limit": c.intFlag("limit", 30)}
	if f := c.flag("filter"); f != "" {
		args["filter"] = f
	}
	if s := c.flag("status"); s != "" {
		args["status"] = s
	}
	if c.has("watch") {
		return watchConns(c, args)
	}
	var out struct {
		Connections []conn `json:"connections"`
		Total       int    `json:"total"`
	}
	raw, err := c.get("list_connections", args, &out)
	if err != nil {
		return err
	}
	if c.json {
		c.printJSON(raw, false)
		return nil
	}
	if len(out.Connections) == 0 {
		fmt.Fprintln(c.out, i18n.T("没有连接。", "No connections."))
		return nil
	}
	var rows [][]cell
	for _, cn := range out.Connections {
		rows = append(rows, connCells(cn))
	}
	c.table([]string{i18n.T("时间", "Time"), i18n.T("应用", "App"), i18n.T("目标", "Host"), i18n.T("路由", "Route"), i18n.T("节点", "Node"), i18n.T("下载", "Down"), i18n.T("上传", "Up"), i18n.T("状态", "State")}, rows)
	if out.Total > len(out.Connections) {
		fmt.Fprintln(c.out, c.paint(gray, fmt.Sprintf(i18n.T("共 %d 个，显示最近 %d 个。", "%d in all; the %d newest shown."), out.Total, len(out.Connections))))
	}
	return nil
}

// watchConns prints each connection when it is first seen and again when
// its state changes: a line each, or with --json an object each (NDJSON).
func watchConns(c *Ctx, args map[string]any) error {
	args["limit"] = 500
	seen := map[string]string{}
	first := true
	for {
		var out struct {
			Connections []json.RawMessage `json:"connections"`
		}
		if _, err := c.get("list_connections", args, &out); err != nil {
			return err
		}
		for _, raw := range slices.Backward(out.Connections) { // oldest first
			var cn conn
			if json.Unmarshal(raw, &cn) != nil {
				continue
			}
			was, known := seen[cn.ID]
			seen[cn.ID] = cn.Status
			if known && was == cn.Status {
				continue
			}
			if first && cn.Status != "active" {
				continue // what ended before the watch began is not news
			}
			if c.json {
				c.printJSON(raw, true)
				continue
			}
			var b strings.Builder
			for i, cl := range connCells(cn) {
				if i > 0 {
					b.WriteString("  ")
				}
				b.WriteString(c.paint(cl.color, cl.text))
			}
			fmt.Fprintln(c.out, b.String())
		}
		first = false
		select {
		case <-c.env.Context.Done():
			return nil
		case <-time.After(c.env.Poll):
		}
	}
}

type logLine struct {
	Seq   int64     `json:"seq"`
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
}

func (c *Ctx) printLog(raw json.RawMessage) {
	if c.json {
		c.printJSON(raw, true)
		return
	}
	var l logLine
	if json.Unmarshal(raw, &l) != nil {
		return
	}
	color := gray
	switch l.Level {
	case "warning":
		color = yellow
	case "error":
		color = red
	case "info":
		color = green
	}
	fmt.Fprintf(c.out, "%s %s %s\n", c.paint(gray, clock(l.Time)), c.paint(color, pad(l.Level, 7)), l.Text)
}

// runLogs prints the recent lines; with -f it goes on with the new ones,
// a line each (with --json, an object each: NDJSON).
func runLogs(c *Ctx) error {
	args := map[string]any{"after": 0, "limit": c.intFlag("lines", 50)}
	if l := c.flag("level"); l != "" {
		args["level"] = l
	}
	for {
		var out struct {
			Logs []json.RawMessage `json:"logs"`
			Last int64             `json:"last"`
		}
		if _, err := c.get("tail_logs", args, &out); err != nil {
			return err
		}
		if c.json && !c.has("follow") {
			b, _ := json.Marshal(map[string]any{"logs": out.Logs})
			c.printJSON(b, false)
			return nil
		}
		for _, raw := range out.Logs {
			c.printLog(raw)
		}
		if !c.has("follow") {
			return nil
		}
		// From here on only what is new; with no limit, after 0 gives none.
		args["after"], args["limit"] = out.Last, 0
		select {
		case <-c.env.Context.Done():
			return nil
		case <-time.After(c.env.Poll):
		}
	}
}

type settingsView struct {
	Settings map[string]json.RawMessage `json:"settings"`
	Editable []string                   `json:"editable"`
	Choices  map[string][]string        `json:"choices"`
}

// plain is a setting's value as people write it: a string without quotes.
func plain(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func runSettingsList(c *Ctx) error {
	var out settingsView
	raw, err := c.get("get_settings", nil, &out)
	if err != nil {
		return err
	}
	if c.json {
		c.printJSON(raw, false)
		return nil
	}
	keys := make([]string, 0, len(out.Settings))
	for k := range out.Settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var rows [][]cell
	for _, k := range keys {
		v := plain(out.Settings[k])
		if dispWidth(v) > 60 {
			v = string([]rune(v)[:57]) + "…"
		}
		rows = append(rows, []cell{t(k), t(v), {strings.Join(out.Choices[k], " | "), gray}})
	}
	c.table([]string{i18n.T("设置", "Setting"), i18n.T("值", "Value"), i18n.T("可选值", "Choices")}, rows)
	return nil
}

// noSetting is the error for a key the settings don't have.
func noSetting(key string, out settingsView) error {
	keys := make([]string, 0, len(out.Settings))
	for k := range out.Settings {
		keys = append(keys, k)
	}
	msg := fmt.Sprintf(i18n.T("没有这项设置: %s", "No such setting: %s"), key)
	if s := suggest(key, keys); s != "" {
		msg += fmt.Sprintf(i18n.T("。你是不是想用 %s？", ". Did you mean %s?"), s)
	}
	return errors.New(msg)
}

func runSettingsGet(c *Ctx) error {
	var out settingsView
	if _, err := c.get("get_settings", nil, &out); err != nil {
		return err
	}
	v, ok := out.Settings[c.args[0]]
	if !ok {
		return noSetting(c.args[0], out)
	}
	if c.json {
		b, _ := json.Marshal(map[string]json.RawMessage{c.args[0]: v})
		c.printJSON(b, false)
	} else {
		fmt.Fprintln(c.out, plain(v))
	}
	return nil
}

// ownCommand are the settings that have a command of their own.
var ownCommand = map[string]string{
	"mode": "bowel mode <rule|global|direct>", "systemProxy": "bowel on / bowel off", "paused": "bowel on / bowel off",
	"tun": "bowel tun <on|off>", "rules": "bowel rules add / bowel rules remove", "gateway": "bowel gateway <on|off>",
}

// sendSetting changes one setting to val as it was typed: as JSON when it
// reads as JSON of the kind the setting has now (old), else as the string
// it is: debug, not "debug".
func (c *Ctx) sendSetting(key, val string, old json.RawMessage) (json.RawMessage, error) {
	quoted, _ := json.Marshal(val)
	tries := []json.RawMessage{json.RawMessage(val), quoted}
	if !json.Valid([]byte(val)) {
		tries = tries[1:]
	} else if len(old) > 0 && old[0] == '"' && val[0] != '"' {
		tries[0], tries[1] = tries[1], tries[0]
	}
	var raw json.RawMessage
	var err error
	for _, v := range tries {
		raw, err = c.call("update_settings", map[string]any{"changes": map[string]json.RawMessage{key: v}})
		if err == nil || !strings.HasPrefix(err.Error(), "bad value") {
			break
		}
	}
	return raw, err
}

func runSettingsSet(c *Ctx) error {
	key, val := c.args[0], c.args[1]
	if cmd := ownCommand[key]; cmd != "" {
		return usagef(c.path, i18n.T("%s 有自己的命令：%s", "%s has a command of its own: %s"), key, cmd)
	}
	var cur settingsView
	if _, err := c.get("get_settings", nil, &cur); err != nil {
		return err
	}
	old, ok := cur.Settings[key]
	if !ok {
		return noSetting(key, cur)
	}
	raw, err := c.sendSetting(key, val, old)
	if err != nil {
		if ch := cur.Choices[key]; len(ch) > 0 {
			return fmt.Errorf("%w (%s: %s)", err, i18n.T("可选值", "Choices"), strings.Join(ch, ", "))
		}
		return err
	}
	if raw[0] != '{' {
		c.done(raw, fmt.Sprintf(i18n.T("%s 已经是 %s", "%s is %s already"), key, val))
		return nil
	}
	var out struct {
		Changed []string `json:"changed"`
	}
	_ = json.Unmarshal(raw, &out)
	c.done(raw, strings.Join(out.Changed, "; "))
	return nil
}

func runTun(c *Ctx) error {
	on := c.args[0] == "on"
	var out struct {
		Takeover takeover `json:"takeover"`
	}
	raw, err := c.get("set_takeover", map[string]any{"tun": on}, &out)
	if err != nil {
		return err
	}
	switch {
	case raw[0] != '{':
		c.done(raw, fmt.Sprintf(i18n.T("TUN 已经是%s", "TUN is %s already"), onOff(on)))
	case on:
		c.done(raw, i18n.T("TUN 已打开", "TUN is on"))
		if !c.json {
			c.takeoverNotes(out.Takeover)
		}
	default:
		c.done(raw, i18n.T("TUN 已关闭", "TUN is off"))
	}
	return nil
}

func runGateway(c *Ctx) error {
	on := c.args[0] == "on"
	var out struct {
		Changed  bool     `json:"changed"`
		Takeover takeover `json:"takeover"`
	}
	raw, err := c.get("set_gateway", map[string]any{"enabled": on}, &out)
	if err != nil {
		return err
	}
	switch {
	case !out.Changed:
		c.done(raw, fmt.Sprintf(i18n.T("网关模式已经是%s", "Gateway mode is %s already"), onOff(on)))
	case on:
		c.done(raw, i18n.T("网关模式已打开", "Gateway mode is on"))
		if !c.json {
			c.takeoverNotes(out.Takeover)
		}
	default:
		c.done(raw, i18n.T("网关模式已关闭", "Gateway mode is off"))
	}
	return nil
}

func runDNSFlush(c *Ctx) error {
	raw, err := c.call("flush_dns", nil)
	if err != nil {
		return err
	}
	c.done(raw, i18n.T("已清空 DNS 缓存", "Flushed the DNS cache"))
	return nil
}

func runStop(c *Ctx) error {
	raw, err := c.call("stop", nil)
	if err != nil {
		return err
	}
	c.done(raw, i18n.T("bowel 正在退出", "bowel is quitting"))
	return nil
}

func runVersion(c *Ctx) error {
	if c.json {
		b, _ := json.Marshal(map[string]string{"version": c.env.Version})
		c.printJSON(b, false)
		return nil
	}
	fmt.Fprintln(c.out, "bowel "+c.env.Version)
	return nil
}
