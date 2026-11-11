package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/oboo/moho/internal/devnet"
	"github.com/oboo/moho/internal/i18n"
)

// ExtraPort is a mixed port on 127.0.0.1 whose traffic all leaves by one
// route group, whatever the rules and the mode say: two programs given
// different ports reach the same site by different exits. Route is the
// route's id, so that renaming the group keeps the port.
type ExtraPort struct {
	Port  int    `json:"port"`
	Route string `json:"route"`
}

// Options are the core's options the 设置 window edits, as Clash clients
// expose them. Settings embeds them, so their JSON keys sit at the top of
// settings.json.
type Options struct {
	// Inbound. HTTPPort and SocksPort are extra listeners besides the
	// mixed port; 0 turns them off.
	HTTPPort  int `json:"httpPort"`
	SocksPort int `json:"socksPort"`
	// Authentication is "user:pass" entries. Connections from this Mac
	// skip it, so it only asks the LAN.
	Authentication []string `json:"authentication"`
	LanAllowedIPs  []string `json:"lanAllowedIps"`
	// ExtraPorts are mixed ports of this machine alone, each for one route
	// group; see ExtraPort.
	ExtraPorts []ExtraPort `json:"extraPorts"`

	// Core.
	IPv6          IPv6Mode `json:"ipv6"`
	UnifiedDelay  bool     `json:"unifiedDelay"`
	TCPConcurrent bool     `json:"tcpConcurrent"`
	FindProcess   string   `json:"findProcess"` // always, strict, off
	// Interface is the network interface outbound traffic uses; "" any.
	Interface string `json:"interface"`
	// KeepAliveInterval is TCP keep-alive's, in seconds.
	KeepAliveInterval int  `json:"keepAliveInterval"`
	DisableKeepAlive  bool `json:"disableKeepAlive"`
	// Fingerprint is the TLS client fingerprint of every node; "" none.
	Fingerprint string `json:"fingerprint"`
	// The automatic groups: how often they test (s) and how much faster
	// (ms) another node must be to switch; HealthInterval, the providers'.
	URLTestInterval  int `json:"urlTestInterval"`
	URLTestTolerance int `json:"urlTestTolerance"`
	HealthInterval   int `json:"healthInterval"`

	// DNS turns the core's resolver on without TUN; TUN always has it.
	DNS                bool     `json:"dns"`
	DNSMode            string   `json:"dnsMode"` // fake-ip, redir-host
	Nameservers        []string `json:"nameservers"`
	DefaultNameservers []string `json:"defaultNameservers"`
	Fallback           []string `json:"fallback"`
	// ProxyNameservers resolve the nodes' own addresses.
	ProxyNameservers []string `json:"proxyNameservers"`
	// NameserverPolicy is "pattern=server" entries; a pattern may repeat.
	NameserverPolicy []string `json:"nameserverPolicy"`
	FakeIPRange      string   `json:"fakeIpRange"`
	FakeIPFilter     []string `json:"fakeIpFilter"`
	// Hosts is "domain=ip" entries.
	Hosts          []string `json:"hosts"`
	RespectRules   bool     `json:"respectRules"`
	UseHosts       bool     `json:"useHosts"`
	UseSystemHosts bool     `json:"useSystemHosts"`
	PreferH3       bool     `json:"preferH3"`

	// TUN.
	TunStack               string   `json:"tunStack"` // mixed, system, gvisor
	StrictRoute            bool     `json:"strictRoute"`
	TunDevice              string   `json:"tunDevice"` // "" the system's choice
	TunMTU                 int      `json:"tunMtu"`
	AutoDetectInterface    bool     `json:"autoDetectInterface"`
	DNSHijack              []string `json:"dnsHijack"`
	EndpointIndependentNAT bool     `json:"endpointIndependentNat"`

	// Sniffer: domains read from the traffic itself.
	Sniff                bool     `json:"sniff"`
	SniffHTTP            []string `json:"sniffHttp"` // ports and ranges
	SniffTLS             []string `json:"sniffTls"`
	SniffQUIC            []string `json:"sniffQuic"`
	SniffOverride        bool     `json:"sniffOverride"`
	SniffForceDNSMapping bool     `json:"sniffForceDnsMapping"`
	SniffParsePureIP     bool     `json:"sniffParsePureIp"`
	SniffSkip            []string `json:"sniffSkip"`

	// Developer networks, see internal/devnet. On unless turned off, as
	// settings saved before they existed have none of this.
	DevOff bool `json:"devNetOff"`
	// DevOffScenarios are the scenarios switched off.
	DevOffScenarios []string `json:"devNetOffScenarios"`
	// DevExtra are the user's own suffixes and networks.
	DevExtra []string `json:"devNetExtra"`

	// GeoData. GeoUpdateInterval is in hours.
	GeoAutoUpdate     bool   `json:"geoAutoUpdate"`
	GeoUpdateInterval int    `json:"geoUpdateInterval"`
	GeoIPURL          string `json:"geoipUrl"`
	GeoSiteURL        string `json:"geositeUrl"`
	MMDBURL           string `json:"mmdbUrl"`

	// ExtController is an extra TCP controller for dashboards (metacubexd,
	// yacd), "" none; ExtSecret its secret.
	ExtController string `json:"extController"`
	ExtSecret     string `json:"extSecret"`
}

