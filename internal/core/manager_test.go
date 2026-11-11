package core

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oboo/moho/internal/routes"
)

func coreBin(t *testing.T) string {
	if p := os.Getenv("MOHO_CORE"); p != "" {
		return p
	}
	name := "mihomo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p, _ := filepath.Abs(filepath.Join("..", "..", "resources", runtime.GOOS+"-"+runtime.GOARCH, "bin", name))
	if _, err := os.Stat(p); err != nil {
		t.Skip("no mihomo binary; run scripts/fetch-core.sh")
	}
	return p
}

// geo copies the bundled geo data into dir, as the app does.
func geo(t *testing.T, dir string) {
	src, _ := filepath.Abs(filepath.Join("..", "..", "resources", "geo"))
	for _, f := range []string{"GeoSite.dat", "GeoIP.dat"} {
		b, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Skip("no geo data; run scripts/fetch-core.sh")
		}
		_ = os.WriteFile(filepath.Join(dir, f), b, 0o600)
	}
}

const yamlNodes = "proxies:\n  - {name: \"东京 01\", type: direct}\n  - {name: \"香港 02\", type: direct}\n"
const linkNodes = "trojan://pw@203.0.113.11:443?sni=example.com#%E9%A6%99%E6%B8%AF%2002\n"

func TestManagerLifecycle(t *testing.T) {
	bin := coreBin(t)
	dir, err := os.MkdirTemp("", "moho")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	geo(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte(yamlNodes), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "b.txt"), []byte(linkNodes), 0o600)
	m, err := New(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	probe, _ := FreePort()
	in := Input{
		Providers: []ProviderRef{{ID: "aaa", Path: filepath.Join(dir, "a.txt")}, {ID: "bbb", Path: filepath.Join(dir, "b.txt")}},
		Routes:    routes.Template(), Mode: "rule", MixedPort: 17899, LogLevel: "info",
		TestURL: "https://www.gstatic.com/generate_204", ProbePort: probe, Controller: m.ControllerFields(),
	}
	cfg, err := BuildConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("start: %v\n%s\n%s", err, m.logs.last(10), cfg)
	}
	defer m.Stop()

	c, _ := m.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	px, err := c.Proxies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	g := px["默认"]
	if g.Type != "Selector" || g.Now != "默认·自动" {
		t.Fatalf("默认 = %+v", g)
	}
	// Both providers' 香港 02 are there, told apart by the marker.
	var hk []string
	for _, n := range g.All {
		if strings.HasPrefix(n, "香港 02"+NodeSep) {
			hk = append(hk, n)
		}
	}
	if len(hk) != 2 {
		t.Fatalf("香港 02 nodes = %q in %q", hk, g.All)
	}
	if err := c.Select(ctx, "AI 服务", "东京 01"+NodeSep+"aaa"); err != nil {
		t.Fatal(err)
	}
	if _, ok := px[ProbeGroup]; !ok {
		t.Fatal("no probe group")
	}
	// The 设置 window's DNS buttons; the core answers even without DNS on.
	if err := c.FlushDNS(ctx); err != nil {
		t.Errorf("flush DNS: %v", err)
	}
	if err := c.FlushFakeIP(ctx); err != nil {
		t.Errorf("flush fake-ip: %v", err)
	}

	// Reload with a provider disabled, without restarting.
	pid := m.Status().PID
	in.Providers = in.Providers[:1]
	in.Mode = "global"
	cfg, _ = BuildConfig(in)
	if err := m.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	conf, _ := c.Config(ctx)
	if conf.Mode != "global" || m.Status().PID != pid {
		t.Fatalf("after reload: %+v pid %d → %d", conf, pid, m.Status().PID)
	}
	m.Stop()
	if s := m.Status(); s.State != Stopped {
		t.Fatalf("after stop: %+v", s)
	}
}

