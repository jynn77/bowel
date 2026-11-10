package bridge

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/routes"
)

// viewData is what the 代理 page and the sheets draw. It is a cut of the
// desktop's Snapshot, the fields the MVP's pages use.
type viewData struct {
	Language  string         `json:"language"`
	VPN       string         `json:"vpn"`
	VPNError  string         `json:"vpnError,omitempty"`
	Mode      string         `json:"mode"`
	Issue     *issueData     `json:"issue,omitempty"`
	Routes    []routeData    `json:"routes"`
	Nodes     []nodeData     `json:"nodes"`
	Exit      *nodeData      `json:"exit,omitempty"`
	You       *placeData     `json:"you,omitempty"`
	Providers []providerData `json:"providers"`
	// RuleCount is every rule in the config, the 模式 card's hint.
	RuleCount int `json:"ruleCount"`
	// UserRules is how many rules the user added, the 我的规则 row.
	UserRules int `json:"userRules"`
	// Core is what the 设置 page says about mihomo.
	Core string `json:"core"`
}

// issueData is what the 代理 page says is wrong, the desktop's title bar.
// Action is providers, activity or fix:<route id>; "" has none.
type issueData struct {
	Level  string `json:"level"`
	Text   string `json:"text"`
	Detail string `json:"detail,omitempty"`
	Action string `json:"action,omitempty"`
	Label  string `json:"label,omitempty"`
}

type placeData struct {
	Country string  `json:"country"`
	City    string  `json:"city"`
	Flag    string  `json:"flag"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

type routeData struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Color  string  `json:"color"`
	Direct bool    `json:"direct"`
	Reject bool    `json:"reject,omitempty"`
	Auto   bool    `json:"auto"`
	Policy string  `json:"policy"`           // auto, order, fixed
	Broken string  `json:"broken,omitempty"` // scope, order, fixed; "" when it has a way out
	Node   string  `json:"node"`
	Share  float64 `json:"share"`
	Rules  int     `json:"rules"`  // how many rules send traffic here
	Detail string  `json:"detail"` // what it matches, for people
}

type nodeData struct {
	Name     string    `json:"name"`
	Display  string    `json:"display"`
	Provider string    `json:"provider"`
	Type     string    `json:"type"`
	Delay    int       `json:"delay"`
	Where    placeData `json:"where"`
	Source   string    `json:"source"` // 实测, 按名称, 未知
	// ProviderName is the Provider's name as a node row shows it.
	ProviderName string `json:"providerName"`
	// H is the node's latest delays, one bar each, oldest first.
	H []int `json:"h"`
}

type providerData struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Nodes   int    `json:"nodes"`
	Used    int64  `json:"used"`
	Total   int64  `json:"total"`
	Expire  int64  `json:"expire"`
	Error   string `json:"error,omitempty"`
	Updated int64  `json:"updated"`
	Enabled bool   `json:"enabled"`
	// AutoUpdate is in hours; 0 is off.
	AutoUpdate int      `json:"autoUpdate"`
	Source     string   `json:"source,omitempty"`   // a subscription's host, never its token
	Warn       string   `json:"warnKind,omitempty"` // quota, expire
	UsedBy     []string `json:"usedBy,omitempty"`   // the routes on one of its nodes now
	// Dots is the health of a short list's nodes, when the view only carries
	// some of them; empty means work it out from the nodes.
	Dots []int `json:"dots,omitempty"`
}

// tickData is the once-a-second push.
type tickData struct {
	Down      int64              `json:"down"`
	Up        int64              `json:"up"`
	ByRoute   map[string]int64   `json:"byRoute"`
	Today     map[string]float64 `json:"today,omitempty"` // each route's part of today's traffic
	Active    int                `json:"active"`
	TodayDown int64              `json:"todayDown"`
	TodayUp   int64              `json:"todayUp"`
	// GoNanos and PrevBytes are the previous tick's work in Go and its size.
	GoNanos   int64 `json:"goNanos"`
	PrevBytes int   `json:"prevBytes"`
	SentMs    int64 `json:"sentMs"` // when Go handed it over, for the one-way latency
	MonthDown int64 `json:"monthDown"`
	// Ranges is what each route carried lately. Sent every few seconds, not each tick.
	Ranges *rangesData `json:"ranges,omitempty"`
}

// rangesData is bytes by route, for the last 7 days and the last 30, today included.
type rangesData struct {
	Week  map[string]int64 `json:"week"`
	Month map[string]int64 `json:"month"`
}

type connData struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Route    string `json:"route"`
	Node     string `json:"node"`
	Status   string `json:"status"`
	DownRate int64  `json:"downRate"`
	UpRate   int64  `json:"upRate"`
	Start    int64  `json:"start"`
	End      int64  `json:"end,omitempty"`
	Rule     string `json:"rule"` // what matched it, for people
	RuleType string `json:"ruleType,omitempty"`
	Payload  string `json:"payload,omitempty"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Net      string `json:"net"` // TCP, UDP
	Down     int64  `json:"down"`
	Up       int64  `json:"up"`
	Err      string `json:"err,omitempty"`
}

