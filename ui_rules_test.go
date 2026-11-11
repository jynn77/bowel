package main

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

// TestActivityRouteMenu: 让它走… offers the site first, the one name in a
// submenu, and, for a connection a rule of the user's matched, taking it
// out.
func TestActivityRouteMenu(t *testing.T) {
	u := actUI()
	u.snap.Settings.Rules = []routes.UserRule{{Kind: routes.KindSuffix, Value: "anthropic.com", Route: "ai"}}
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if err := tt.Click("api.anthropic.com:443"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if !tt.HasText("你的规则") {
		t.Errorf("a connection the user's rule matched isn't marked: %q", tt.Texts())
	}
	if err := tt.Click("让它走…"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	m := tt.Menu()
	shot(t, tt, "activity-route-menu")
	for _, want := range []string{"让 anthropic.com 及其子域名 走：", "其他流量", "AI 服务", "直连", "只让 api.anthropic.com 走…", "删除这条规则", "管理规则…"} {
		if !slices.Contains(m, want) {
			t.Errorf("menu lacks %q: %q", want, m)
		}
	}
	if err := tt.ChooseMenuItem("只让 api.anthropic.com 走…", "开发"); err != nil {
		t.Error(err)
	}
}

// TestRoutesShowMine: the route list counts the user's rules in, and
// offers to manage them.
func TestRoutesShowMine(t *testing.T) {
	u := fixtureUI()
	for i := range u.snap.Routes {
		if u.snap.Routes[i].ID == "ai" {
			u.snap.Routes[i].Mine = 2
			u.snap.Routes[i].Rules += 2
		}
	}
	u.home.x = "routes"
	tt := ui.NewTester(u.view, 1180, 760)
	tt.Advance(2 * time.Second)
	if !hasTextPart(tt, "含你加的 2 条") {
		t.Errorf("no count of the user's rules: %q", tt.Texts())
	}
	shot(t, tt, "home-routes-mine")
	if err := tt.Click("管理路由组"); err != nil {
		t.Fatal(err)
	}
	if u.page != "routes" {
		t.Errorf("管理路由组 went to %q", u.page)
	}
}

// TestSettingsValidateRules: a saved rule must parse back to itself, point
// at a route that exists, and be there once.
func TestSettingsValidateRules(t *testing.T) {
	ok := routes.UserRule{Kind: routes.KindSuffix, Value: "openai.com", Route: "ai"}
	for name, rules := range map[string][]routes.UserRule{
		"route":     {{Kind: routes.KindSuffix, Value: "openai.com", Route: "nowhere"}},
		"twice":     {ok, {Kind: routes.KindSuffix, Value: "openai.com", Route: "dev"}},
		"malformed": {{Kind: routes.KindSuffix, Value: "OpenAI.com", Route: "ai"}},
		"kind":      {{Kind: "regex", Value: "x", Route: "ai"}},
	} {
		s := settings.Defaults.Clone()
		s.Rules = rules
		if s.Validate() == nil {
			t.Errorf("%s: accepted %+v", name, rules)
		}
	}
	s := settings.Defaults.Clone()
	s.Rules = []routes.UserRule{ok, {Kind: routes.KindIP, Value: "10.0.0.0/8", Route: "direct"}}
	if err := s.Validate(); err != nil {
		t.Error(err)
	}
}
