package core

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oboo/moho/internal/routes"
	"gopkg.in/yaml.v3"
)

// extraListeners are the listeners of a config besides the probe's: their
// proxy by their port.
func extraListeners(t *testing.T, in Input) map[int]string {
	t.Helper()
	b, err := BuildConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Listeners []struct {
			Name, Type, Listen, Proxy string
			Port                      int
		}
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	out := map[int]string{}
	for _, l := range cfg.Listeners {
		if l.Name == "moho-probe" {
			continue
		}
		if l.Type != "mixed" || l.Listen != "127.0.0.1" || l.Name != ExtraPortListener(l.Port) {
			t.Errorf("listener %+v", l)
		}
		out[l.Port] = l.Proxy
	}
	return out
}

// TestExtraPortsInConfig: each extra port is a listener of this machine
// alone that goes where its route's rules go, in every mode; one whose route
// is gone is left out, and so are all of them under a provider's routing.
func TestExtraPortsInConfig(t *testing.T) {
	var p routes.Plan
	p.ChangePolicy("media", func(q *routes.Policy) { q.Direct = true })
	all := routes.Build(p, nil)
	ports := []ExtraPort{{7901, "ai"}, {7902, routes.Default}, {7903, "media"}, {7904, "direct"}, {7905, routes.Reject}, {7906, "gone"}}
	want := map[int]string{7901: "AI 服务", 7902: "默认", 7903: "流媒体", 7904: "DIRECT", 7905: "REJECT"}
	for _, mode := range []string{"rule", "global", "direct"} {
		in := Input{Routes: all, Mode: mode, MixedPort: 7899, ProbePort: 7900, TestURL: "x", Options: Options{ExtraPorts: ports}}
		got := extraListeners(t, in)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s mode: listeners %v, want %v", mode, got, want)
		}
	}
	in := Input{Routes: all, Mode: "rule", MixedPort: 7899, ProbePort: 7900, TestURL: "x", Options: Options{ExtraPorts: ports},
		Imported: &Imported{Rules: []string{"MATCH,DIRECT"}}}
	if got := extraListeners(t, in); len(got) != 0 {
		t.Errorf("a provider's routing: listeners %v", got)
	}
	if got := extraListeners(t, Input{Routes: all, Mode: "rule", MixedPort: 7899, ProbePort: 7900, TestURL: "x"}); len(got) != 0 {
		t.Errorf("no extra ports: listeners %v", got)
	}
}

func TestExtraPortsValidate(t *testing.T) {
	for _, bad := range [][]ExtraPort{{{0, "ai"}}, {{70000, "ai"}}, {{7901, ""}}} {
		o := DefaultOptions.Clone()
		o.ExtraPorts = bad
		if err := o.Validate(); err == nil {
			t.Errorf("%v should not pass", bad)
		}
	}
	o := DefaultOptions.Clone()
	o.ExtraPorts = []ExtraPort{{7901, "ai"}}
	if err := o.Validate(); err != nil {
		t.Error(err)
	}
	c := o.Clone()
	c.ExtraPorts[0].Port = 1
	if o.ExtraPorts[0].Port != 7901 {
		t.Error("the clone shares the list")
	}
}

// TestExtraPortsRealCore: in direct mode, where the mixed port sends
// everything straight out, two extra ports still leave by their own groups;
// and one that another program holds is told as the mixed port's is.
func TestExtraPortsRealCore(t *testing.T) {
	bin := coreBin(t)
	dir, err := os.MkdirTemp("", "moho")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	geo(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte(yamlNodes), 0o600)
	m, err := New(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer web.Close()
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	held := taken.Addr().(*net.TCPAddr).Port

	mixed, ai, media, probe := mustFree(t), mustFree(t), mustFree(t), mustFree(t)
	in := Input{
		Providers: []ProviderRef{{ID: "aaa", Path: filepath.Join(dir, "a.txt")}},
		Routes:    routes.Template(), Mode: "direct", MixedPort: mixed, LogLevel: "info",
		TestURL: "http://x/", ProbePort: probe, Controller: m.ControllerFields(),
		Options: Options{FindProcess: "off", ExtraPorts: []ExtraPort{{ai, "ai"}, {media, "media"}, {held, "dev"}}},
	}
	cfg, err := BuildConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("start: %v\n%s\n%s", err, m.logs.last(10), cfg)
	}
	defer m.Stop()

	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: status %+v\n%s", what, m.Status(), m.logs.last(30))
			}
		}
	}
	waitFor("the held port is told", func() bool { return strings.Contains(m.Status().Error, strconv.Itoa(held)) })

	for port, via := range map[int]string{ai: "AI 服务", media: "流媒体", mixed: "DIRECT"} {
		proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
		client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}}
		res, err := client.Get(web.URL)
		if err != nil {
			t.Fatalf("port %d: %v\n%s", port, err, m.logs.last(30))
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("port %d: status %d", port, res.StatusCode)
		}
		waitFor(fmt.Sprintf("port %d goes by %s", port, via), func() bool {
			for _, l := range m.logs.all() {
				if strings.Contains(l, strings.TrimPrefix(web.URL, "http://")) && strings.Contains(l, "using "+via) {
					return true
				}
			}
			return false
		})
	}
}
