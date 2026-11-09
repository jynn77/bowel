// Package settings keeps the app's preferences in a JSON file.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/routes"
)

type Settings struct {
	// Language is the interface's: zh, zh-Hant, en or ja (i18n.Languages).
	Language string `json:"language"`
	// Accent is the interface's theme color: "#RRGGBB", or "ink" for none,
	// black on white and white on black.
	Accent string `json:"accent"`
	// Appearance is light or dark, or "system" to follow the Mac.
	Appearance string `json:"appearance"`
	// Borders is how the home board draws its lines: "straight", or
	// "twisted", bending as the logo's line does.
	Borders string `json:"borders"`
	// Skin is the interface's look as a whole: its paper and ink, faces,
	// corners and texture (Skins). "" is the first, paper.
	Skin string `json:"skin"`

	// Takeover: which entries the user wants. Whether they are actually in
	// effect is the status model's business, not stored here.
	SystemProxy bool `json:"systemProxy"`
	Tun         bool `json:"tun"`
	// Paused turns takeover off without forgetting the entries above.
	Paused bool `json:"paused"`
	// Gateway: this Mac is the LAN's gateway and DNS, through TUN. Like
	// the entries, it waits while takeover is paused or TUN is off.
	Gateway bool `json:"gateway"`

	Mode      string   `json:"mode"` // rule, global, direct
	MixedPort int      `json:"mixedPort"`
	AllowLan  bool     `json:"allowLan"`
	Bypass    []string `json:"bypass"`
	LogLevel  string   `json:"logLevel"`
	TestURL   string   `json:"testUrl"`
	// LocateURL is what a probe asks, through a node, where the node exits:
	// it must answer with the address and the country it sees.
	LocateURL string `json:"locateUrl"`
	// IPQuality has the exit's address looked up: whether it belongs to a
	// data centre or a home line, and how clean it looks. Off unless asked,
	// since it asks a service outside about every exit.
	IPQuality bool `json:"ipQuality"`
	// IPInfoURL is the service that answers that, with {ip} where the
	// address goes. Empty asks ipinfo.DefaultSource.
	IPInfoURL string `json:"ipInfoUrl"`

	// RouteTargets are the sites a route is tested against, where the user
	// changed them from its defaults (TargetsOf).
	RouteTargets map[string][]routes.Target `json:"routeTargets,omitempty"`

	// Rules are the user's own, the newest first.
	Rules []routes.UserRule `json:"rules"`
	// Routing is what the user changed about route groups and services.
	Routing routes.Plan `json:"routing,omitzero"`

	// StartHidden: opened at login, Moho stays in the menu bar.
	StartHidden bool `json:"startHidden"`

	// MCPPolicy is what happens when an agent asks, over MCP, to change
	// something: "ask" the user each time, "allow" it, or "deny" it.
	// Reading is always allowed. "" is ask.
	MCPPolicy string `json:"mcpPolicy"`

	// The core's options, the 设置 window's; their keys sit at this level.
	core.Options
}

// Appearances are the choices of Appearance.
var Appearances = []string{"system", "light", "dark"}

// Skins are the choices of Skin; the interface (skins.go) draws each.
var Skins = []string{"paper", "broadsheet", "phosphor", "neon", "blueprint", "sakura"}

// BorderStyles are the choices of Borders.
var BorderStyles = []string{"straight", "twisted"}

// MCPPolicies are the choices of MCPPolicy.
var MCPPolicies = []string{"ask", "allow", "deny"}

// Policy is MCPPolicy, with "" as ask.
func (s Settings) Policy() string {
	if s.MCPPolicy == "" {
		return "ask"
	}
	return s.MCPPolicy
}

// LogLevels are the core's log levels.
var LogLevels = []string{"debug", "info", "warning", "error", "silent"}

var Defaults = Settings{
	Language:    "zh",
	Accent:      "#DDFB4C",
	Appearance:  "system",
	Borders:     "straight",
	Skin:        "paper",
	MCPPolicy:   "ask",
	SystemProxy: true,
	Paused:      true,
	Mode:        "rule",
	MixedPort:   7899,
	Bypass:      []string{"localhost", "127.0.0.1", "*.local", "169.254/16", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "timestamp.apple.com"},
	LogLevel:    "info",
	TestURL:     "https://www.gstatic.com/generate_204",
	LocateURL:   "https://www.cloudflare.com/cdn-cgi/trace",
	Options:     core.DefaultOptions,
}

// Target is one address the 测速 setting offers by name: a URL that answers
// 204 at once is what a latency test wants, and one at home is what a node's
// reach into this country is judged by.
type Target struct {
	Name string
	URL  string
}