type logData struct {
	Time  int64  `json:"time"`
	Level string `json:"level"` // info, warn
	Text  string `json:"text"`
}

func (x *bridge) view() viewData {
	x.mu.Lock()
	vpn, vErr, px, prov, version, sw := x.vpn, x.vpnErr, x.proxies, x.provNodes, x.version, x.switched
	x.mu.Unlock()
	st := x.set.Get()
	all := x.routes()
	v := viewData{Language: i18n.Lang(), VPN: vpn, VPNError: vErr, Mode: st.Mode, Routes: []routeData{}, Nodes: []nodeData{}, Providers: []providerData{}}
	v.You = x.youPlace()
	used := map[string][]string{}
	for _, r := range all {
		if r.Direct || r.Reject {
			continue
		}
		if p := providerOf(currentNode(px, r)); p != "" {
			used[p] = append(used[p], routes.Title(r))
		}
	}
	for _, p := range x.store.List() {
		pname := p.Name
		// The core lists a Provider's nodes while it runs; the file does otherwise.
		names := prov[p.ID]
		if len(names) == 0 {
			names = fullNames(x.store.Names(p.ID), p.ID)
		}
		pv := providerData{ID: p.ID, Name: p.Name, Kind: string(p.Kind), Nodes: len(names), Error: p.Error, Updated: p.UpdatedAt.UnixMilli(),
			Enabled: p.Enabled, AutoUpdate: p.AutoUpdate, Source: maskSource(p), UsedBy: used[p.ID]}
		if u := p.Usage; u != nil {
			pv.Used, pv.Total, pv.Expire = u.Upload+u.Download, u.Total, u.Expire
			if u.Total > 0 && float64(u.Upload+u.Download)/float64(u.Total) > .9 {
				pv.Warn = "quota"
			} else if u.Expire > 0 && time.Until(time.Unix(u.Expire, 0)) < 7*24*time.Hour {
				pv.Warn = "expire"
			}
		}
		v.Providers = append(v.Providers, pv)
		if !p.Enabled {
			continue // its nodes are not on offer
		}
		for _, n := range names {
			nv := x.located(nodeView(n, p.ID, px[n]))
			nv.ProviderName = pname
			// A pasted link's node goes by its Provider's name, so renaming
			// the Provider renames the node, as on the desktop.
			if len(names) == 1 && p.Kind == provider.Link {
				nv.Display = pname
			}
			v.Nodes = append(v.Nodes, nv)
		}
	}
	v.UserRules = len(st.Rules)
	v.Core = i18n.T("mihomo · 未运行", "mihomo · stopped")
	if vpn == "on" {
		v.Core = i18n.T("mihomo · 运行中", "mihomo · running")
		if version != "" {
			v.Core = "mihomo " + version
		}
	}
	for _, r := range all {
		rv := routeData{ID: r.ID, Name: routes.Title(r), Color: routeColor(r.Color), Direct: r.Direct, Reject: r.Reject, Policy: r.Policy.Mode, Rules: routeRules(r), Detail: routes.Describe(r)}
		v.RuleCount += rv.Rules
		if !r.Direct && !r.Reject {
			rv.Broken = brokenWhy(r.Policy, v.Nodes)
			rv.Auto = r.Policy.Mode != routes.ModeFixed
			if r.Policy.Mode == routes.ModeFixed {
				rv.Node = r.Policy.Fixed // what it will be on, with the VPN off too
			}
			if px != nil {
				g := px[routes.Group(r)]
				rv.Auto = g.Now == routes.Auto(r) || g.Now == routes.Backup(r)
				if now := currentNode(px, r); now != "" && now != "DIRECT" {
					rv.Node = now
				}
				if r.ID == routes.Default && rv.Node != "" {
					n := x.located(nodeView(rv.Node, providerOf(rv.Node), px[rv.Node]))
					v.Exit = &n
				}
			}
		}
		v.Routes = append(v.Routes, rv)
	}
	if vpn == "on" {
		v.Issue = judge(st.Mode, v, sw)
	}
	return v
}

