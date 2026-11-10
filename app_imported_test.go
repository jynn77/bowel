package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

// issue33Sub is the example of issue #33, with nodes that really connect
// and a rule a test can reach without the internet.
const issue33Sub = `proxies:
  - {name: JP-01, type: direct}
  - {name: JP-02, type: direct}
proxy-groups:
  - name: Japan
    type: url-test
    proxies: [JP-01, JP-02]
    url: http://127.0.0.1:9/
    interval: 300
  - name: Streaming
    type: select
    proxies: [Japan, DIRECT]
rules:
  - DOMAIN-SUFFIX,netflix.com,Streaming
  - MATCH,Japan
`

// importedApp is an app on a real core with a subscription whose body the
// returned function replaces.
func importedApp(t *testing.T) (*App, string, func(string)) {
	t.Helper()
	a, _ := routingApp(t, func(s *settings.Settings) {
		s.Rules = []routes.UserRule{{Kind: routes.KindDomain, Value: "mine.example", Route: "ai"}}
		s.Hosts = []string{"netflix.com=127.0.0.1"} // the example's rule, without the internet
		s.DevOff = true                             // which would keep loopback for itself
	})
	var mu sync.Mutex
	body := issue33Sub
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	v, err := a.addProvider(context.Background(), srv.URL+"/full")
	if err != nil {
		t.Fatal(err)
	}
	return a, v.ID, func(s string) {
		mu.Lock()
		defer mu.Unlock()
		body = s
	}
}

func hasRoute(a *App, name string) bool {
	return slices.ContainsFunc(a.routes(), func(r routes.Route) bool { return r.Name == name })
}

