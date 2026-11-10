package routes

import (
	"strings"

	"github.com/oboo/moho/internal/i18n"
)

// A builtin is a service Moho ships: a name, the route group it starts in
// and its rules. Rules are written short: a domain means it and what is
// under it, geo:x is a GeoSite list and geoip:X a GeoIP one.
type builtin struct {
	id, name, group string
	rules           []string
}

func sv(id, name, group, rules string) builtin {
	b := builtin{id: id, name: name, group: group}
	for _, f := range strings.Fields(rules) {
		switch {
		case strings.HasPrefix(f, "geo:"):
			b.rules = append(b.rules, "GEOSITE,"+f[len("geo:"):])
		case strings.HasPrefix(f, "geoip:"):
			b.rules = append(b.rules, "GEOIP,"+f[len("geoip:"):]+",no-resolve")
		default:
			b.rules = append(b.rules, "DOMAIN-SUFFIX,"+f)
		}
	}
	return b
}

var builtins = []builtin{
	sv("openai", "OpenAI", "ai", "openai.com chatgpt.com oaistatic.com oaiusercontent.com sora.com"),
	sv("claude", "Claude", "ai", "anthropic.com claude.ai claude.com claudeusercontent.com"),
	sv("gemini", "Gemini", "ai", "gemini.google.com generativelanguage.googleapis.com aistudio.google.com notebooklm.google.com bard.google.com"),
	sv("perplexity", "Perplexity", "ai", "perplexity.ai"),
	sv("grok", "Grok", "ai", "x.ai grok.com"),
	sv("cursor", "Cursor", "ai", "cursor.com cursor.sh cursorapi.com"),
	sv("poe", "Poe", "ai", "poe.com"),
	sv("mistral", "Mistral", "ai", "mistral.ai"),
	sv("hf", "Hugging Face", "ai", "huggingface.co"),
	sv("openrouter", "OpenRouter", "ai", "openrouter.ai"),
	sv("midjourney", "Midjourney", "ai", "midjourney.com"),
	sv("characterai", "Character.AI", "ai", "character.ai"),

	sv("youtube", "YouTube", "media", "geo:youtube"),
	sv("netflix", "Netflix", "media", "geo:netflix netflix.com nflxvideo.net"),
	sv("spotify", "Spotify", "media", "geo:spotify"),
	sv("disney", "Disney+", "media", "geo:disney"),
	sv("hbo", "HBO", "media", "geo:hbo"),
	sv("prime", "Prime Video", "media", "geo:primevideo"),
	sv("twitch", "Twitch", "media", "geo:twitch"),
	sv("tiktok", "TikTok", "media", "geo:tiktok"),
	sv("bahamut", "巴哈姆特", "media", "geo:bahamut"),
	sv("abema", "AbemaTV", "media", "geo:abema"),

	sv("github", "GitHub", "dev", "github.com githubusercontent.com githubassets.com github.io ghcr.io"),
	sv("npm", "npm", "dev", "npmjs.org npmjs.com yarnpkg.com"),
	sv("docker", "Docker", "dev", "docker.io docker.com"),
	sv("pypi", "PyPI", "dev", "pypi.org pythonhosted.org"),
	sv("golang", "Go", "dev", "golang.org go.dev proxy.golang.org"),
	sv("rust", "Rust", "dev", "crates.io rust-lang.org"),
	sv("gitlab", "GitLab", "dev", "gitlab.com"),
	sv("bitbucket", "Bitbucket", "dev", "bitbucket.org"),
	sv("so", "Stack Overflow", "dev", "stackoverflow.com"),
	sv("cdn", "前端 CDN", "dev", "jsdelivr.net unpkg.com"),
	sv("vercel", "Vercel", "dev", "vercel.com"),
	sv("brew", "Homebrew", "dev", "brew.sh"),

	sv("lan", "局域网", "direct", "geo:private geoip:private"),
	sv("cn", "国内网站", "direct", "geo:cn geoip:CN"),
}

var serviceEn = map[string]string{"bahamut": "Bahamut", "cdn": "Web CDNs", "lan": "Local network", "cn": "Mainland sites"}

// builtinRules are the rules of the built-in services of group id, as
// mihomo writes them.
func builtinRules(id string) []string {
	var out []string
	for _, b := range builtins {
		if b.group == id {
			out = append(out, b.rules...)
		}
	}
	return out
}

// Rule is one line of a service.
type Rule struct {
	Text    string // as mihomo writes it, without its target
	Kind    string // one of Kinds, KindGeo or KindGeoIP
	Value   string
	Builtin bool // Moho's: it can be switched off, not deleted
	Off     bool
}

// Shown is the rule as people read it: *.example.com for a suffix.
func (r Rule) Shown() string {
	if r.Kind == KindSuffix {
		return "*." + r.Value
	}
	return r.Value
}

// User is a rule of the user's as it is stored.
func (r Rule) User(s Service) UserRule {
	u := UserRule{Kind: r.Kind, Value: r.Value, Route: s.Group}
	if !s.Mine {
		u.Service = s.ID
	}
	return u
}

// ruleOf reads a line of mihomo's back into a Rule.
func ruleOf(text string) Rule {
	kind, rest, _ := strings.Cut(text, ",")
	value, _, _ := strings.Cut(rest, ",")
	r := Rule{Text: text, Value: value}
	switch kind {
	case "DOMAIN":
		r.Kind = KindDomain
	case "DOMAIN-SUFFIX":
		r.Kind = KindSuffix
	case "DOMAIN-KEYWORD":
		r.Kind = KindKeyword
	case "IP-CIDR", "IP-CIDR6":
		r.Kind = KindIP
	case "GEOSITE":
		r.Kind = KindGeo
	case "GEOIP":
		r.Kind = KindGeoIP
	}
	return r
}

// Service is what one site or product needs: the rules that recognise it.
// It lives in one route group; services do not exist for the core, which
// only sees their rules.
type Service struct {
	ID      string
	Name    string
	Group   string // the route group's id
	Builtin bool
	Mine    bool // the group's 我添加的: where rules made from Log land
	Off     bool
	Changed bool // a built-in that differs from what Moho ships
	Rules   []Rule
	Source  string // a rule set's link or file; its rules aren't in Rules
}

// MineID is the id of group's 我添加的 service.
func MineID(group string) string { return "mine:" + group }

// ServiceTitle is a service's name in the interface's language.
func ServiceTitle(s Service) string {
	if s.Mine {
		return i18n.T("我添加的", "Added by me")
	}
	if en, ok := serviceEn[s.ID]; ok && s.Builtin {
		return i18n.T(s.Name, en)
	}
	return s.Name
}

// Live are the rules of s that count now.
func (s Service) Live() []Rule {
	if s.Off {
		return nil
	}
	var out []Rule
	for _, r := range s.Rules {
		if !r.Off {
			out = append(out, r)
		}
	}
	return out
}