func TestBuildConfigWithoutProviders(t *testing.T) {
	out, err := BuildConfig(Input{Routes: routes.Template(), Mode: "rule", MixedPort: 7899, ProbePort: 7900, TestURL: "x"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"MATCH,默认", "GEOSITE,cn,DIRECT", "DOMAIN-SUFFIX,openai.com,AI 服务", "proxy: MOHO-PROBE"} {
		if !strings.Contains(s, want) {
			t.Errorf("config lacks %q", want)
		}
	}
	if strings.Contains(s, "proxy-providers") || strings.Contains(s, "tun:") {
		t.Errorf("unexpected section:\n%s", s)
	}
}

// TestUserRulesComeFirst: the user's rules go before the template's, each
// to its route's group, a direct route's to DIRECT; a rule for a route
// that is gone is left out.
func TestUserRulesComeFirst(t *testing.T) {
	out, err := BuildConfig(Input{Routes: routes.Template(), Mode: "rule", MixedPort: 7899, ProbePort: 7900, TestURL: "x",
		UserRules: []routes.UserRule{
			{Kind: "suffix", Value: "openai.com", Route: "direct"},
			{Kind: "ip", Value: "1.2.3.0/24", Route: "ai"},
			{Kind: "domain", Value: "x.cn", Route: "default"},
			{Kind: "suffix", Value: "gone.com", Route: "gone"},
		}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	mine := strings.Index(s, "- DOMAIN-SUFFIX,openai.com,DIRECT")
	tpl := strings.Index(s, "- DOMAIN-SUFFIX,openai.com,AI 服务")
	if mine < 0 || tpl < 0 || mine > tpl {
		t.Errorf("user rule not before the template's (%d, %d):\n%s", mine, tpl, s)
	}
	for _, want := range []string{"- IP-CIDR,1.2.3.0/24,AI 服务,no-resolve", "- DOMAIN,x.cn,默认"} {
		if !strings.Contains(s, want) {
			t.Errorf("config lacks %q", want)
		}
	}
	if strings.Contains(s, "gone.com") {
		t.Error("a rule for a missing route was kept")
	}
}

// TestRejectRoute: a rule of the route that refuses targets REJECT, in its
// proper place among the rules, and the route has no group of its own.
func TestRejectRoute(t *testing.T) {
	out, err := BuildConfig(Input{Routes: routes.Template(), Mode: "rule", MixedPort: 7899, ProbePort: 7900, TestURL: "x",
		UserRules: []routes.UserRule{
			{Kind: "suffix", Value: "ads.example.com", Route: routes.Reject},
			{Kind: "ip", Value: "1.2.3.0/24", Route: routes.Reject},
			{Kind: "suffix", Value: "example.com", Route: "direct"},
		}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"- DOMAIN-SUFFIX,ads.example.com,REJECT", "- IP-CIDR,1.2.3.0/24,REJECT,no-resolve"} {
		if !strings.Contains(s, want) {
			t.Errorf("config lacks %q:\n%s", want, s)
		}
	}
	if i, j := strings.Index(s, "ads.example.com,REJECT"), strings.Index(s, "- DOMAIN-SUFFIX,example.com,DIRECT"); j < 0 || i > j {
		t.Errorf("the more specific rule isn't first (%d, %d)", i, j)
	}
	if strings.Contains(s, "name: 拦截") {
		t.Errorf("the route got a group:\n%s", s)
	}
}

// TestCloseKeepsCoreDown: after Close nothing starts a core, whatever is
// still under way when the app quits.
func TestCloseKeepsCoreDown(t *testing.T) {
	m, err := New("/nonexistent/mihomo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	if err := m.start(); !errors.Is(err, ErrClosed) {
		t.Errorf("start after Close: %v", err)
	}
}

// TestStrategyGroups: a real core takes the groups of all three strategies:
// a range filters 自动 by exact names, even ones with regex characters, an
// empty range rejects rather than widens, and the backup list keeps its
// order.
func TestStrategyGroups(t *testing.T) {
	bin := coreBin(t)
	dir, err := os.MkdirTemp("", "moho")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	geo(t, dir)
	nodes := "proxies:\n  - {name: \"东京 01\", type: direct}\n  - {name: \"香港 (IEPL) 02\", type: direct}\n  - {name: \"美国 03\", type: direct}\n"
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte(nodes), 0o600)
	m, err := New(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	probe, _ := FreePort()
	tokyo, hk, us := "东京 01"+NodeSep+"aaa", "香港 (IEPL) 02"+NodeSep+"aaa", "美国 03"+NodeSep+"aaa"
	in := Input{
		Providers: []ProviderRef{{ID: "aaa", Path: filepath.Join(dir, "a.txt")}},
		Routes:    routes.Template(), Mode: "rule", MixedPort: mustFree(t), LogLevel: "info",
		TestURL: "https://www.gstatic.com/generate_204", ProbePort: probe, Controller: m.ControllerFields(),
		Scopes: map[string][]string{"ai": {hk, tokyo}, "dev": {}},
		Orders: map[string][]string{"ai": {us, hk}},
	}
	cfg, err := BuildConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("start: %v\n%s\n%s", err, m.logs.last(10), cfg)
	}
	c, _ := m.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	px, err := c.Proxies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ai, dev routes.Route
	for _, r := range routes.Template() {
		switch r.ID {
		case "ai":
			ai = r
		case "dev":
			dev = r
		}
	}
	auto := px[routes.Auto(ai)].All
	slices.Sort(auto)
	want := []string{hk, tokyo}
	slices.Sort(want)
	if !slices.Equal(auto, want) {
		t.Errorf("scoped 自动 = %q, want %q", auto, want)
	}
	if got := px[routes.Auto(dev)].All; !slices.Equal(got, []string{"REJECT"}) {
		t.Errorf("empty range = %q", got)
	}
	backup := px[routes.Backup(ai)]
	if backup.Type != "Fallback" || !slices.Equal(backup.All, []string{routes.BackupStep(ai, 0), routes.BackupStep(ai, 1)}) {
		t.Fatalf("backup = %+v", backup)
	}
	for i, n := range []string{us, hk} {
		if got := px[routes.BackupStep(ai, i)].All; !slices.Equal(got, []string{n}) {
			t.Errorf("step %d = %q, want %q", i, got, n)
		}
	}
	// A strategy is only a position of the selector.
	if err := c.Select(ctx, routes.Group(ai), routes.Backup(ai)); err != nil {
		t.Errorf("select the backup group: %v", err)
	}
	if err := c.Select(ctx, routes.Group(ai), us); err != nil {
		t.Errorf("select a node out of range: %v", err)
	}
}

func mustFree(t *testing.T) int {
	t.Helper()
	p, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestListenErrorFromCoreLog reads the line of each system, and leaves the
// other listeners and the core's other complaints alone.
func TestListenErrorFromCoreLog(t *testing.T) {
	const prefix = `time="2026-11-10T04:08:08Z" level=error msg="`
	for _, tc := range []struct {
		name, msg, port string
	}{
		{"unix", `Start Mixed(http+socks) server error: listen tcp 127.0.0.1:7899: bind: address already in use`, "7899"},
		{"windows", `Start Mixed(http+socks) server error: listen tcp 127.0.0.1:7899: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted.`, "7899"},
		{"reserved on windows", `Start Mixed(http+socks) server error: listen tcp 127.0.0.1:7899: bind: An attempt was made to access a socket in a way forbidden by its access permissions.`, "7899"},
		{"all interfaces", `Start HTTP server error: listen tcp :8080: bind: address already in use`, "8080"},
		{"socks", `Start SOCKS server error: listen tcp [::1]:1080: bind: address already in use`, "1080"},
		{"an extra port", `Listener bowel-port-7901 listen err: listen tcp 127.0.0.1:7901: bind: address already in use`, "7901"},
		{"the probe's is not the user's", `Listener moho-probe listen err: listen tcp 127.0.0.1:7900: bind: address already in use`, ""},
		{"dns is not the proxy", `Start DNS server error: listen udp 127.0.0.1:1053: bind: address already in use`, ""},
		{"not a bind", `Start Mixed(http+socks) server error: too many open files`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manager{status: Status{State: Running}}
			calls := 0
			m.OnListenError = func() { calls++ }
			m.noteListenError(prefix + tc.msg + `"`)
			got := m.Status().Error
			if tc.port == "" {
				if got != "" || calls != 0 {
					t.Fatalf("error %q, %d calls", got, calls)
				}
				return
			}
			if !strings.Contains(got, tc.port) || calls != 1 {
				t.Fatalf("error %q, %d calls", got, calls)
			}
			m.noteListenError(prefix + tc.msg + `"`)
			if calls != 1 {
				t.Errorf("the same line again told %d times", calls)
			}
		})
	}
}

// TestListenErrorOnlyWhileRunning: a stopped core has no port to complain of.
func TestListenErrorOnlyWhileRunning(t *testing.T) {
	m := &Manager{status: Status{State: Stopped}}
	m.setListenErr("port")
	if got := m.Status().Error; got != "" {
		t.Errorf("stopped: %q", got)
	}
}

// TestProxyPortTaken runs the real core with its port taken: it goes on
// running, which must not pass for working, and a reload that gets the port
// clears the error.
func TestProxyPortTaken(t *testing.T) {
	bin := coreBin(t)
	dir := t.TempDir()
	geo(t, dir)
	m, err := New(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	told := make(chan struct{}, 8)
	m.OnListenError = func() { told <- struct{}{} }
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := taken.Addr().(*net.TCPAddr).Port
	probe, _ := FreePort()
	cfg, err := BuildConfig(Input{
		Routes: routes.Template(), Mode: "rule", MixedPort: port, LogLevel: "info",
		TestURL: "http://x/", ProbePort: probe, Controller: m.ControllerFields(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("start: %v\n%s", err, m.logs.last(10))
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: status %+v\n%s", what, m.Status(), m.logs.last(10))
			}
		}
	}
	want := strconv.Itoa(port)
	waitFor("port taken", func() bool { return strings.Contains(m.Status().Error, want) })
	if s := m.Status(); s.State != Running {
		t.Errorf("state %s, the core is still up", s.State)
	}
	<-told

	taken.Close()
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("reload: %v\n%s", err, m.logs.last(10))
	}
	waitFor("port free", func() bool { return m.Status().Error == "" })
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+want, time.Second)
	if err != nil {
		t.Fatalf("the core should have the port now: %v", err)
	}
	c.Close()
}