func TestImportedRoutingEndToEnd(t *testing.T) {
	a, id, serve := importedApp(t)
	ctx := context.Background()

	// Bringing groups changes nothing until asked for, updates included.
	pv := a.providerView(mustProvider(t, a, id))
	if pv.Routing.Groups != 2 || pv.Routing.Rules != 2 || pv.Routing.InUse {
		t.Fatalf("routing = %+v", pv.Routing)
	}
	if err := a.updateProvider(ctx, id); err != nil {
		t.Fatal(err)
	}
	if hasRoute(a, "Japan") || !hasRoute(a, "AI 服务") {
		t.Fatalf("a subscription's update changed the routing: %v", a.routes())
	}

	if err := a.setImportedRouting(id); err != nil {
		t.Fatal(err)
	}
	a.refresh()

	// The groups are as written, a group inside a group.
	px := coreProxies(t, a)
	if got := px["Streaming"].All; !slices.Equal(got, []string{"Japan", "DIRECT"}) {
		t.Errorf("Streaming = %v", got)
	}
	if got := px["Japan"].All; !slices.Equal(got, []string{"JP-01", "JP-02"}) {
		t.Errorf("Japan = %v", got)
	}
	if px["Japan"].Type != "URLTest" || px["Streaming"].Type != "Selector" {
		t.Errorf("types %s %s", px["Japan"].Type, px["Streaming"].Type)
	}

	// Routes are the groups rules point at; MATCH's is the default.
	if d := a.route(routes.Default); d.Name != "Japan" || d.Direct {
		t.Errorf("default = %+v", d)
	}
	snap := a.snapshot()
	if snap.Imported == nil || len(snap.Imported.Groups) != 2 || snap.Imported.PausedRules != 1 {
		t.Fatalf("imported = %+v", snap.Imported)
	}
	if snap.RuleCount != 2 {
		t.Errorf("rule count = %d", snap.RuleCount)
	}
	if snap.Exit == nil || !strings.HasPrefix(snap.Exit.Node, "JP-0") || providerOf(snap.Exit.Node) != id {
		t.Errorf("the exit is not a listed node: %+v", snap.Exit)
	}
	sid := a.routeOfGroup("Streaming", "x")
	if sid == "" || sid == routes.Default {
		t.Errorf("Streaming's route = %q", sid)
	}

	// A select group changes; another kind doesn't.
	if err := a.selectMember("Streaming", "DIRECT"); err != nil {
		t.Fatal(err)
	}
	if now := coreProxies(t, a)["Streaming"].Now; now != "DIRECT" {
		t.Errorf("Streaming now = %s", now)
	}
	if err := a.selectMember("Japan", "JP-02"); err == nil {
		t.Error("the core chooses in a url-test group")
	}

	// The rule hits the group.
	s := a.settings.Get()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var snapc mihomo.Snapshot
		_ = a.coreCall(5e9, func(ctx context.Context, c *mihomo.Client) (err error) { snapc, err = c.Connections(ctx); return err })
		b, _ := json.Marshal(snapc)
		_, _ = w.Write(b)
	}))
	defer target.Close()
	pu, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(s.MixedPort))
	hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu), DisableKeepAlives: true}}
	resp, err := hc.Get("http://netflix.com:" + target.URL[strings.LastIndex(target.URL, ":")+1:])
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got mihomo.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(got.Connections, func(c mihomo.Connection) bool {
		return c.Rule == "DomainSuffix" && c.RulePayload == "netflix.com" && slices.Contains(c.Chains, "Streaming") && c.Chains[0] == "DIRECT"
	}) {
		t.Errorf("no connection through the group: %+v", got.Connections)
	}

	// The user's rules wait.
	if _, err := a.addRule(routes.UserRule{Kind: routes.KindDomain, Value: "x.example", Route: routes.Default}); err == nil {
		t.Error("a rule can't take effect now and must not be taken")
	}

	// An update that would break the routing leaves it as it was.
	before, _ := os.ReadFile(a.providers.Path(id))
	for name, bad := range map[string]string{
		"only nodes": "proxies:\n  - {name: JP-01, type: direct}\n",
		"dangling":   strings.Replace(issue33Sub, "[Japan, DIRECT]", "[Gone, DIRECT]", 1),
		"no match":   strings.Replace(issue33Sub, "MATCH,Japan", "MATCH,Gone", 1),
	} {
		serve(bad)
		if err := a.updateProvider(ctx, id); err == nil {
			t.Errorf("%s: applied", name)
		}
		if now, _ := os.ReadFile(a.providers.Path(id)); string(now) != string(before) {
			t.Errorf("%s: the last good file was replaced", name)
		}
		if p := mustProvider(t, a, id); p.Error == "" {
			t.Errorf("%s: nothing says so", name)
		}
		if !hasRoute(a, "Japan") || hasRoute(a, "AI 服务") {
			t.Errorf("%s: the routing changed", name)
		}
	}
	if is := a.importedIssue(a.settings.Get(), a.snapshot()); is == nil || !strings.Contains(is.Text, "没有应用") {
		t.Errorf("issue = %+v", is)
	}

	// A good one replaces the groups; the choice in a group stays.
	serve(strings.Replace(issue33Sub, "rules:", "  - {name: Extra, type: select, proxies: [JP-02]}\nrules:", 1))
	if err := a.updateProvider(ctx, id); err != nil {
		t.Fatal(err)
	}
	if p := mustProvider(t, a, id); p.Error != "" {
		t.Errorf("error = %s", p.Error)
	}
	px = coreProxies(t, a)
	if _, ok := px["Extra"]; !ok {
		t.Error("the update's new group is missing")
	}
	if px["Streaming"].Now != "DIRECT" {
		t.Errorf("the choice was lost: %s", px["Streaming"].Now)
	}

	// Disabled, the template takes over; enabled, the source again.
	if err := a.setProviderEnabled(id, false); err != nil {
		t.Fatal(err)
	}
	if hasRoute(a, "Japan") || !hasRoute(a, "AI 服务") {
		t.Errorf("disabled: %v", a.routes())
	}
	if err := a.setProviderEnabled(id, true); err != nil {
		t.Fatal(err)
	}
	if !hasRoute(a, "Japan") {
		t.Error("enabled: the routing didn't come back")
	}

	// Back to bowel's, with the user's rules where they were.
	if err := a.setImportedRouting(""); err != nil {
		t.Fatal(err)
	}
	if hasRoute(a, "Japan") || len(a.settings.Get().Rules) != 1 {
		t.Errorf("back: %v, %v", a.routes(), a.settings.Get().Rules)
	}
	if _, ok := coreProxies(t, a)["Japan"]; ok {
		t.Error("the source's groups stayed in the core")
	}

	// Deleting the source ends its use for good.
	if err := a.setImportedRouting(id); err != nil {
		t.Fatal(err)
	}
	if err := a.deleteProvider(id); err != nil {
		t.Fatal(err)
	}
	if a.settings.Get().ImportedRouting != "" || hasRoute(a, "Japan") {
		t.Errorf("deleted: %q", a.settings.Get().ImportedRouting)
	}
}

