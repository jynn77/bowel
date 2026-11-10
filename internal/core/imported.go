package core

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dlclark/regexp2"
	"gopkg.in/yaml.v3"

	"github.com/oboo/moho/internal/devnet"
	"github.com/oboo/moho/internal/i18n"
)

// Imported is the routing a complete Clash config brings with it: its nodes,
// groups and rules, and what those depend on. Everything else in such a
// config (ports, DNS, TUN, listeners, a controller, scripts) is Moho's to
// decide or none of the source's business, and is never read.
//
// The nodes keep the names the source gave them and go at the top level of
// the runtime config. A group's `proxies:` can only name top-level proxies,
// not those of a proxy-provider, so the suffixed names of the provider's
// copy could not be used here; and with the names unchanged no reference
// has to be rewritten and a group's `filter` still means what it meant.
type Imported struct {
	Proxies        []map[string]any          `yaml:"proxies"`
	Groups         []map[string]any          `yaml:"proxy-groups"`
	Rules          []string                  `yaml:"rules"`
	SubRules       map[string][]string       `yaml:"sub-rules"`
	RuleProviders  map[string]map[string]any `yaml:"rule-providers"`
	ProxyProviders map[string]map[string]any `yaml:"proxy-providers"`
}

// builtinTargets exist in every core without being defined.
var builtinTargets = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "PASS-RULE", "COMPATIBLE", "GLOBAL"}

// ParseImported reads the routing out of a provider's content. It gives nil
// and no error when the content has no groups (a list of nodes), and an
// error saying what is wrong when it has groups Moho cannot use.
func ParseImported(data []byte) (*Imported, error) {
	var im Imported
	if err := yaml.Unmarshal(data, &im); err != nil || len(im.Groups) == 0 {
		return nil, nil // base64, node links, or nothing of the kind
	}
	if err := im.check(); err != nil {
		return nil, err
	}
	return &im, nil
}

// HasGroups says whether content defines groups at all, usable or not.
func HasGroups(data []byte) bool {
	var im struct {
		Groups []any `yaml:"proxy-groups"`
	}
	return yaml.Unmarshal(data, &im) == nil && len(im.Groups) > 0
}

func nameOf(m map[string]any) string {
	if v, ok := m["name"]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func stringsOf(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, fmt.Sprint(e))
	}
	return out
}

func on(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}

// check keeps only what the routing depends on and refuses what could not
// work, each with a reason a person can act on: the core would say much the
// same, but later and in its own words.
func (im *Imported) check() error {
	if len(im.Rules) == 0 {
		return errors.New(i18n.T("这份配置有路由组，但没有规则", "This config has route groups but no rules"))
	}
	known := map[string]bool{}
	for _, n := range builtinTargets {
		known[n] = true
	}
	for _, list := range [][]map[string]any{im.Proxies, im.Groups} {
		for _, m := range list {
			n := nameOf(m)
			if n == "" {
				return errors.New(i18n.T("这份配置里有没有名字的节点或路由组", "This config has a node or route group without a name"))
			}
			if n == ProbeGroup {
				return fmt.Errorf(i18n.T("「%s」这个名字是 bowel 自己用的", "The name %s is bowel's own"), n)
			}
			known[n] = true
		}
	}

	usedProviders := map[string]bool{}
	for _, g := range im.Groups {
		name := nameOf(g)
		members, uses := stringsOf(g["proxies"]), stringsOf(g["use"])
		allProxies := on(g, "include-all") || on(g, "include-all-proxies")
		allProviders := on(g, "include-all") || on(g, "include-all-providers")
		if len(members) == 0 && len(uses) == 0 && !allProxies && !allProviders {
			return fmt.Errorf(i18n.T("路由组「%s」里没有成员", "Route group %s has no members"), name)
		}
		for _, m := range members {
			if !known[m] {
				return fmt.Errorf(i18n.T("路由组「%s」引用的「%s」不存在", "Route group %s refers to %s, which isn't there"), name, m)
			}
		}
		for _, u := range uses {
			if _, ok := im.ProxyProviders[u]; !ok {
				return fmt.Errorf(i18n.T("路由组「%s」引用的节点来源「%s」不存在", "Route group %s refers to the provider %s, which isn't there"), name, u)
			}
			usedProviders[u] = true
		}
		if allProviders {
			for u := range im.ProxyProviders {
				usedProviders[u] = true
			}
		}
		for _, key := range []string{"filter", "exclude-filter"} {
			if _, err := filtersOf(g, key); err != nil {
				return fmt.Errorf(i18n.T("路由组「%s」的 %s 不是有效的正则表达式", "The %[2]s of route group %[1]s is not a valid regular expression"), name, key)
			}
		}
	}

	usedSets := map[string]bool{}
	checkRules := func(rules []string) error {
		for _, line := range rules {
			tp, payload, target := ruleParts(line)
			switch {
			case tp == "RULE-SET":
				if _, ok := im.RuleProviders[payload]; !ok {
					return fmt.Errorf(i18n.T("规则「%s」引用的规则集不存在", "The rule %s refers to a rule set that isn't there"), line)
				}
				usedSets[payload] = true
			case tp == "SUB-RULE":
				if _, ok := im.SubRules[target]; !ok {
					return fmt.Errorf(i18n.T("规则「%s」引用的子规则不存在", "The rule %s refers to a sub-rule that isn't there"), line)
				}
				continue
			}
			if target == "" || !known[target] {
				return fmt.Errorf(i18n.T("规则「%s」的目标不存在", "The target of rule %s isn't there"), line)
			}
		}
		return nil
	}
	if err := checkRules(im.Rules); err != nil {
		return err
	}
	for _, rules := range im.SubRules {
		if err := checkRules(rules); err != nil {
			return err
		}
	}

	var err error
	if im.RuleProviders, err = usableProviders(im.RuleProviders, usedSets, known); err != nil {
		return err
	}
	im.ProxyProviders, err = usableProviders(im.ProxyProviders, usedProviders, known)
	return err
}