// IPv6Mode says when the core uses IPv6. Auto follows what the network can
// do; see devnet.IPv6Watcher.
type IPv6Mode string

const (
	IPv6Off  IPv6Mode = "off"
	IPv6Auto IPv6Mode = "auto"
	IPv6On   IPv6Mode = "on"
)

// Enabled says whether the core gets IPv6, usable being what Auto found.
func (m IPv6Mode) Enabled(usable bool) bool {
	return m == IPv6On || m == IPv6Auto && usable
}

// MarshalJSON writes Off as the false it always was: a version from before
// the modes reads that, and takes the strings for its default, which is on.
func (m IPv6Mode) MarshalJSON() ([]byte, error) {
	if m == IPv6Off {
		return []byte("false"), nil
	}
	return json.Marshal(string(m))
}

// UnmarshalJSON takes the modes and the booleans this setting used to be.
func (m *IPv6Mode) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case "false", `"off"`:
		*m = IPv6Off
	case "true", `"on"`:
		*m = IPv6On
	case `"auto"`:
		*m = IPv6Auto
	case `""`, "null":
	default:
		return fmt.Errorf("ipv6 must be off, auto or on, not %s", b)
	}
	return nil
}

// Choices of the options that take one of a few values.
var (
	IPv6Modes    = []string{string(IPv6Off), string(IPv6Auto), string(IPv6On)}
	DNSModes     = []string{"fake-ip", "redir-host"}
	TunStacks    = []string{"mixed", "system", "gvisor"}
	FindProcess  = []string{"always", "strict", "off"}
	Fingerprints = []string{"", "chrome", "firefox", "safari", "ios", "android", "edge", "random"}
)

// DefaultNameservers are the resolvers DNS uses when none are given.
var DefaultNameservers = []string{"https://doh.pub/dns-query", "https://dns.alidns.com/dns-query"}

// NameserverHosts are the addresses of the DNS servers Moho knows by name,
// as their operators publish them. The core has to look a server's own name
// up before it can ask it anything, and does so over plain port 53; where
// that question is blocked, or led back into the tunnel and answered with a
// fake address, no name resolves at all. With the addresses written down the
// question is never asked.
var NameserverHosts = map[string][]string{
	"dns.alidns.com": {"223.5.5.5", "223.6.6.6"},
	"doh.pub":        {"1.12.12.12", "120.53.53.53"},
	"dot.pub":        {"1.12.12.12", "120.53.53.53"},
}

// nameserverHost is the name in a DNS server's address: "" for one written
// as an IP, and for system, dhcp and the like.
func nameserverHost(server string) string {
	if !strings.Contains(server, "://") {
		return ""
	}
	u, err := url.Parse(server)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if _, err := netip.ParseAddr(host); err == nil {
		return ""
	}
	return host
}

