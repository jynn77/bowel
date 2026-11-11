package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/gateway"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
	"github.com/oboo/moho/internal/sysproxy"
)

// TestAppFlows drives the app without a window: a real core, a
// subscription served locally, and the actions the page calls. It runs on
// a port of its own, and the system proxy is out of its reach: a call to
// change it fails the test.
func TestAppFlows(t *testing.T) {
	if testing.Short() {
		t.Skip("starts the core")
	}
	saved := osProxy
	osProxy.enable = func(string, int, []string) error { t.Error("the test tried to enable the system proxy"); return nil }
	osProxy.disable = func() error { t.Error("the test tried to disable the system proxy"); return nil }
	osProxy.snapshot = func() (sysproxy.Saved, error) { return sysproxy.Saved{}, nil }
	osProxy.restore = func(context.Context, sysproxy.Saved) error {
		t.Error("the test tried to restore the system proxy")
		return nil
	}
	defer func() { osProxy = saved }()
	savedGw := osGateway
	osGateway = func() gateway.System { return &gwFake{t: t} }
	defer func() { osGateway = savedGw }()
	dir, err := os.MkdirTemp("", "mh") // short: unix socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	mygo.App.SetPath(mygo.PathUserData, dir)
	defer mygo.App.SetPath(mygo.PathUserData, "")
	if !exists(bundledCore()) {
		t.Skip("no core; run scripts/fetch-core.sh")
	}

	sub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=0; download=95; total=100; expire=0")
		_, _ = io.WriteString(w, "proxies:\n  - {name: \"🇭🇰 香港 01\", type: direct}\n  - {name: \"JP-Tokyo-02\", type: direct}\n")
	}))
	defer sub.Close()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond) // stay open long enough to be seen
		_, _ = io.WriteString(w, strings.Repeat("x", 1<<16))
	}))
	defer page.Close()

	a, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	a.http = sub.Client()
	port, err := core.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.settings.Update(func(s *settings.Settings) { s.MixedPort = port }); err != nil {
		t.Fatal(err)
	}
	a.copyGeo()
	if err := a.applyConfig(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.shutdown()

	s := a.snapshot()
	if s.State != StateOff || s.Core.State != "running" || len(s.Routes) != 6 || len(s.Nodes) != 0 {
		t.Fatalf("fresh snapshot: state %s core %+v routes %d nodes %d", s.State, s.Core, len(s.Routes), len(s.Nodes))
	}

	ctx := context.Background()
	if k, _ := a.detectProvider(sub.URL); k != "subscription" {
		t.Fatalf("detect = %q", k)
	}
	pv, err := a.addProvider(ctx, sub.URL+"/s?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pv.Source, "secret") || pv.Warn != "quota" {
		t.Fatalf("provider view %+v", pv)
	}
	a.refresh()
	s = a.snapshot()
	if len(s.Nodes) != 2 || s.Providers[0].Nodes != 2 {
		t.Fatalf("nodes %+v", s.Nodes)
	}
	var hk, jp NodeView
	for _, n := range s.Nodes {
		switch n.Display {
		case "香港 01": // the flag is drawn, not in the label (trimFlag)
			hk = n
		case "JP-Tokyo-02":
			jp = n
		}
	}
	if hk.Where.Place == nil || hk.Where.Place.Country != "HK" || hk.Where.Source != "name" || jp.Where.Place == nil || jp.Where.Place.City != "东京" {
		t.Fatalf("places: %+v / %+v", hk.Where, jp.Where)
	}
	if hk.Provider != pv.ID || !strings.HasPrefix(hk.Name, "🇭🇰 香港 01"+core.NodeSep) {
		t.Fatalf("node ids: %+v", hk)
	}
	def := routeIn(s, "default")
	if def.ID != "default" || !def.Auto || def.Node == "" {
		t.Fatalf("default route %+v", def)
	}

	// Manual choice, then back to automatic.
	if err := a.selectNode("ai", jp.Name); err != nil {
		t.Fatal(err)
	}
	if r := routeIn(a.snapshot(), "ai"); r.Auto || r.Node != jp.Name {
		t.Fatalf("ai after select %+v", r)
	}
	if err := a.selectNode("ai", ""); err != nil {
		t.Fatal(err)
	}
	if r := routeIn(a.snapshot(), "ai"); !r.Auto {
		t.Fatalf("ai after auto %+v", r)
	}

	if err := a.setMode("global"); err != nil {
		t.Fatal(err)
	}
	if a.snapshot().Mode != "global" {
		t.Fatal("mode")
	}
	_ = a.setMode("rule")

	// A connection through the mixed port is attributed to a route. As
	// with the window shown: the monitor polls every second.
	a.winShown.Store(true)
	a.mon.Wake()
	proxyURL, _ := url.Parse("http://127.0.0.1:" + itoa(a.settings.Get().MixedPort))
	cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 10 * time.Second}
	go func() { _, _ = cl.Get(page.URL) }()
	var seen Conn
	for i := 0; i < 30 && seen.ID == ""; i++ {
		time.Sleep(200 * time.Millisecond)
		for _, c := range a.connections() {
			if c.Port != 0 && (c.Host != "" && strings.HasPrefix(page.URL, "http://"+c.Host+":") || c.IP != "" && strings.Contains(page.URL, "//"+c.IP+":")) {
				seen = c
			}
		}
	}
	if seen.ID == "" {
		t.Fatalf("connection not seen: %+v", a.connections())
	}
	if seen.Route != "direct" { // 127.0.0.1 matches GEOIP,private
		t.Errorf("connection route %q (rule %s/%s, chain %v)", seen.Route, seen.Rule, seen.Payload, seen.Chain)
	}
	// A rule of the user's sends the same address elsewhere, at once.
	mine := routes.UserRule{Kind: routes.KindIP, Value: "127.0.0.1/32", Route: "ai"}
	if _, err := a.addRule(mine); err != nil {
		t.Fatal(err)
	}
	if r := routeIn(a.snapshot(), "ai"); r.Mine != 1 {
		t.Errorf("ai after the rule: %+v", r)
	}
	go func() { _, _ = cl.Get(page.URL) }()
	var again Conn
	for i := 0; i < 30 && again.ID == ""; i++ {
		time.Sleep(200 * time.Millisecond)
		for _, c := range a.connections() {
			if c.ID != seen.ID && c.Port != 0 && c.Route == "ai" && mine.Matched(c.Rule, c.Payload) {
				again = c
			}
		}
	}
	if again.ID == "" {
		t.Errorf("no connection by the user's rule: %+v", a.connections())
	}
	if _, err := a.removeRule(mine); err != nil {
		t.Fatal(err)
	}
	if r := routeIn(a.snapshot(), "ai"); r.Mine != 0 || len(a.settings.Get().Rules) != 0 {
		t.Errorf("ai after removing the rule: %+v", r)
	}
	// The route that refuses: the request never reaches the site.
	var reached atomic.Int32
	refused := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	defer refused.Close()
	block := routes.UserRule{Kind: routes.KindIP, Value: "127.0.0.1/32", Route: routes.Reject}
	if _, err := a.addRule(block); err != nil {
		t.Fatal(err)
	}
	if r := routeIn(a.snapshot(), routes.Reject); r.Mine != 1 || !r.Reject || r.Node != "" {
		t.Errorf("reject after the rule: %+v", r)
	}
	if resp, err := cl.Get(refused.URL); err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Error("the blocked request succeeded")
		}
	}
	if n := reached.Load(); n != 0 {
		t.Errorf("the blocked site was reached %d times", n)
	}
	// The core's log tells of it, though a poll would not have caught it.
	var shown Conn
	for i := 0; i < 30 && shown.ID == ""; i++ {
		time.Sleep(100 * time.Millisecond)
		for _, c := range a.connections() {
			if c.Status == "blocked" {
				shown = c
			}
		}
	}
	if shown.Route != routes.Reject || shown.Rule != "IPCIDR" || shown.Payload != "127.0.0.1/32" || !slices.Equal(shown.Chain, []string{routes.Title(a.route(routes.Reject))}) {
		t.Errorf("blocked connection: %+v", shown)
	}
	if got := a.routeOfGroup("REJECT", "IPCIDR"); got != routes.Reject {
		t.Errorf("a refused connection is on route %q", got)
	}
	if got := a.routeOfGroup("DIRECT", "IPCIDR"); got != "direct" {
		t.Errorf("a direct connection is on route %q", got)
	}
	if _, err := a.removeRule(block); err != nil {
		t.Fatal(err)
	}
	for _, c := range a.connections() {
		if len(c.Chain) > 0 && c.Chain[0] == core.ProbeGroup {
			t.Errorf("the probe's own connection is listed: %+v", c)
		}
	}
	tk := a.buildTick()
	if tk.ByRoute == nil || len(tk.Events) == 0 {
		t.Errorf("tick %+v", tk)
	}

	// Disabling the only provider leaves no nodes.
	if err := a.setProviderEnabled(pv.ID, false); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	if n := len(a.snapshot().Nodes); n != 0 {
		t.Fatalf("nodes after disable: %d", n)
	}
	if err := a.deleteProvider(pv.ID); err != nil {
		t.Fatal(err)
	}
}

func routeIn(s Snapshot, id string) RouteView {
	for _, r := range s.Routes {
		if r.ID == id {
			return r
		}
	}
	return RouteView{}
}