// usableProviders are the used ones of providers, as the core may read
// them. A remote one is the core's to download, to a place of its own
// choosing rather than one the source names. A local file can't come along:
// its path means something only on the machine the config was written on.
func usableProviders(all map[string]map[string]any, used map[string]bool, known map[string]bool) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	for name, p := range all {
		if !used[name] {
			continue
		}
		q := maps.Clone(p)
		switch kind := strings.ToLower(fmt.Sprint(p["type"])); kind {
		case "http":
			delete(q, "path")
			delete(q, "path-in-bundle")
		case "inline":
		case "file":
			return nil, fmt.Errorf(i18n.T("「%s」指向本地文件，这台机器上读不到，请改成链接或内联内容", "%s points at a local file, which can't be read here. Use a link or inline content"), name)
		default:
			return nil, fmt.Errorf(i18n.T("「%s」的类型 %s 不受支持", "%s has a type that isn't supported: %s"), name, kind)
		}
		if via, ok := p["proxy"]; ok && !known[fmt.Sprint(via)] {
			return nil, fmt.Errorf(i18n.T("「%s」下载时要用的「%v」不存在", "%s is to be downloaded through %v, which isn't there"), name, via)
		}
		out[name] = q
	}
	return out, nil
}

// ruleParts splits a rule the way the core does (RC.ParseRulePayload).
func ruleParts(line string) (tp, payload, target string) {
	item := strings.Split(line, ",")
	for i := range item {
		item[i] = strings.TrimSpace(item[i])
	}
	tp = strings.ToUpper(item[0])
	if len(item) < 2 {
		return tp, "", ""
	}
	switch tp {
	case "MATCH":
		target = item[1]
	case "NOT", "OR", "AND", "SUB-RULE", "DOMAIN-REGEX", "PROCESS-NAME-REGEX", "PROCESS-PATH-REGEX":
		target = item[len(item)-1]
		payload = strings.Join(item[1:len(item)-1], ",")
	default:
		payload = item[1]
		if len(item) > 2 {
			target = item[2]
		}
	}
	return tp, payload, target
}