// TestImportedGlobalAndDirect: the modes override the source's rules as they
// do the route groups'.
func TestImportedGlobalAndDirect(t *testing.T) {
	a, id, _ := importedApp(t)
	if err := a.setImportedRouting(id); err != nil {
		t.Fatal(err)
	}
	if err := a.setMode("global"); err != nil {
		t.Fatal(err)
	}
	if now := coreProxies(t, a)["GLOBAL"].Now; now != "Japan" {
		t.Errorf("global = %s", now)
	}
	if err := a.setMode("direct"); err != nil {
		t.Fatal(err)
	}
	if a.snapshot().Mode != "direct" {
		t.Error("mode")
	}
}

func TestImportedRefusedWithAReason(t *testing.T) {
	a, _ := routingApp(t, nil)
	v, err := a.addProvider(context.Background(), "proxies:\n  - {name: A, type: direct}\nrule-providers:\n  r: {type: file, path: ./r.yaml, behavior: domain}\nproxy-groups:\n  - {name: G, type: select, proxies: [A]}\nrules:\n  - RULE-SET,r,G\n  - MATCH,G\n")
	if err != nil {
		t.Fatal(err)
	}
	if v.Routing.Problem == "" || v.Routing.Groups != 0 {
		t.Errorf("routing = %+v", v.Routing)
	}
	if err := a.setImportedRouting(v.ID); err == nil || !strings.Contains(err.Error(), "本地文件") {
		t.Errorf("err = %v", err)
	}
	if a.settings.Get().ImportedRouting != "" {
		t.Error("stayed set")
	}
}

func TestImportedRoutesOfDirectDefault(t *testing.T) {
	a, _ := routingApp(t, nil)
	v, err := a.addProvider(context.Background(), "proxies:\n  - {name: A, type: direct}\nproxy-groups:\n  - {name: G, type: select, proxies: [A, DIRECT]}\nrules:\n  - DOMAIN,a.example,G\n  - DOMAIN,b.example,REJECT\n  - MATCH,DIRECT\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.setImportedRouting(v.ID); err != nil {
		t.Fatal(err)
	}
	all := a.routes()
	var ids []string
	for _, r := range all {
		ids = append(ids, r.ID)
	}
	if !slices.Equal(ids, []string{routes.Default, "g1", routes.Reject}) {
		t.Fatalf("routes = %v", ids)
	}
	if d := a.route(routes.Default); !d.Direct || routes.Group(d) != "DIRECT" {
		t.Errorf("default = %+v", d)
	}
	a.refresh()
	if ex := a.snapshot().Exit; ex != nil {
		t.Errorf("exit = %+v", ex)
	}
}

func mustProvider(t *testing.T, a *App, id string) provider.Provider {
	t.Helper()
	p, ok := a.providers.Get(id)
	if !ok {
		t.Fatalf("no provider %s", id)
	}
	return p
}
