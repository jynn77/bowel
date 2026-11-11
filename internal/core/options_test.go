package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/oboo/moho/internal/devnet"
	"github.com/oboo/moho/internal/routes"
)

// everything turns every option on, with the values a user would type.
func everything() Options {
	o := DefaultOptions.Clone()
	o.HTTPPort, o.SocksPort = 17801, 17802
	o.Authentication = []string{"me:secret"}
	o.LanAllowedIPs = []string{"192.168.0.0/16"}
	o.FindProcess, o.Fingerprint = "strict", "chrome"
	o.KeepAliveInterval, o.DisableKeepAlive = 30, true
	o.URLTestInterval, o.URLTestTolerance, o.HealthInterval = 120, 50, 900
	o.DNS = true
	o.DefaultNameservers = []string{"223.5.5.5", "tls://1.1.1.1:853"}
	o.Fallback = []string{"https://1.1.1.1/dns-query"}
	o.ProxyNameservers = []string{"https://doh.pub/dns-query"}
	o.NameserverPolicy = []string{"geosite:cn=223.5.5.5", "geosite:cn = 119.29.29.29", "+.corp.example=10.0.0.53"}
	o.Hosts = []string{"router.lan=192.168.1.1"}
	o.RespectRules, o.PreferH3 = true, true
	o.TunStack, o.TunDevice, o.TunMTU, o.EndpointIndependentNAT = "gvisor", "utun1024", 1500, true
	o.Sniff, o.SniffOverride = true, true
	o.GeoAutoUpdate, o.GeoUpdateInterval = true, 48
	o.ExtController, o.ExtSecret = "127.0.0.1:19090", "s3cret"
	return o
}