// DefaultOptions are Moho's choices.
var DefaultOptions = Options{
	LanAllowedIPs: []string{"0.0.0.0/0", "::/0"},

	IPv6: IPv6Auto, UnifiedDelay: true, TCPConcurrent: true,
	FindProcess:       "always",
	KeepAliveInterval: 15,
	URLTestInterval:   300, URLTestTolerance: 30, HealthInterval: 600,

	DNSMode:            "fake-ip",
	Nameservers:        DefaultNameservers,
	DefaultNameservers: []string{"223.5.5.5", "119.29.29.29"},
	FakeIPRange:        "198.18.0.1/16",
	FakeIPFilter:       []string{"*.lan", "*.local", "localhost.ptlogin2.qq.com", "+.stun.*.*", "+.msftconnecttest.com", "+.msftncsi.com", "time.*.com", "+.ntp.org"},
	UseHosts:           true,
	UseSystemHosts:     true,

	TunStack:            "mixed",
	TunMTU:              9000,
	AutoDetectInterface: true,
	DNSHijack:           []string{"any:53", "tcp://any:53"},

	SniffHTTP:            []string{"80", "8080-8880"},
	SniffTLS:             []string{"443", "8443"},
	SniffQUIC:            []string{"443", "8443"},
	SniffForceDNSMapping: true,
	SniffParsePureIP:     true,
	SniffSkip:            []string{"+.push.apple.com", "dlg.io.mi.com"},

	GeoUpdateInterval: 24,
	// A CDN reachable without a proxy, for the first download.
	GeoIPURL:   "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip-lite.dat",
	GeoSiteURL: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat",
	MMDBURL:    "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/country-lite.mmdb",
}

// Clone is a copy that shares no lists with o.
func (o Options) Clone() Options {
	for _, l := range o.lists() {
		*l = slices.Clone(*l)
	}
	o.ExtraPorts = slices.Clone(o.ExtraPorts)
	return o
}

func (o *Options) lists() []*[]string {
	return []*[]string{&o.Authentication, &o.LanAllowedIPs, &o.Nameservers, &o.DefaultNameservers,
		&o.Fallback, &o.ProxyNameservers, &o.NameserverPolicy, &o.FakeIPFilter, &o.Hosts, &o.DNSHijack,
		&o.SniffHTTP, &o.SniffTLS, &o.SniffQUIC, &o.SniffSkip, &o.DevOffScenarios, &o.DevExtra}
}

// Fill gives the options that cannot be empty their default, for settings
// saved before they existed or emptied by hand.
func (o *Options) Fill() {
	d := DefaultOptions.Clone()
	if o.IPv6 == "" {
		o.IPv6 = d.IPv6
	}
	str := func(v *string, def string) {
		if *v == "" {
			*v = def
		}
	}
	num := func(v *int, def int) {
		if *v <= 0 {
			*v = def
		}
	}
	list := func(v *[]string, def []string) {
		if len(*v) == 0 {
			*v = def
		}
	}
	str(&o.FindProcess, d.FindProcess)
	str(&o.DNSMode, d.DNSMode)
	str(&o.FakeIPRange, d.FakeIPRange)
	str(&o.TunStack, d.TunStack)
	str(&o.GeoIPURL, d.GeoIPURL)
	str(&o.GeoSiteURL, d.GeoSiteURL)
	str(&o.MMDBURL, d.MMDBURL)
	num(&o.KeepAliveInterval, d.KeepAliveInterval)
	num(&o.URLTestInterval, d.URLTestInterval)
	num(&o.HealthInterval, d.HealthInterval)
	num(&o.TunMTU, d.TunMTU)
	num(&o.GeoUpdateInterval, d.GeoUpdateInterval)
	list(&o.Nameservers, d.Nameservers)
	list(&o.DefaultNameservers, d.DefaultNameservers)
	list(&o.LanAllowedIPs, d.LanAllowedIPs)
	list(&o.SniffHTTP, d.SniffHTTP)
	list(&o.SniffTLS, d.SniffTLS)
	list(&o.SniffQUIC, d.SniffQUIC)
}

// Pair splits a "key=value" entry.
func Pair(s string) (k, v string, ok bool) {
	k, v, ok = strings.Cut(s, "=")
	k, v = strings.TrimSpace(k), strings.TrimSpace(v)
	return k, v, ok && k != "" && v != ""
}

