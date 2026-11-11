// Package routes is Moho's routing template: kinds of traffic and the rules
// that recognise them. Providers only supply nodes; every provider's nodes
// can serve every route (decision D1 of docs/product-ia.md).
package routes

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/oboo/moho/internal/i18n"
)

// Route is a kind of traffic and where it may go.
type Route struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Color   string `json:"color"`   // CSS custom property of its color
	Summary string `json:"summary"` // what it matches, for people
	// Rules are mihomo rules without their target, e.g. "DOMAIN-SUFFIX,openai.com".
	Rules []string `json:"-"`
	// Direct routes never use a node.
	Direct bool `json:"direct"`
	// Reject routes are Direct ones whose traffic is refused, not let out.
	Reject bool `json:"reject,omitempty"`

	// What Build adds, for the groups as the user has them.
	Label    string    `json:"-"` // the user's name for it; Name stays the mihomo group's
	Custom   bool      `json:"-"` // made by the user
	Policy   Policy    `json:"-"`
	Services []Service `json:"-"`
}

// Default is the route that takes what no rule matched.
const Default = "default"

// defaultGroup is the default route's mihomo group. The interface calls the
// route 其他流量 (Title); this name stays, since it is what the core
// remembers the selector by.
const defaultGroup = "默认"

// Reject is the route whose traffic is refused.
const Reject = "reject"

// Group is the name of a route's mihomo group; Auto that of its automatic
// (fastest + failover) group.
func Group(r Route) string { return r.Name }
func Auto(r Route) string  { return r.Name + "·自动" }

// Backup is the name of a route's ordered fallback group, and BackupStep
// that of its i'th place: mihomo keeps a provider's own order inside a
// group, so each place is a group of one node.
func Backup(r Route) string            { return r.Name + "·备用" }
func BackupStep(r Route, i int) string { return fmt.Sprintf("%s·备用·%d", r.Name, i+1) }

// Internal says whether name is one of a route's helper groups.
func Internal(r Route, name string) bool { return strings.HasPrefix(name, r.Name+"·") }

// NodeGroups says whether the core has groups of nodes for r. The default
// route keeps its own while it goes direct: global mode still uses them.
func (r Route) NodeGroups() bool { return !r.Direct || r.ID == Default && r.Policy.Direct }

// OwnExit says whether a route that goes direct by the user's choice keeps
// a group of its own in the core, holding DIRECT alone: the core then names
// the group for its connections, as it does for one that uses nodes. The
// default route has none, since MATCH is the rule that says whose they are.
func (r Route) OwnExit() bool { return r.Policy.Direct && r.ID != Default && ChoosesExit(r.ID) }

// Title is a route's name as the interface shows it. Name stays the
// mihomo group's, in Chinese, so that configs and choices keep across a
// change of language.
func Title(r Route) string {
	if r.Label != "" {
		return r.Label
	}
	if en, ok := titleEn[r.ID]; ok {
		if r.ID == Default && r.Name == defaultGroup {
			return i18n.T("其他流量", en)
		}
		return i18n.T(r.Name, en)
	}
	return r.Name
}

// Describe is a route's Summary as the interface shows it.
func Describe(r Route) string {
	if en, ok := summaryEn[r.Summary]; ok {
		return i18n.T(r.Summary, en)
	}
	return r.Summary
}

var titleEn = map[string]string{Default: "Other", "ai": "AI", "media": "Streaming", "dev": "Dev", "direct": "Direct", Reject: "Block"}

var summaryEn = map[string]string{
	"没命中任何规则的流量":                              "Traffic no rule matched",
	"OpenAI · Claude · Gemini · Perplexity 等": "OpenAI · Claude · Gemini · Perplexity and more",
	"YouTube · Netflix · Spotify · Disney+ 等": "YouTube · Netflix · Spotify · Disney+ and more",
	"GitHub · npm · Docker · PyPI · Go 等":     "GitHub · npm · Docker · PyPI · Go and more",
	"国内网站 · 局域网":                              "Mainland China sites · LAN",
	"命中规则的连接会被拒绝":                             "Connections matching its rules are refused",
}