func TestOptionsValidate(t *testing.T) {
	if err := DefaultOptions.Validate(); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if err := everything().Validate(); err != nil {
		t.Fatalf("everything: %v", err)
	}
	cases := map[string]func(*Options){
		"http port":         func(o *Options) { o.HTTPPort = 70000 },
		"auth":              func(o *Options) { o.Authentication = []string{"nopassword"} },
		"lan ips":           func(o *Options) { o.LanAllowedIPs = []string{"192.168.1.1"} },
		"find process":      func(o *Options) { o.FindProcess = "maybe" },
		"fingerprint":       func(o *Options) { o.Fingerprint = "netscape" },
		"interval":          func(o *Options) { o.URLTestInterval = 1 },
		"mtu":               func(o *Options) { o.TunMTU = 100 },
		"dns mode":          func(o *Options) { o.DNSMode = "magic" },
		"stack":             func(o *Options) { o.TunStack = "lwip" },
		"no dns":            func(o *Options) { o.Nameservers = nil },
		"bootstrap by name": func(o *Options) { o.DefaultNameservers = []string{"dns.google"} },
		"respect rules":     func(o *Options) { o.RespectRules, o.ProxyNameservers = true, nil },
		"policy":            func(o *Options) { o.NameserverPolicy = []string{"geosite:cn"} },
		"fake-ip range":     func(o *Options) { o.FakeIPRange = "198.18.0.1" },
		"hosts":             func(o *Options) { o.Hosts = []string{"a.lan"} },
		"sniff port":        func(o *Options) { o.SniffTLS = []string{"443-80"} },
		"geo url":           func(o *Options) { o.GeoIPURL = "ftp://x/geoip.dat" },
		"controller":        func(o *Options) { o.ExtController = "9090" },
	}
	if runtime.GOOS == "darwin" {
		// Only macOS names the adapter, and only there must it be utun*.
		cases["device"] = func(o *Options) { o.TunDevice = "tun0" }
	}
	for name, fn := range cases {
		o := DefaultOptions.Clone()
		fn(&o)
		if o.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFillAndClone(t *testing.T) {
	var o Options
	o.Fill()
	if o.TunMTU != 9000 || o.DNSMode != "fake-ip" || len(o.Nameservers) == 0 || o.URLTestInterval != 300 {
		t.Fatalf("filled: %+v", o)
	}
	o.Nameservers[0] = "changed"
	if DefaultOptions.Nameservers[0] == "changed" {
		t.Fatal("Fill shares its lists with the defaults")
	}
	c := DefaultOptions.Clone()
	c.SniffSkip[0] = "changed"
	if DefaultOptions.SniffSkip[0] == "changed" {
		t.Fatal("Clone shares its lists")
	}
}

// TestEveryOptionPassesCore: with every option on, the real core accepts
// the config.
func TestEveryOptionPassesCore(t *testing.T) {
	bin := coreBin(t)
	dir := t.TempDir()
	geo(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte(yamlNodes), 0o600)
	m, err := New(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tun := range []bool{false, true} {
		in := Input{Routes: routes.Template(), Mode: "rule", MixedPort: 17899, LogLevel: "info", AllowLan: true,
			TestURL: "https://www.gstatic.com/generate_204", ProbePort: 17900, Controller: m.ControllerFields(),
			Tun: tun, Options: everything(), Providers: []ProviderRef{{ID: "aaa", Path: filepath.Join(dir, "a.txt")}},
			UserRules: []routes.UserRule{{Kind: "suffix", Value: "example.com", Route: "ai"}, {Kind: "domain", Value: "a.example.org", Route: "direct"},
				{Kind: "keyword", Value: "tracker", Route: "default"}, {Kind: "ip", Value: "10.0.0.0/8", Route: "media"}, {Kind: "ip", Value: "2001:db8::/32", Route: "dev"}}}
		cfg, err := BuildConfig(in)
		if err != nil {
			t.Fatal(err)
		}
		s := string(cfg)
		want := []string{
			"port: 17801", "socks-port: 17802", "- me:secret", "skip-auth-prefixes", "global-client-fingerprint: chrome",
			"find-process-mode: strict", "keep-alive-interval: 30", "interval: 120", "tolerance: 50", "interval: 900",
			"respect-rules: true", "geosite:cn:", "- 119.29.29.29", "router.lan: 192.168.1.1", "sniffer:", "8080-8880",
			"geo-update-interval: 48",
		}
		// The controller the manager asked for is the one in the config: the
		// profile's own stays only where the manager uses a unix socket and
		// leaves that key to it.
		if m.TCPAddr() != "" {
			want = append(want, "external-controller: "+m.TCPAddr(), "secret: "+m.ControllerFields()["secret"].(string))
		} else {
			want = append(want, "external-controller: 127.0.0.1:19090", "secret: s3cret")
		}
		for _, w := range want {
			if !strings.Contains(s, w) {
				t.Errorf("tun %v: config lacks %q", tun, w)
			}
		}
		if tun && (!strings.Contains(s, "device: utun1024") || !strings.Contains(s, "mtu: 1500")) {
			t.Errorf("tun options missing:\n%s", s)
		}
		p := filepath.Join(dir, "check.yaml")
		if err := os.WriteFile(p, cfg, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := m.Test(p); err != nil {
			t.Fatalf("tun %v: %v\n%s", tun, err, s)
		}
	}
}

// TestOwnTCPControllerWins: when Moho's controller fell back to TCP, the
// dashboards' one is left out rather than replacing it.
func TestOwnTCPControllerWins(t *testing.T) {
	in := Input{Routes: routes.Template(), Mode: "rule", MixedPort: 7899, ProbePort: 7900, TestURL: "x",
		Controller: map[string]any{"external-controller": "127.0.0.1:5555", "secret": "own"},
		Options:    everything()}
	out, _ := BuildConfig(in)
	s := string(out)
	if !strings.Contains(s, "127.0.0.1:5555") || strings.Contains(s, "19090") || strings.Contains(s, "s3cret") {
		t.Errorf("controller:\n%s", s)
	}
}

// Each route's automatic group goes by that route's main target.
func TestAutoGroupsUseMainURLs(t *testing.T) {
	in := Input{Routes: routes.Template(), Mode: "rule", MixedPort: 17899, LogLevel: "info", ProbePort: 17900,
		TestURL:   "https://base.example/204",
		MainURLs:  map[string]string{"media": "https://www.youtube.com/generate_204"},
		Providers: []ProviderRef{{ID: "aaa", Path: "/tmp/a.txt"}}}
	cfg, err := BuildConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Groups []struct {
			Name string `yaml:"name"`
			URL  string `yaml:"url"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(cfg, &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, g := range out.Groups {
		got[g.Name] = g.URL
	}
	if got["流媒体·自动"] != "https://www.youtube.com/generate_204" || got["AI 服务·自动"] != in.TestURL {
		t.Fatalf("groups: %+v", got)
	}
}

func TestIPv6ModeJSON(t *testing.T) {
	for in, want := range map[string]IPv6Mode{`true`: IPv6On, `false`: IPv6Off, `"on"`: IPv6On, `"off"`: IPv6Off, `"auto"`: IPv6Auto} {
		var m IPv6Mode
		if err := json.Unmarshal([]byte(in), &m); err != nil || m != want {
			t.Errorf("%s: %q %v", in, m, err)
		}
	}
	for _, bad := range []string{`"maybe"`, `1`} {
		var m IPv6Mode
		if err := json.Unmarshal([]byte(bad), &m); err == nil {
			t.Errorf("%s was taken", bad)
		}
	}
	for m, want := range map[IPv6Mode]string{IPv6Off: `false`, IPv6On: `"on"`, IPv6Auto: `"auto"`} {
		if b, err := json.Marshal(m); err != nil || string(b) != want {
			t.Errorf("%s: %s %v", m, b, err)
		}
	}
	// What a version from before the modes does with what this one saved:
	// it reads the rest, and keeps its default for a value it can't take.
	var old struct {
		IPv6 bool `json:"ipv6"`
		Port int  `json:"port"`
	}
	old.IPv6 = true
	_ = json.Unmarshal([]byte(`{"ipv6":"auto","port":7}`), &old)
	if !old.IPv6 || old.Port != 7 {
		t.Errorf("downgrade: %+v", old)
	}
}

func TestIPv6ModeEnabled(t *testing.T) {
	if IPv6Off.Enabled(true) || !IPv6On.Enabled(false) || IPv6Auto.Enabled(false) || !IPv6Auto.Enabled(true) {
		t.Error("wrong")
	}
}

// Every place the core's config names IPv6 follows what Auto found, and
// the TUN's routes are left to the core: it drops them along with the
// address when ipv6 is false.
func TestBuildConfigIPv6(t *testing.T) {
	base := func() Input {
		return Input{Mode: "rule", MixedPort: 7890, LogLevel: "info", TestURL: "http://x/", ProbePort: 7999, Routes: routes.Template()}
	}
	cases := []struct {
		name string
		in   func(*Input)
	}{
		{"tun", func(i *Input) { i.Tun = true }},
		{"dns switch", func(i *Input) { i.Options.DNS = true }},
		{"plain", func(i *Input) {}},
		{"dev policy", func(i *Input) { i.Dev = devnet.Overlay{Policy: []devnet.Policy{{Pattern: "+.x", Server: "1.1.1.1"}}} }},
		{"imported", func(i *Input) {
			i.Imported = &Imported{Rules: []string{"MATCH,DIRECT"}}
		}},
	}
	for _, c := range cases {
		for _, tc := range []struct {
			mode   IPv6Mode
			usable bool
			want   bool
		}{{IPv6Auto, true, true}, {IPv6Auto, false, false}, {IPv6On, false, true}, {IPv6Off, true, false}} {
			in := base()
			c.in(&in)
			in.IPv6, in.IPv6Usable = tc.mode, tc.usable
			raw, err := BuildConfig(in)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			var cfg struct {
				IPv6 bool `yaml:"ipv6"`
				DNS  struct {
					IPv6 bool `yaml:"ipv6"`
				} `yaml:"dns"`
			}
			if err := yaml.Unmarshal(raw, &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.IPv6 != tc.want || cfg.DNS.IPv6 != tc.want {
				t.Errorf("%s %s usable=%v: top %v dns %v", c.name, tc.mode, tc.usable, cfg.IPv6, cfg.DNS.IPv6)
			}
		}
	}
}

// Each system names TUN devices its own way.
func TestCheckTunDevice(t *testing.T) {
	for _, c := range []struct {
		name, goos string
		ok         bool
	}{
		{"", "darwin", true}, {"utun1024", "darwin", true}, {"tun0", "darwin", false},
		{"", "linux", true}, {"tun0", "linux", true}, {"bowel", "linux", true}, {"utun1024", "linux", true},
		{"a-name-of-16-byt", "linux", false}, {"tun 0", "linux", false}, {"a/b", "linux", false}, {"..", "linux", false},
		{"bowel", "windows", true}, {"My Tunnel", "windows", true},
	} {
		if err := checkTunDevice(c.name, c.goos); (err == nil) != c.ok {
			t.Errorf("%q on %s: %v", c.name, c.goos, err)
		}
	}
}
