// Package ruleset keeps the rule lists people import from a link or a file.
// bowel downloads and checks them itself and hands the core a clean
// `type: file` rule-provider, so that a list is seen before it counts, and
// 测试一个地址 can look inside it, which the core offers no way to do.
package ruleset

import (
	"bytes"
	"errors"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/routes"
)

// The behaviors of a mihomo rule-provider.
const (
	Classical = "classical" // whole rules: DOMAIN-SUFFIX,x.com
	Domain    = "domain"    // names: x.com, +.x.com
	IPCIDR    = "ipcidr"    // addresses: 10.0.0.0/8
)

// Set is a rule list in the form the core reads.
type Set struct {
	Behavior string
	Rules    []string
	Skipped  int // lines the core could not use, left out
}

// supported are the rule types mihomo takes in a classical rule-provider
// (rules/parser.go; MATCH, RULE-SET and SUB-RULE are refused there).
var supported = map[string]bool{}

func init() {
	for _, t := range strings.Fields(`DOMAIN DOMAIN-SUFFIX DOMAIN-KEYWORD DOMAIN-REGEX DOMAIN-WILDCARD
		GEOSITE GEOIP SRC-GEOIP IP-ASN SRC-IP-ASN IP-CIDR IP-CIDR6 SRC-IP-CIDR IP-SUFFIX SRC-IP-SUFFIX
		SRC-PORT DST-PORT IN-PORT DSCP PROCESS-NAME PROCESS-PATH PROCESS-NAME-REGEX PROCESS-PATH-REGEX
		PROCESS-NAME-WILDCARD PROCESS-PATH-WILDCARD NETWORK UID IN-TYPE IN-USER IN-NAME AND OR NOT`) {
		supported[t] = true
	}
}

// commaPayload are the types whose payload may hold commas, so are left as
// they are written.
var commaPayload = map[string]bool{"AND": true, "OR": true, "NOT": true,
	"DOMAIN-REGEX": true, "PROCESS-NAME-REGEX": true, "PROCESS-PATH-REGEX": true}

