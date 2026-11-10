package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

// switchFixture is a pair of providers: one with several nodes, one that is a
// whole pasted link, and the names the core gives their nodes.
func switchFixture(t *testing.T) (*App, string, string, string) {
	t.Helper()
	ps, err := provider.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, err := settings.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	solo, err := ps.Add(ctx, nil, "trojan://pw@hk.example.com:443#HK01")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := ps.Add(ctx, nil, "trojan://pw@a.example.com:443#A-IPLC\ntrojan://pw@b.example.com:443#B-02")
	if err != nil {
		t.Fatal(err)
	}
	if solo, err = ps.Rename(solo.ID, "Ohio"); err != nil {
		t.Fatal(err)
	}
	if pair, err = ps.Rename(pair.ID, "某 IPLC 机场"); err != nil {
		t.Fatal(err)
	}
	a := &App{providers: ps, settings: st}
	from, to := "A-IPLC"+core.NodeSep+pair.ID, "B-02"+core.NodeSep+pair.ID
	a.provNodes = map[string][]string{
		solo.ID: {"HK01" + core.NodeSep + solo.ID},
		pair.ID: {from, to},
	}
	return a, from, to, "HK01" + core.NodeSep + solo.ID
}

// A notice names a node as "[provider]node", with both names without what the
// user's rules match; only what is matched is dropped, never the whole name.
func TestNodeShown(t *testing.T) {
	a, from, to, soloNode := switchFixture(t)

	if got, want := a.nodeShown(a.provNodes, to), "[某 IPLC 机场]B-02"; got != want {
		t.Errorf("no rule: %q, want %q", got, want)
	}
	// A link provider's only node already goes by the provider's name: it is
	// not said twice.
	if got := a.nodeShown(a.provNodes, soloNode); got != "Ohio" {
		t.Errorf("a one-node link provider: %q", got)
	}
	// DIRECT has no provider, and neither does a name the core gave nothing.
	if got := a.nodeShown(nil, "DIRECT"); got != "DIRECT" {
		t.Errorf("DIRECT = %q", got)
	}

	rules, bad := settings.CompileRules([]string{"IPLC"})
	if len(bad) != 0 {
		t.Fatalf("the rule does not compile: %v", bad)
	}
	a.strip.Store(&rules)
	got := a.nodeShown(a.provNodes, from)
	if strings.Contains(got, "IPLC") {
		t.Errorf("a name the user asked to hide is shown: %q", got)
	}
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "A-") {
		t.Errorf("with a rule: %q", got)
	}
	// The name is matched as shown, zero-width characters and all.
	a.strip.Store(&rules)
	if got := a.nodeShown(a.provNodes, "A-I\u200bPLC"+core.NodeSep+providerOf(from)); strings.Contains(got, "I\u200bPLC") || strings.Contains(got, "IPLC") {
		t.Errorf("a zero-width name slipped through: %q", got)
	}

	// A name a rule takes whole still reads as something.
	rules, _ = settings.CompileRules([]string{"B-02"})
	a.strip.Store(&rules)
	if got := a.nodeShown(a.provNodes, to); !strings.HasSuffix(got, "…") {
		t.Errorf("a name taken whole: %q", got)
	}

	// A provider that is gone leaves the node's own name, not a bare id.
	if err := a.providers.Delete(providerOf(from)); err != nil {
		t.Fatal(err)
	}
	if got := a.nodeShown(a.provNodes, from); strings.HasPrefix(got, "[") || got == providerOf(from) {
		t.Errorf("provider gone: %q", got)
	}
}

func TestSwitchExpiry(t *testing.T) {
	n := &switchNote{at: time.Now()}
	if switchExpired(n, n.at.Add(switchShow-time.Millisecond)) {
		t.Error("expired a moment too early")
	}
	if !switchExpired(n, n.at.Add(switchShow)) {
		t.Error("not expired at the mark")
	}
}