// judge is the first thing worth saying about a running tunnel.
func judge(mode string, v viewData, sw *switchNote) *issueData {
	if len(v.Nodes) == 0 && mode != "direct" {
		return &issueData{Level: "warn", Text: i18n.T("还没有节点，流量正在直连", "No nodes yet; traffic is going direct"), Action: "providers", Label: i18n.T("添加提供商", "Add Provider")}
	}
	if sw != nil && time.Since(sw.at) < 10*time.Minute {
		name := sw.route
		if i := slices.IndexFunc(v.Routes, func(r routeData) bool { return r.ID == sw.route }); i >= 0 {
			name = v.Routes[i].Name
		}
		return &issueData{Level: "warn", Text: fmt.Sprintf(i18n.T("%s：%s 不可用，已换到 %s", "%s: %s is down, switched to %s"), name, sw.from, sw.to), Action: "activity", Label: i18n.T("查看", "View")}
	}
	inUse := func(r routeData) bool { return mode == "rule" || r.ID == routes.Default }
	for _, r := range v.Routes {
		if r.Broken == "" || !inUse(r) {
			continue
		}
		is := &issueData{Level: "error", Action: "fix:" + r.ID}
		switch r.Broken {
		case "fixed":
			is.Text = fmt.Sprintf(i18n.T("「%s」固定的节点不通", "%s: its fixed node is down"), r.Name)
			is.Label = i18n.T("换节点", "Change node")
		case "order":
			is.Text = fmt.Sprintf(i18n.T("「%s」备用列表里的节点都不通", "%s: every node on its list is down"), r.Name)
			is.Label = i18n.T("调整顺序", "Edit order")
		default:
			is.Text = fmt.Sprintf(i18n.T("「%s」的范围里没有可用节点", "%s: no usable node in its range"), r.Name)
			is.Label = i18n.T("改范围", "Change range")
		}
		is.Detail = i18n.T("这个路由组现在连不上。", "This route group can't connect now.")
		return is
	}
	for _, r := range v.Routes {
		if r.Direct || r.Reject || r.Node == "" || !inUse(r) {
			continue
		}
		for _, n := range v.Nodes {
			if n.Name == r.Node && len(n.H) > 0 && n.Delay == 0 {
				return &issueData{Level: "warn", Text: fmt.Sprintf(i18n.T("%s：%s 测速失败", "%s: %s failed the speed test"), r.Name, n.Display)}
			}
		}
	}
	for _, p := range v.Providers {
		switch p.Warn {
		case "quota":
			return &issueData{Level: "warn", Text: fmt.Sprintf(i18n.T("%s 流量快用完了", "%s is almost out of data"), p.Name), Action: "providers", Label: i18n.T("查看", "View")}
		case "expire":
			return &issueData{Level: "warn", Text: fmt.Sprintf(i18n.T("%s 快到期了", "%s expires soon"), p.Name), Action: "providers", Label: i18n.T("查看", "View")}
		}
	}
	return nil
}

// brokenWhy says why a group has no way out now, "" if it has one: only
// what has been tested counts as down.
func brokenWhy(q routes.Policy, nodes []nodeData) string {
	if len(nodes) == 0 {
		return ""
	}
	alive := func(n nodeData) bool { return len(n.H) == 0 || n.Delay > 0 }
	switch q.Mode {
	case routes.ModeFixed:
		if !slices.ContainsFunc(nodes, func(n nodeData) bool { return n.Name == q.Fixed && alive(n) }) {
			return "fixed"
		}
	case routes.ModeOrder:
		if !slices.ContainsFunc(nodes, func(n nodeData) bool { return slices.Contains(q.Order, n.Name) && alive(n) }) {
			return "order"
		}
	default:
		if q.Scoped() && !slices.ContainsFunc(nodes, func(n nodeData) bool { return inScope(q, n.Provider, n.Where.Country) && alive(n) }) {
			return "scope"
		}
	}
	return ""
}

// routeRules is how many live rules send traffic to r, its user's included.
func routeRules(r routes.Route) int {
	n := 0
	for _, s := range r.Services {
		if !s.Off {
			n += len(s.Live())
		}
	}
	return n
}

// currentNode is the node a route's selector ends up on, "" if none.
func currentNode(px map[string]mihomo.Proxy, r routes.Route) string {
	now := px[routes.Group(r)].Now
	for range 4 { // through its automatic or fallback groups
		if now == "" || !routes.Internal(r, now) {
			break
		}
		now = px[now].Now
	}
	return now
}

// fullNames are a Provider's nodes as the core calls them.
func fullNames(names []string, id string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n + core.NodeSep + id
	}
	return out
}

// maskSource hides a subscription's token: only scheme and host are shown.
func maskSource(p provider.Provider) string {
	if p.Kind == provider.Subscription {
		if u, err := url.Parse(p.Source); err == nil {
			return u.Scheme + "://" + u.Host + "/…"
		}
	}
	return ""
}

// barHeight turns a delay into the height of its bar, 2 (timed out or slow) to 11.
func barHeight(d int) int {
	if d <= 0 {
		return 2
	}
	return max(2, min(11, 12-d/25))
}

func routeColor(c string) string { return strings.TrimPrefix(c, "--") }

func providerOf(node string) string {
	_, id, _ := strings.Cut(node, core.NodeSep)
	return id
}

func nodeView(name, provider string, p mihomo.Proxy) nodeData {
	n := nodeData{Name: name, Display: display(name), Provider: provider, Type: p.Type, Where: placeOf(name), Source: "按名称"}
	if n.Where.City == "" && n.Where.Country == "" {
		n.Source = "未知"
	}
	if l := len(p.History); l > 0 {
		n.Delay = p.History[l-1].Delay
		for _, h := range p.History[max(0, l-10):] {
			n.H = append(n.H, barHeight(h.Delay))
		}
	}
	return n
}
