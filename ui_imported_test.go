package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func importedFixture() *UI {
	u := fixtureUI()
	node := u.snap.Nodes[0].Name
	u.snap.Imported = &ImportedView{Provider: u.snap.Providers[0].ID, Name: "Full config", Rules: 12, PausedRules: 3, PausedSets: 1, Groups: []GroupView{
		{Name: "Streaming", Kind: "select", Route: "g1", Rules: 9, Now: "Japan", Exit: node, Selectable: true, Members: []MemberView{{Name: "Japan", Group: true, Tested: true, Delay: 88}, {Name: "DIRECT"}}},
		{Name: "Japan", Kind: "url-test", Route: "default", Rules: 3, Now: "JP-01", Exit: node, Members: []MemberView{{Name: "JP-01", Node: node, Tested: true, Delay: 88}, {Name: "JP-02"}}},
		{Name: "Hidden", Kind: "fallback", Now: "JP-02", Exit: "JP-02", Members: []MemberView{{Name: "JP-02"}}},
	}}
	return u
}

func TestImportedPageShowsTheGroups(t *testing.T) {
	u := importedFixture()
	u.page = "routes"
	tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(2 * time.Second)
	shot(t, tt, "imported-routes")
	for _, s := range []string{"Full config", "Streaming", "Japan", "手动选择", "自动最快", "改回 bowel 路由", "DIRECT", "路由组"} {
		if !tt.HasText(s) {
			t.Errorf("page lacks %q: %q", s, tt.Texts())
		}
	}
	if tt.HasText("＋ 新建路由组") {
		t.Error("new route groups are offered while the config's routing is in use")
	}
	// A group only rules' targets lead to is read-only in its own words.
	u.routes.group = "Japan"
	tt = ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(time.Second)
	shot(t, tt, "imported-routes-urltest")
	if !tt.HasText("自动最快 · 3 条规则指向这里 · 由内核自己选择") {
		t.Errorf("url-test group: %q", tt.Texts())
	}
}

func TestProviderOffersItsRouting(t *testing.T) {
	u := fixtureUI()
	u.snap.Providers[0].Routing = ProviderRouting{Groups: 2, Rules: 5}
	u.page = "providers"
	tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(2 * time.Second)
	shot(t, tt, "imported-offer")
	if !tt.HasText("这份配置带 2 个路由组、5 条规则") || !tt.HasText("使用它们") {
		t.Errorf("no offer: %q", tt.Texts())
	}
	u.snap.Providers[0].Routing = ProviderRouting{Groups: 2, Rules: 5, InUse: true}
	tt = ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(time.Second)
	if !tt.HasText("正在使用它的 2 个路由组") || !tt.HasText("改回 bowel 路由") {
		t.Errorf("not in use: %q", tt.Texts())
	}
	u.snap.Providers[0].Routing = ProviderRouting{Problem: "「r」指向本地文件"}
	tt = ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(time.Second)
	if !tt.HasText("带路由组，但不能使用：「r」指向本地文件") {
		t.Errorf("no reason: %q", tt.Texts())
	}
	// A proxy-provider that is a local file can be given one from the row,
	// besides the add bar's own button; a local file can be read again.
	files := func() (n int) {
		for _, s := range tt.Texts() {
			if s == "选择文件…" {
				n++
			}
		}
		return n
	}
	before := files()
	u.snap.Providers[0].Routing = ProviderRouting{Problem: "「nodes」指向本地文件", NeedsFile: "nodes"}
	u.snap.Providers[2].Kind = "file"
	tt = ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(time.Second)
	shot(t, tt, "imported-needs-file")
	if files() != before+1 || !tt.HasText("重新读取") {
		t.Errorf("no file to choose or read again: %q", tt.Texts())
	}
}