// Validate says what is wrong with o, in words for the interface.
func (o Options) Validate() error {
	for _, p := range []struct {
		name string
		v    int
	}{{i18n.T("HTTP 端口", "The HTTP port"), o.HTTPPort}, {i18n.T("SOCKS 端口", "The SOCKS port"), o.SocksPort}} {
		if p.v < 0 || p.v > 65535 {
			return fmt.Errorf(i18n.T("%s要在 0 到 65535 之间", "%s must be between 0 and 65535"), p.name)
		}
	}
	for _, e := range o.ExtraPorts {
		if e.Port < 1 || e.Port > 65535 {
			return errors.New(i18n.T("端口要在 1 到 65535 之间", "The port must be between 1 and 65535"))
		}
		if e.Route == "" {
			return fmt.Errorf(i18n.T("端口 %d 没有写路由组", "Port %d names no route group"), e.Port)
		}
	}
	for _, a := range o.Authentication {
		if u, pw, ok := strings.Cut(a, ":"); !ok || u == "" || pw == "" {
			return fmt.Errorf(i18n.T("认证要写成 用户名:密码: %s", "Authentication must be user:password: %s"), a)
		}
	}
	for _, p := range o.LanAllowedIPs {
		if _, err := netip.ParsePrefix(p); err != nil {
			return fmt.Errorf(i18n.T("允许的地址要写成网段, 如 192.168.0.0/16: %s", "Allowed addresses must be networks, as 192.168.0.0/16: %s"), p)
		}
	}
	if !slices.Contains(FindProcess, o.FindProcess) {
		return fmt.Errorf(i18n.T("未知进程匹配模式: %s", "Unknown process matching mode: %s"), o.FindProcess)
	}
	if !slices.Contains(Fingerprints, o.Fingerprint) {
		return fmt.Errorf(i18n.T("未知 TLS 指纹: %s", "Unknown TLS fingerprint: %s"), o.Fingerprint)
	}
	if strings.ContainsAny(o.Interface, " \t,") {
		return fmt.Errorf(i18n.T("网卡名写法不对: %s", "Malformed interface name: %s"), o.Interface)
	}
	for _, n := range []struct {
		name      string
		v, lo, hi int
	}{
		{i18n.T("Keep-alive 间隔", "The keep-alive interval"), o.KeepAliveInterval, 1, 3600},
		{i18n.T("自动测速间隔", "The test interval"), o.URLTestInterval, 10, 86400},
		{i18n.T("切换容差", "The tolerance"), o.URLTestTolerance, 0, 5000},
		{i18n.T("健康检查间隔", "The health check interval"), o.HealthInterval, 10, 86400},
		{i18n.T("MTU", "The MTU"), o.TunMTU, 576, 65535},
		{i18n.T("GeoData 更新间隔", "The GeoData interval"), o.GeoUpdateInterval, 1, 24 * 30},
	} {
		if n.v < n.lo || n.v > n.hi {
			return fmt.Errorf(i18n.T("%s要在 %d 到 %d 之间", "%s must be between %d and %d"), n.name, n.lo, n.hi)
		}
	}

	if !slices.Contains(DNSModes, o.DNSMode) {
		return fmt.Errorf(i18n.T("未知 DNS 模式: %s", "Unknown DNS mode: %s"), o.DNSMode)
	}
	if len(o.Nameservers) == 0 {
		return errors.New(i18n.T("至少要有一个 DNS 服务器", "At least one DNS server is needed"))
	}
	if len(o.DefaultNameservers) == 0 {
		return errors.New(i18n.T("至少要有一个引导 DNS 服务器", "At least one bootstrap DNS server is needed"))
	}
	for _, ns := range o.DefaultNameservers {
		if !ipServer(ns) {
			return fmt.Errorf(i18n.T("引导 DNS 要写 IP: %s", "Bootstrap DNS servers must be IPs: %s"), ns)
		}
	}
	for _, l := range [][]string{o.Nameservers, o.DefaultNameservers, o.Fallback, o.ProxyNameservers, o.FakeIPFilter, o.DNSHijack, o.SniffSkip} {
		for _, s := range l {
			if strings.ContainsAny(s, " \t,") {
				return fmt.Errorf(i18n.T("写法不对: %s", "Malformed entry: %s"), s)
			}
		}
	}
	if o.RespectRules && len(o.ProxyNameservers) == 0 {
		return errors.New(i18n.T("DNS 遵循规则要先填写节点域名的 DNS 服务器", "Following the rules needs DNS servers for the nodes' names first"))
	}
	for _, p := range o.NameserverPolicy {
		if _, _, ok := Pair(p); !ok {
			return fmt.Errorf(i18n.T("DNS 策略要写成 域名=服务器: %s", "DNS policy entries must be domain=server: %s"), p)
		}
	}
	if _, err := netip.ParsePrefix(o.FakeIPRange); err != nil {
		return fmt.Errorf(i18n.T("Fake-IP 网段写法不对: %s", "Malformed Fake-IP range: %s"), o.FakeIPRange)
	}
	for _, h := range o.Hosts {
		if _, v, ok := Pair(h); !ok || strings.ContainsAny(v, " \t,") {
			return fmt.Errorf(i18n.T("Hosts 要写成 域名=IP: %s", "Hosts entries must be domain=IP: %s"), h)
		}
	}

	if !slices.Contains(TunStacks, o.TunStack) {
		return fmt.Errorf(i18n.T("未知协议栈: %s", "Unknown stack: %s"), o.TunStack)
	}
	if err := checkTunDevice(o.TunDevice, runtime.GOOS); err != nil {
		return err
	}

	for _, l := range [][]string{o.SniffHTTP, o.SniffTLS, o.SniffQUIC} {
		for _, p := range l {
			if !portRange(p) {
				return fmt.Errorf(i18n.T("嗅探端口要写成 443 或 8080-8880: %s", "Sniffing ports must be like 443 or 8080-8880: %s"), p)
			}
		}
	}

	for _, id := range o.DevOffScenarios {
		if !devnet.Known(id) {
			return fmt.Errorf(i18n.T("未知的开发者网络场景: %s", "Unknown developer network scenario: %s"), id)
		}
	}
	for _, e := range o.DevExtra {
		if !devnet.Valid(e) {
			return fmt.Errorf(i18n.T("要写成域名后缀或网段, 如 corp.example 或 10.8.0.0/16: %s", "Must be a domain suffix or a network, as corp.example or 10.8.0.0/16: %s"), e)
		}
	}

	for _, s := range []string{o.GeoIPURL, o.GeoSiteURL, o.MMDBURL} {
		if u, err := url.Parse(s); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf(i18n.T("GeoData 下载地址要是 http 或 https 链接: %s", "GeoData URLs must be http or https links: %s"), s)
		}
	}

	if o.ExtController != "" {
		if _, port, err := net.SplitHostPort(o.ExtController); err != nil || !portRange(port) || strings.Contains(port, "-") {
			return errors.New(i18n.T("外部控制地址要写成 127.0.0.1:9090", "The controller address must be like 127.0.0.1:9090"))
		}
	}
	if strings.ContainsAny(o.ExtSecret, " \t") {
		return errors.New(i18n.T("密钥不能有空格", "The secret cannot have spaces"))
	}
	return nil
}