var typed = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*,`)

// Parse reads a mihomo YAML rule-provider or a text list (Clash classical,
// Surge). A line the core could not use is left out and counted, and only a
// list with nothing usable is an error.
func Parse(data []byte) (Set, error) {
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return Set{}, errors.New(i18n.T("暂不支持 .mrs 二进制格式，请用 .yaml 或 .list 版本", "Binary .mrs lists aren't supported yet. Use the .yaml or .list version"))
	}
	lines, isYAML := payload(data)
	var classical, domains, cidrs []string
	skipped := 0
	for _, s := range lines {
		s = strings.TrimSpace(s)
		switch {
		case s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, ";"):
		case typed.MatchString(s):
			if r, ok := classicalRule(s); ok {
				classical = append(classical, r)
			} else {
				skipped++
			}
		default:
			if c, ok := cidrOf(s); ok {
				cidrs = append(cidrs, c)
			} else if d, ok := domainOf(s, isYAML); ok {
				domains = append(domains, d)
			} else {
				skipped++
			}
		}
	}
	set := Set{Behavior: Domain, Rules: domains, Skipped: skipped}
	switch {
	case len(classical) > 0:
		set = Set{Behavior: Classical, Rules: classical, Skipped: skipped + len(domains) + len(cidrs)}
	case len(cidrs) > len(domains):
		set = Set{Behavior: IPCIDR, Rules: cidrs, Skipped: skipped + len(domains)}
	default:
		set.Skipped += len(cidrs)
	}
	if len(set.Rules) == 0 {
		return Set{}, errors.New(i18n.T("没有找到可用的规则，请检查这是不是规则集", "No usable rules found. Check that this is a rule list"))
	}
	return set, nil
}

// payload is the entries of a YAML rule-provider, or else the file's lines.
func payload(data []byte) (lines []string, isYAML bool) {
	var y struct {
		Payload []any `yaml:"payload"`
	}
	if yaml.Unmarshal(data, &y) == nil && len(y.Payload) > 0 {
		for _, v := range y.Payload {
			if s, ok := v.(string); ok {
				lines = append(lines, s)
			} else {
				lines = append(lines, "") // not a rule; a number, say
			}
		}
		return lines, true
	}
	return strings.Split(string(data), "\n"), false
}

// classicalRule is a rule in the form the core reads: its type in capitals,
// and no target, which belongs to whoever imports the list.
func classicalRule(s string) (string, bool) {
	kind, rest, _ := strings.Cut(s, ",")
	kind = strings.ToUpper(strings.TrimSpace(kind))
	if !supported[kind] {
		return "", false
	}
	if commaPayload[kind] {
		return kind + "," + strings.TrimSpace(rest), strings.TrimSpace(rest) != ""
	}
	fields := strings.Split(rest, ",")
	for i := range fields {
		fields[i] = strings.TrimSpace(fields[i])
	}
	value := fields[0]
	if value == "" {
		return "", false
	}
	switch kind {
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "DOMAIN-WILDCARD":
		value = strings.ToLower(value)
	case "IP-CIDR", "IP-CIDR6", "SRC-IP-CIDR":
		if _, err := netip.ParsePrefix(value); err != nil {
			return "", false
		}
	}
	r := kind + "," + value
	if slices.Contains(fields[1:], "no-resolve") {
		r += ",no-resolve"
	}
	return r, true
}

func cidrOf(s string) (string, bool) {
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked().String(), true
	}
	if a, err := netip.ParseAddr(s); err == nil {
		a = a.Unmap()
		return netip.PrefixFrom(a, a.BitLen()).String(), true
	}
	return "", false
}

var domainLabels = regexp.MustCompile(`^[a-z0-9_*-]+(\.[a-z0-9_*-]+)+$`)

// domainOf is a line of a name list. In a text list ".x.com" is Surge's way
// of saying x.com and what is under it, which mihomo writes "+.x.com"; in
// mihomo's own YAML it stays what it means there.
func domainOf(s string, isYAML bool) (string, bool) {
	s = strings.ToLower(s)
	bare := strings.TrimPrefix(strings.TrimPrefix(s, "+"), ".")
	if !domainLabels.MatchString(bare) {
		return "", false
	}
	if !isYAML && strings.HasPrefix(s, ".") {
		s = "+" + s
	}
	return s, true
}

// matcher answers where a set would catch a connection, for the rules that
// can be told from a host and an address. What takes a process or GeoIP data
// isn't in it.
type matcher struct {
	exact, suffix map[string]string
	keywords      [][2]string // value, rule
	nets          []netRule
	preview       []routes.Rule
}

type netRule struct {
	prefix netip.Prefix
	text   string
}

// previewSize is how many rules of a set are ever shown; the rest are the
// source's business.
const previewSize = 5

func newMatcher(s Set) *matcher {
	m := &matcher{exact: map[string]string{}, suffix: map[string]string{}}
	show := func(text, kind, value string) {
		if len(m.preview) < previewSize {
			m.preview = append(m.preview, routes.Rule{Text: text, Kind: kind, Value: value})
		}
	}
	addNet := func(value, text string) {
		if p, err := netip.ParsePrefix(value); err == nil {
			m.nets = append(m.nets, netRule{p.Masked(), text})
			show(text, routes.KindIP, value)
		}
	}
	for _, r := range s.Rules {
		switch s.Behavior {
		case IPCIDR:
			addNet(r, r)
		case Domain:
			switch {
			case strings.Contains(strings.TrimPrefix(r, "+."), "*"):
			case strings.HasPrefix(r, "+.") || strings.HasPrefix(r, "."):
				v := strings.TrimPrefix(strings.TrimPrefix(r, "+"), ".")
				m.suffix[v] = r
				show(r, routes.KindSuffix, v)
			default:
				m.exact[r] = r
				show(r, routes.KindDomain, r)
			}
		default:
			kind, rest, _ := strings.Cut(r, ",")
			value, _, _ := strings.Cut(rest, ",")
			switch kind {
			case "DOMAIN":
				m.exact[value] = r
				show(r, routes.KindDomain, value)
			case "DOMAIN-SUFFIX":
				m.suffix[value] = r
				show(r, routes.KindSuffix, value)
			case "DOMAIN-KEYWORD":
				m.keywords = append(m.keywords, [2]string{value, r})
				show(r, routes.KindKeyword, value)
			case "IP-CIDR", "IP-CIDR6":
				addNet(value, r)
			}
		}
	}
	return m
}

func (m *matcher) match(host, ip string) (string, bool) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host != "" {
		if r, ok := m.exact[host]; ok {
			return r, true
		}
		for h := host; ; {
			if r, ok := m.suffix[h]; ok {
				return r, true
			}
			_, rest, more := strings.Cut(h, ".")
			if !more {
				break
			}
			h = rest
		}
		for _, k := range m.keywords {
			if strings.Contains(host, k[0]) {
				return k[1], true
			}
		}
	}
	if a, err := netip.ParseAddr(ip); err == nil {
		a = a.Unmap()
		for _, n := range m.nets {
			if n.prefix.Contains(a) {
				return n.text, true
			}
		}
	}
	return "", false
}
