package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/ruleset"
	"github.com/oboo/moho/internal/settings"
)

// hitHost resolves to this machine, by the core's own hosts.
const hitHost = "hit.bowel-rule-test.net"

func withHitHost(s *settings.Settings) {
	s.Hosts = []string{hitHost + "=127.0.0.1"}
	s.DevOff = true // its rules for this Mac come first, as they should
}

// hitOf makes a connection through the core to a local server, held open
// while the core is asked how it matched it.
func hitOf(t *testing.T, a *App) (rule, payload string) {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	proxy, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(a.settings.Get().MixedPort))
	cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}, Timeout: 8 * time.Second}
	go func() {
		if resp, err := cl.Get("http://" + hitHost + ":" + port + "/"); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	for end := time.Now().Add(6 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		var snap mihomo.Snapshot
		if err := a.coreCall(3*time.Second, func(ctx context.Context, c *mihomo.Client) (err error) {
			snap, err = c.Connections(ctx)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		for _, cn := range snap.Connections {
			if cn.Metadata.DestinationPort == port {
				return cn.Rule, cn.RulePayload
			}
		}
	}
	t.Fatal("the core never showed the connection")
	return "", ""
}

// TestRuleSetInTheCore: imported, a set decides connections in the real
// core; updated, the new rules count with no reload; off and gone, it stops.
func TestRuleSetInTheCore(t *testing.T) {
	a, _ := routingApp(t, withHitHost)
	body := "DOMAIN-SUFFIX,only.example\nIP-CIDR,10.99.0.0/16,no-resolve\nUSER-AGENT,skipme*\n"
	list := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
	defer list.Close()
	src := list.URL + "/rules/my-list.yaml"

	c, err := a.checkRuleSet(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Set.Rules) != 2 || c.Set.Skipped != 1 || c.Set.Behavior != ruleset.Classical {
		t.Fatalf("%+v", c.Set)
	}
	id, err := a.importRuleSet(c, "direct")
	if err != nil {
		t.Fatal(err)
	}
	if rule, _ := hitOf(t, a); rule == "RuleSet" {
		t.Fatal("a set that doesn't hold the address caught it")
	}
	sv, ok := routes.FindSource(a.routes(), src)
	if !ok || sv.ID != id || sv.Name != "my-list" || sv.Group != "direct" {
		t.Fatalf("%+v %v", sv, ok)
	}
	v := a.snapshot().RuleSets[id]
	if v.Count != 2 || v.Skipped != 1 || len(v.Preview) != 2 || v.UpdatedAt.IsZero() {
		t.Errorf("view: %+v", v)
	}
	if _, err := a.importRuleSet(c, "ai"); err == nil {
		t.Error("the same source imported twice")
	}

	config := func() string { b, _ := os.ReadFile(a.core.ConfigPath()); return string(b) }
	before := config()
	body = "DOMAIN-SUFFIX," + hitHost + "\n"
	if err := a.updateRuleSet(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	// The core picks it up without a new config.
	if config() != before {
		t.Error("the update rewrote the config")
	}
	want := routes.RuleSetProvider(id)
	if rule, payload := hitOf(t, a); rule != "RuleSet" || payload != want {
		t.Errorf("after the update: %s %s", rule, payload)
	}
	if got, mine := routeAndMine(a, "www."+hitHost); got != "direct" || mine {
		t.Errorf("explain: %s %v", got, mine)
	}

	// A failed update keeps what works, and says so.
	list.Close()
	if err := a.updateRuleSet(context.Background(), id); err == nil {
		t.Fatal("no error from a dead server")
	}
	if v := a.snapshot().RuleSets[id]; v.Error == "" || v.Count != 1 {
		t.Errorf("after failing: %+v", v)
	}
	if rule, payload := hitOf(t, a); rule != "RuleSet" || payload != want {
		t.Errorf("after a failed update: %s %s", rule, payload)
	}

	changeRouting(t, a, func(p *routes.Plan) { p.ToggleService(id) })
	if rule, _ := hitOf(t, a); rule == "RuleSet" {
		t.Error("a set switched off still decides")
	}
	changeRouting(t, a, func(p *routes.Plan) { p.ToggleService(id) })
	if rule, _ := hitOf(t, a); rule != "RuleSet" {
		t.Errorf("switched back on: %s", rule)
	}

	if err := a.updateSettings(func(s *settings.Settings) { s.Rules = s.Routing.RemoveService(s.Rules, id) }); err != nil {
		t.Fatal(err)
	}
	a.ruleSets.Delete(id)
	if _, err := os.Stat(a.ruleSets.Path(id)); !os.IsNotExist(err) {
		t.Errorf("the file stayed: %v", err)
	}
	if rule, _ := hitOf(t, a); rule == "RuleSet" {
		t.Error("a deleted set still decides")
	}
}

func routeAndMine(a *App, host string) (string, bool) {
	id, _, _, mine, _ := routes.ExplainSets(host, nil, a.routes(), a.matchRuleSet)
	return id, mine
}

// TestRuleSetLeftBehindIsClearedAtStart: a file no service names goes, as
// after a crash between saving the file and the settings.
func TestRuleSetLeftBehindIsClearedAtStart(t *testing.T) {
	a, _ := routingApp(t, nil)
	set, err := ruleset.Parse([]byte("x.example\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ruleSets.Save("orphan", &ruleset.Checked{Set: set}); err != nil {
		t.Fatal(err)
	}
	a.keepRuleSets()
	if _, ok := a.ruleSets.Get("orphan"); ok || strings.Contains(a.ruleSets.Path("orphan"), "..") {
		t.Error("orphan kept")
	}
}

// TestRuleSetOrderInTheCore: of two sets holding the same address, the one
// listed first decides in the real core. A broad set imported first and a
// narrow one imported into a newer group are put the other way round by
// moving the group, and two sets of one group by moving the set.
func TestRuleSetOrderInTheCore(t *testing.T) {
	a, _ := routingApp(t, withHitHost)
	list := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "DOMAIN-SUFFIX,"+hitHost+"\n")
	}))
	defer list.Close()
	add := func(name, group string) string {
		t.Helper()
		c, err := a.checkRuleSet(context.Background(), list.URL+"/"+name+".yaml")
		if err != nil {
			t.Fatal(err)
		}
		id, err := a.importRuleSet(c, group)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	decides := func(what, id, route string) {
		t.Helper()
		if rule, payload := hitOf(t, a); rule != "RuleSet" || payload != routes.RuleSetProvider(id) {
			t.Errorf("%s: %s %s decides, want the set %s", what, rule, payload, id)
		}
		if got, _ := routeAndMine(a, hitHost); got != route {
			t.Errorf("%s: explain says %q, want %q", what, got, route)
		}
	}
	broad := add("broad", "ai")
	var g string
	changeRouting(t, a, func(p *routes.Plan) { g = p.AddGroup(nil, "OpenAI") })
	narrow := add("narrow", g)
	decides("as imported", broad, "ai")
	changeRouting(t, a, func(p *routes.Plan) { p.MoveGroup(g, "ai") })
	decides("the newer group moved above", narrow, g)
	changeRouting(t, a, func(p *routes.Plan) { p.MoveGroup(g, "dev") })
	decides("the newer group moved back below", broad, "ai")

	second := add("second", "ai")
	decides("a second set in the group", broad, "ai")
	changeRouting(t, a, func(p *routes.Plan) { p.MoveRuleSet(second, broad) })
	decides("the second set moved above the first", second, "ai")
}