// Template returns the routes in the order their rules apply; the default
// route comes first for display, its rule (MATCH) last.
func Template() []Route {
	return []Route{
		{ID: Default, Name: defaultGroup, Color: "--r-default", Summary: "没命中任何规则的流量"},
		{ID: "ai", Name: "AI 服务", Color: "--r-ai", Summary: "OpenAI · Claude · Gemini · Perplexity 等", Rules: builtinRules("ai")},
		{ID: "media", Name: "流媒体", Color: "--r-media", Summary: "YouTube · Netflix · Spotify · Disney+ 等", Rules: builtinRules("media")},
		{ID: "dev", Name: "开发", Color: "--r-dev", Summary: "GitHub · npm · Docker · PyPI · Go 等", Rules: builtinRules("dev")},
		{ID: "direct", Name: "直连", Color: "--r-direct", Summary: "国内网站 · 局域网", Direct: true, Rules: builtinRules("direct")},
		{ID: Reject, Name: "拦截", Color: "--r-reject", Summary: "命中规则的连接会被拒绝", Direct: true, Reject: true},
	}
}

// Count is the number of rules, MATCH included.
func Count(rs []Route) int {
	n := 1
	for _, r := range rs {
		n += len(r.Rules)
	}
	return n
}

// Target is a site a route is tested against.
type Target struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// MaxTargets is how many sites a route can be tested against. Each is a
// request through the node every few minutes.
const MaxTargets = 6

// DefaultTargets are the sites route id is tested against unless the user
// changes them, the first being the main target. The default route starts
// with the test URL of the settings, which is added by Settings.TargetsOf.
func DefaultTargets(id string) []Target {
	switch id {
	case Default:
		return []Target{{"Google", "https://www.google.com"}, {"GitHub", "https://github.com"}}
	case "ai":
		return []Target{{"OpenAI", "https://api.openai.com"}, {"Claude", "https://claude.ai"}, {"Gemini", "https://gemini.google.com"}}
	case "media":
		return []Target{{"YouTube", "https://www.youtube.com/generate_204"}, {"Netflix", "https://www.netflix.com"},
			{"Spotify", "https://open.spotify.com"}, {"Disney+", "https://www.disneyplus.com"}}
	case "dev":
		// raw, not github.com: that one times out on most nodes when they
		// are all tested at once, and Auto would count them as dead.
		return []Target{{"GitHub Raw", "https://raw.githubusercontent.com"}, {"GitHub", "https://github.com"}, {"npm", "https://registry.npmjs.org"},
			{"Docker Hub", "https://registry-1.docker.io/v2/"}, {"PyPI", "https://pypi.org"}}
	}
	return nil
}

// ParseTarget makes a target of an address as typed: the scheme is optional,
// and the site's name, its host without "www.", stands for it.
func ParseTarget(input string) (Target, error) {
	s := strings.TrimSpace(input)
	if s != "" && !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || strings.ContainsAny(u.Host, " \t") {
		return Target{}, fmt.Errorf(i18n.T("测速站点要是 http 或 https 链接: %s", "A test site must be an http or https link: %s"), input)
	}
	return Target{Name: strings.TrimPrefix(u.Hostname(), "www."), URL: s}, nil
}

// ValidTargets says what is wrong with a list of targets for route id.
func ValidTargets(id string, list []Target) error {
	if id == "direct" || id == Reject { // the groups no node carries
		return fmt.Errorf(i18n.T("路由 %s 不能设置测速站点", "Route %s can't have test sites"), id)
	}
	if len(list) == 0 {
		return errors.New(i18n.T("至少要留一个测速站点", "Keep at least one test site"))
	}
	if len(list) > MaxTargets {
		return fmt.Errorf(i18n.T("一条路由最多 %d 个测速站点", "A route can have at most %d test sites"), MaxTargets)
	}
	for i, t := range list {
		if _, err := ParseTarget(t.URL); err != nil {
			return err
		}
		if t.Name == "" || !strings.Contains(t.URL, "://") {
			return fmt.Errorf(i18n.T("测速站点要是 http 或 https 链接: %s", "A test site must be an http or https link: %s"), t.URL)
		}
		if slices.ContainsFunc(list[:i], func(o Target) bool { return o.URL == t.URL }) {
			return fmt.Errorf(i18n.T("测速站点 %s 重复了", "Test site %s is there twice"), t.URL)
		}
	}
	return nil
}