// A note stops telling what happens once the route moved on, was pinned by
// hand, or the node it left answers again.
func TestSwitchStale(t *testing.T) {
	a, from, to, _ := switchFixture(t)
	r := a.route(routes.Default)
	if r.ID == "" {
		t.Fatal("no default route")
	}
	url := a.settings.Get().TargetsOf(r.ID)[0].URL
	px := func(group, auto, fromDelay int) map[string]mihomo.Proxy {
		m := map[string]mihomo.Proxy{
			routes.Group(r): {Now: routes.Auto(r)},
			routes.Auto(r):  {Now: to},
			to:              {Name: to},
			from: {Name: from, Extra: map[string]mihomo.URLTests{
				url: {History: []mihomo.History{{Delay: fromDelay}}},
			}},
		}
		if group != 0 {
			m[routes.Group(r)] = mihomo.Proxy{Now: "pinned"}
		}
		if auto != 0 {
			m[routes.Auto(r)] = mihomo.Proxy{Now: "elsewhere"}
		}
		return m
	}
	n := &switchNote{route: r.ID, from: from, to: to, at: time.Now()}
	if a.switchStale(n, px(0, 0, 0), a.settings.Get()) {
		t.Error("still telling, but called stale")
	}
	if !a.switchStale(n, px(1, 0, 0), a.settings.Get()) {
		t.Error("the group was pinned by hand, and the note stayed")
	}
	if !a.switchStale(n, px(0, 1, 0), a.settings.Get()) {
		t.Error("the group moved again, and the note stayed")
	}
	if !a.switchStale(n, px(0, 0, 120), a.settings.Get()) {
		t.Error("the node it left answers again, and the note stayed")
	}
	if !a.switchStale(&switchNote{route: "no-such-route", from: from, to: to, at: time.Now()}, px(0, 0, 0), a.settings.Get()) {
		t.Error("a route that is gone, and the note stayed")
	}
}

// The note is rendered with the shown names, and dropped — not just hidden —
// when its time is up or traffic is not being taken over.
func TestSwitchForRendersAndClears(t *testing.T) {
	a, from, to, _ := switchFixture(t)
	rules, _ := settings.CompileRules([]string{"IPLC"})
	a.strip.Store(&rules)
	r := a.route(routes.Default)
	url := a.settings.Get().TargetsOf(r.ID)[0].URL
	px := map[string]mihomo.Proxy{
		routes.Group(r): {Now: routes.Auto(r)},
		routes.Auto(r):  {Now: to},
		to:              {Name: to},
		from: {Name: from, Extra: map[string]mihomo.URLTests{
			url: {History: []mihomo.History{{Delay: 0}}},
		}},
	}
	running := core.Status{State: core.Running}

	a.switched = &switchNote{route: r.ID, from: from, to: to, at: time.Now()}
	say := a.switchFor(px, a.provNodes, running, a.settings.Get(), true)
	if say == nil {
		t.Fatal("the switch is not said")
	}
	if !strings.HasPrefix(say.from, "[") || !strings.HasPrefix(say.to, "[") {
		t.Errorf("no provider in the names: %q → %q", say.from, say.to)
	}
	if strings.Contains(say.from+say.to, "IPLC") {
		t.Errorf("a name the user asked to hide is shown: %q → %q", say.from, say.to)
	}

	a.switched = &switchNote{route: r.ID, from: from, to: to, at: time.Now().Add(-switchShow)}
	if say := a.switchFor(px, a.provNodes, running, a.settings.Get(), true); say != nil {
		t.Errorf("said after its time: %+v", say)
	}
	if a.switched != nil {
		t.Error("the note was kept after its time")
	}

	a.switched = &switchNote{route: r.ID, from: from, to: to, at: time.Now()}
	if say := a.switchFor(px, a.provNodes, running, a.settings.Get(), false); say != nil {
		t.Errorf("said with traffic not taken over: %+v", say)
	}
	if a.switched != nil {
		t.Error("the note was kept with traffic not taken over")
	}
}