// TestURLs are the addresses worth naming. Any other http(s) address works
// too: these are only the ones people ask for.
var TestURLs = []Target{
	{"Google 204", "https://www.gstatic.com/generate_204"},
	{"Cloudflare 204", "https://cp.cloudflare.com/generate_204"},
	{"GitHub", "https://github.com"},
	{"百度", "https://www.baidu.com"},
}

// IPInfoURLs are the address services the 出口 IP 质量检测 setting offers by
// name: each answers about an address with its network's name and, where it
// says, whether that network is a hosting provider's. {ip} is where the
// address goes; a URL without it gets the address appended.
var IPInfoURLs = []Target{
	{"ip-api.com", "http://ip-api.com/json/{ip}?fields=status,countryCode,isp,org,as,hosting,proxy,mobile,query"},
	{"ipwho.is", "https://ipwho.is/{ip}"},
	{"ip.sb", "https://api.ip.sb/geoip/{ip}"},
	{"ipapi.co", "https://ipapi.co/{ip}/json/"},
}

type Store struct {
	path string
	mu   sync.Mutex
	s    Settings
	gen  uint64 // counts changes
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// A clone: decoding reuses the lists' arrays, which must not be Defaults'.
	st := &Store{path: filepath.Join(dir, "settings.json"), s: Defaults.Clone()}
	if b, err := os.ReadFile(st.path); err == nil {
		_ = json.Unmarshal(b, &st.s)
	}
	if st.s.Language == "" {
		st.s.Language = Defaults.Language
	}
	if st.s.Accent == "" {
		st.s.Accent = Defaults.Accent
	}
	if st.s.Appearance == "" {
		st.s.Appearance = Defaults.Appearance
	}
	if st.s.Borders == "" {
		st.s.Borders = Defaults.Borders
	}
	if st.s.Skin == "" {
		st.s.Skin = Defaults.Skin
	}
	if st.s.MixedPort == 0 {
		st.s.MixedPort = Defaults.MixedPort
	}
	if st.s.Mode == "" {
		st.s.Mode = Defaults.Mode
	}
	if st.s.TestURL == "" {
		st.s.TestURL = Defaults.TestURL
	}
	if st.s.LocateURL == "" {
		st.s.LocateURL = Defaults.LocateURL
	}
	if st.s.LogLevel == "" {
		st.s.LogLevel = Defaults.LogLevel
	}
	st.s.Options.Fill()
	return st, nil
}

// Gen counts the changes so far.
func (st *Store) Gen() uint64 {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.gen
}

func (st *Store) Get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Clone()
}

// Clone is a copy that shares no lists with s.
func (s Settings) Clone() Settings {
	s.Bypass = slices.Clone(s.Bypass)
	s.Rules = slices.Clone(s.Rules)
	s.Routing = s.Routing.Clone()
	s.Options = s.Options.Clone()
	if s.RouteTargets != nil {
		m := make(map[string][]routes.Target, len(s.RouteTargets))
		for id, t := range s.RouteTargets {
			m[id] = slices.Clone(t)
		}
		s.RouteTargets = m
	}
	return s
}

// TargetsOf is what route id is tested against, its main target first: the
// user's list, or the route's defaults. The default route's main target is
// the test URL of the settings, and so is that of a group the user made.
func (s Settings) TargetsOf(id string) []routes.Target {
	if t := s.RouteTargets[id]; len(t) > 0 {
		return slices.Clone(t)
	}
	t := routes.DefaultTargets(id)
	if id == routes.Default || len(t) == 0 && id != "direct" && id != routes.Reject { // a group the user made has only this
		if main, err := routes.ParseTarget(s.TestURL); err == nil {
			t = append([]routes.Target{main}, t...)
		} else if id != routes.Default {
			t = routes.DefaultTargets(routes.Default)
		}
	}
	return t
}

// Update changes the settings with fn and saves them.
func (st *Store) Update(fn func(*Settings)) (Settings, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.s)
	st.gen++
	b, _ := json.MarshalIndent(st.s, "", "  ")
	return st.s.Clone(), os.WriteFile(st.path, b, 0o600)
}

// ResetCore puts the core's options back to Moho's defaults. What decides
// who reaches Moho stays: the takeover, the mode, the ports, the LAN's
// access and the dashboards' controller; so does the language.
func (s *Settings) ResetCore() {
	d := Defaults.Clone()
	keep := s.Options
	s.LogLevel, s.TestURL, s.LocateURL, s.Bypass, s.Options = d.LogLevel, d.TestURL, d.LocateURL, d.Bypass, d.Options
	s.RouteTargets = nil
	s.HTTPPort, s.SocksPort, s.Authentication, s.LanAllowedIPs = keep.HTTPPort, keep.SocksPort, keep.Authentication, keep.LanAllowedIPs
	s.ExtController, s.ExtSecret = keep.ExtController, keep.ExtSecret
}