// ExtraPortNumbers are the ports of ExtraPorts.
func (o Options) ExtraPortNumbers() []int {
	var out []int
	for _, e := range o.ExtraPorts {
		out = append(out, e.Port)
	}
	return out
}

// ExtPort is the extra controller's port, 0 without one.
func (o Options) ExtPort() int {
	_, p, err := net.SplitHostPort(o.ExtController)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

// ipServer says whether a resolver is given by its IP, as the bootstrap
// ones must be: "223.5.5.5", "223.5.5.5:53", "tls://1.1.1.1".
func ipServer(s string) bool {
	if _, rest, ok := strings.Cut(s, "://"); ok {
		s = rest
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	return net.ParseIP(strings.Trim(s, "[]")) != nil
}

// portRange says whether s is a port, or two joined by "-".
func portRange(s string) bool {
	lo, hi, rng := strings.Cut(s, "-")
	a, err := strconv.Atoi(lo)
	if err != nil || a < 1 || a > 65535 {
		return false
	}
	if !rng {
		return true
	}
	b, err := strconv.Atoi(hi)
	return err == nil && b >= a && b <= 65535
}

// checkTunDevice says whether the system goos can name a TUN device so.
// macOS only has utunN; Linux takes any name of up to 15 bytes without a
// slash or a space; Windows names the adapter as it likes.
func checkTunDevice(name, goos string) error {
	switch {
	case name == "":
	case goos == "darwin" && !strings.HasPrefix(name, "utun"):
		return errors.New(i18n.T("macOS 的 TUN 网卡名要以 utun 开头, 如 utun1024", "On macOS the TUN device must start with utun, as utun1024"))
	case goos == "linux" && (len(name) > 15 || strings.ContainsAny(name, "/ \t") || name == "." || name == ".."):
		return errors.New(i18n.T("Linux 的网卡名最长 15 个字符，不能有空格和斜杠", "On Linux a device name is at most 15 characters, with no space or slash"))
	}
	return nil
}