func filtersOf(g map[string]any, key string) ([]*regexp2.Regexp, error) {
	s, _ := g[key].(string)
	if s == "" {
		return nil, nil
	}
	var out []*regexp2.Regexp
	for _, f := range strings.Split(s, "`") {
		re, err := regexp2.Compile(f, regexp2.None)
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}

// Match is the target of the rule that takes what no other rule matched, ""
// when the source has none: the core then lets that traffic out directly.
func (im *Imported) Match() string {
	for _, line := range slices.Backward(im.Rules) {
		if tp, _, target := ruleParts(line); tp == "MATCH" {
			return target
		}
	}
	return ""
}

// Targets are what the rules send traffic to, sub-rules included, each once
// and in the order the rules first name them. MATCH's is not left out.
func (im *Imported) Targets() []string {
	var out []string
	seen := map[string]bool{}
	var walk func(rules []string, depth int)
	walk = func(rules []string, depth int) {
		for _, line := range rules {
			tp, _, target := ruleParts(line)
			if tp == "SUB-RULE" {
				if depth < 8 {
					walk(im.SubRules[target], depth+1)
				}
				continue
			}
			if target != "" && !seen[target] {
				seen[target] = true
				out = append(out, target)
			}
		}
	}
	walk(im.Rules, 0)
	return out
}

// RulesTo is how many of the rules, sub-rules included, send traffic to target.
func (im *Imported) RulesTo(target string) int {
	n := 0
	count := func(rules []string) {
		for _, line := range rules {
			if _, _, t := ruleParts(line); t == target {
				n++
			}
		}
	}
	count(im.Rules)
	for _, rules := range im.SubRules {
		count(rules)
	}
	return n
}

// GroupOf is the group called name.
func (im *Imported) GroupOf(name string) (map[string]any, bool) {
	i := slices.IndexFunc(im.Groups, func(g map[string]any) bool { return nameOf(g) == name })
	if i < 0 {
		return nil, false
	}
	return im.Groups[i], true
}

// GroupName is a group's name, GroupKind its type in the core's own words.
func GroupName(g map[string]any) string { return nameOf(g) }
func GroupKind(g map[string]any) string { return strings.ToLower(fmt.Sprint(g["type"])) }

// ProviderNames are the source's own node providers that are in use.
func (im *Imported) ProviderNames() []string {
	return slices.Sorted(maps.Keys(im.ProxyProviders))
}

// runtimeGroups are the groups as they go to the core. `include-all` and its
// kin mean every proxy and provider of the config, which would take in the
// nodes of Moho's own providers and the developer networks' outbounds; so
// they become the same lists, of the source's own. A group that is left
// empty falls back as the core would have made it.
func (im *Imported) runtimeGroups() ([]any, error) {
	own := make([]string, 0, len(im.Proxies))
	for _, p := range im.Proxies {
		own = append(own, nameOf(p))
	}
	providers := im.ProviderNames()
	groups := make([]any, 0, len(im.Groups)+1)
	for _, g := range im.Groups {
		out := maps.Clone(g)
		allProxies := on(g, "include-all") || on(g, "include-all-proxies")
		allProviders := on(g, "include-all") || on(g, "include-all-providers")
		for _, k := range []string{"include-all", "include-all-proxies", "include-all-providers"} {
			delete(out, k)
		}
		if allProviders {
			out["use"] = providers
		}
		if allProxies || allProviders {
			members := stringsOf(g["proxies"])
			if allProxies {
				filters, err := filtersOf(g, "filter")
				if err != nil {
					return nil, err
				}
				for _, n := range own {
					if !slices.Contains(members, n) && matchesAny(filters, n) {
						members = append(members, n)
					}
				}
			}
			if len(members) == 0 && !(allProviders && len(providers) > 0) {
				fallback, _ := g["empty-fallback"].(string)
				if fallback == "" {
					fallback = "COMPATIBLE"
				}
				members = []string{fallback}
			}
			if len(members) > 0 {
				out["proxies"] = members
			} else {
				delete(out, "proxies")
			}
		}
		groups = append(groups, out)
	}
	return groups, nil
}

func matchesAny(filters []*regexp2.Regexp, name string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, re := range filters {
		if ok, _ := re.MatchString(name); ok {
			return true
		}
	}
	return false
}

// importedRules are the source's rules behind the developer networks' own.
// Outside rule mode the list is reduced as rulesFor does: the way to this
// machine's own networks stays and the rest goes as the mode says.
func importedRules(mode string, dev devnet.Overlay, source []string) []string {
	var own []string
	for _, r := range dev.Rules {
		own = append(own, withTarget(r.Text, r.Target))
	}
	switch {
	case dev.Empty() || mode == "rule":
		return slices.Concat(own, source)
	case mode == "global":
		return append(own, "MATCH,GLOBAL")
	}
	return append(own, "MATCH,DIRECT")
}