// Validate says what is wrong with s, in words for the interface.
func (s Settings) Validate() error {
	if !i18n.Known(s.Language) {
		return fmt.Errorf(i18n.T("未知语言: %s", "Unknown language: %s"), s.Language)
	}
	if !ValidAccent(s.Accent) {
		return fmt.Errorf(i18n.T("主题色要写成 #RRGGBB: %s", "The theme color must be #RRGGBB: %s"), s.Accent)
	}
	if !slices.Contains(Appearances, s.Appearance) {
		return fmt.Errorf(i18n.T("未知外观: %s", "Unknown appearance: %s"), s.Appearance)
	}
	if s.Skin != "" && !slices.Contains(Skins, s.Skin) { // "" is paper
		return fmt.Errorf(i18n.T("未知皮肤: %s", "Unknown skin: %s"), s.Skin)
	}
	if s.Borders != "" && !slices.Contains(BorderStyles, s.Borders) { // "" is straight
		return fmt.Errorf(i18n.T("未知边框样式: %s", "Unknown border style: %s"), s.Borders)
	}
	if s.MCPPolicy != "" && !slices.Contains(MCPPolicies, s.MCPPolicy) { // "" is ask
		return fmt.Errorf(i18n.T("未知的 agent 变更策略: %s", "Unknown agent change policy: %s"), s.MCPPolicy)
	}
	if s.MixedPort < 1 || s.MixedPort > 65535 {
		return errors.New(i18n.T("端口要在 1 到 65535 之间", "The port must be between 1 and 65535"))
	}
	if !slices.Contains(LogLevels, s.LogLevel) {
		return fmt.Errorf(i18n.T("未知日志级别: %s", "Unknown log level: %s"), s.LogLevel)
	}
	if u, err := url.Parse(s.TestURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New(i18n.T("测速地址要是 http 或 https 链接", "The test URL must be an http or https link"))
	}
	if s.LocateURL != "" { // empty asks the default
		if u, err := url.Parse(s.LocateURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New(i18n.T("出口测试地址要是 http 或 https 链接", "The exit lookup URL must be an http or https link"))
		}
	}
	for _, b := range s.Bypass {
		if strings.ContainsAny(b, " \t,;") {
			return fmt.Errorf(i18n.T("不走代理的写法不对: %s", "Malformed bypass entry: %s"), b)
		}
	}
	// Every listener on a port of its own.
	ports := map[int]bool{s.MixedPort: true}
	for _, p := range []int{s.HTTPPort, s.SocksPort, s.ExtPort()} {
		if p > 0 && ports[p] {
			return fmt.Errorf(i18n.T("端口 %d 用了两次", "Port %d is used twice"), p)
		}
		ports[p] = true
	}
	if err := s.Routing.Check(); err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, r := range routes.Build(s.Routing, nil) {
		ids[r.ID] = true
	}
	for i, r := range s.Rules {
		if err := r.Valid(); err != nil {
			return err
		}
		if !ids[r.Route] {
			return fmt.Errorf(i18n.T("规则 %s 指向不存在的路由", "Rule %s points at a route that doesn't exist"), r.Value)
		}
		if slices.ContainsFunc(s.Rules[:i], r.Same) {
			return fmt.Errorf(i18n.T("规则 %s 重复了", "Rule %s is there twice"), r.Value)
		}
	}
	for id, list := range s.RouteTargets {
		if !ids[id] {
			return fmt.Errorf(i18n.T("路由 %s 不能设置测速站点", "Route %s can't have test sites"), id)
		}
		if err := routes.ValidTargets(id, list); err != nil {
			return err
		}
	}
	return s.Options.Validate()
}

// SplitList is the entries of a list typed as text: separated by commas,
// spaces or lines, with the empty ones left out.
func SplitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == ';' || unicode.IsSpace(r) })
}

// ValidAccent says whether a is a theme color: "ink", or "#" and six hex
// digits.
func ValidAccent(a string) bool {
	if a == "ink" {
		return true
	}
	if len(a) != 7 || a[0] != '#' {
		return false
	}
	for _, c := range a[1:] {
		if !unicode.Is(unicode.ASCII_Hex_Digit, c) {
			return false
		}
	}
	return true
}
