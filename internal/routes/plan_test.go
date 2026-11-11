package routes

import (
	"slices"
	"testing"
)

func groupOf(t *testing.T, all []Route, id string) Route {
	t.Helper()
	for _, r := range all {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no group %q", id)
	return Route{}
}

func serviceIn(r Route, id string) (Service, bool) {
	i := slices.IndexFunc(r.Services, func(s Service) bool { return s.ID == id })
	if i < 0 {
		return Service{}, false
	}
	return r.Services[i], true
}

func lineTexts(all []Route, users []UserRule) []string {
	var out []string
	for _, l := range Lines(all, users) {
		out = append(out, l.Text+"→"+l.Route)
	}
	return out
}

// TestBuildMatchesTemplate: with nothing changed the tree carries the
// template: the same groups, each with its services.
func TestBuildMatchesTemplate(t *testing.T) {
	all := Build(Plan{}, nil)
	var ids []string
	for _, r := range Template() {
		ids = append(ids, r.ID)
	}
	var got []string
	for _, r := range all {
		got = append(got, r.ID)
	}
	if !slices.Equal(ids, got) {
		t.Fatalf("groups %q, want %q", got, ids)
	}
	if _, ok := serviceIn(groupOf(t, all, "ai"), "claude"); !ok {
		t.Errorf("ai has no Claude: %+v", groupOf(t, all, "ai").Services)
	}
}

// TestRulesBySpecificity: more specific rules go first and the user's win
// ties, whichever group they are in.
func TestRulesBySpecificity(t *testing.T) {
	users := []UserRule{
		{Kind: KindSuffix, Value: "api.anthropic.com", Route: "direct"},
		{Kind: KindDomain, Value: "x.example.com", Route: "default"},
		{Kind: KindKeyword, Value: "tracker", Route: "direct"},
	}
	lines := lineTexts(Build(Plan{}, users), users)
	at := func(s string) int { return slices.Index(lines, s) }
	long, short := at("DOMAIN-SUFFIX,api.anthropic.com→direct"), at("DOMAIN-SUFFIX,anthropic.com→ai")
	exact, kw, cn := at("DOMAIN,x.example.com→default"), at("DOMAIN-KEYWORD,tracker→direct"), at("GEOSITE,cn→direct")
	if long < 0 || short < 0 || exact < 0 || kw < 0 || cn < 0 {
		t.Fatalf("missing lines: %q", lines)
	}
	if !(exact < long && long < short && short < kw && kw < cn) {
		t.Errorf("order: exact %d, long %d, short %d, keyword %d, geo %d", exact, long, short, kw, cn)
	}
}

// TestNoDuplicateLines: a user's rule in a built-in service is one line.
func TestNoDuplicateLines(t *testing.T) {
	users := []UserRule{{Kind: KindSuffix, Value: "claude.example", Route: "ai", Service: "claude"}}
	all := Build(Plan{}, users)
	n := 0
	for _, l := range lineTexts(all, users) {
		if l == "DOMAIN-SUFFIX,claude.example→ai" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the rule appears %d times", n)
	}
	s, ok := serviceIn(groupOf(t, all, "ai"), "claude")
	if !ok || !slices.ContainsFunc(s.Rules, func(r Rule) bool { return r.Value == "claude.example" && !r.Builtin }) {
		t.Errorf("not in its service: %+v", s)
	}
}

// TestOffServiceLeavesLines: a service off gives up its rules, so its
// traffic falls through; one rule of a service can be off on its own.
func TestOffServiceLeavesLines(t *testing.T) {
	var p Plan
	p.ToggleService("claude")
	if l := lineTexts(Build(p, nil), nil); slices.Contains(l, "DOMAIN-SUFFIX,claude.ai→ai") {
		t.Errorf("an off service's rule stayed: %q", l)
	}
	p.ToggleService("claude")
	p.ToggleRule("claude", "DOMAIN-SUFFIX,claude.ai")
	l := lineTexts(Build(p, nil), nil)
	if slices.Contains(l, "DOMAIN-SUFFIX,claude.ai→ai") || !slices.Contains(l, "DOMAIN-SUFFIX,anthropic.com→ai") {
		t.Errorf("one rule off: %q", l)
	}
}

// TestMoveSplitDelete: services move with their rules, a split makes a new
// group that keeps them, and deleting a group hands them on.
func TestMoveSplitDelete(t *testing.T) {
	users := []UserRule{{Kind: KindSuffix, Value: "mine.example", Route: "ai", Service: "claude"}}
	var p Plan
	p.MoveService(users, "claude", "dev")
	all := Build(p, users)
	if _, ok := serviceIn(groupOf(t, all, "dev"), "claude"); !ok {
		t.Fatal("not moved")
	}
	active := Active(all, users)
	if len(active) != 1 || active[0].Route != "dev" {
		t.Errorf("a moved service's own rule goes to %+v", active)
	}
	if l := lineTexts(all, users); !slices.Contains(l, "DOMAIN-SUFFIX,claude.ai→dev") {
		t.Errorf("built-in rule didn't move: %q", l)
	}

	g := p.SplitService(users, "claude")
	all = Build(p, users)
	if _, ok := serviceIn(groupOf(t, all, g), "claude"); !ok {
		t.Fatalf("not split into %q", g)
	}
	if !p.DeleteGroup(users, g, "media") {
		t.Fatal("delete refused")
	}
	all = Build(p, users)
	if _, ok := serviceIn(groupOf(t, all, "media"), "claude"); !ok {
		t.Error("the services weren't handed on")
	}
	if p.DeleteGroup(users, Default, "media") || p.DeleteGroup(users, "direct", "media") {
		t.Error("默认 or 直连 was deleted")
	}
}

// TestCustomGroups: ids are the smallest free, names unique, and a new
// group goes before 直连.
func TestCustomGroups(t *testing.T) {
	var p Plan
	a := p.AddGroup(nil, "新路由组")
	b := p.AddGroup(nil, "新路由组")
	if a != "g1" || b != "g2" {
		t.Fatalf("ids %q %q", a, b)
	}
	all := Build(p, nil)
	if groupOf(t, all, a).Label == groupOf(t, all, b).Label {
		t.Error("two groups share a name")
	}
	if n := len(all); all[n-2].ID != "direct" || all[n-1].ID != Reject {
		t.Errorf("last are %q, %q", all[n-2].ID, all[n-1].ID)
	}
	p.DeleteGroup(nil, a, Default)
	if c := p.AddGroup(nil, "x"); c != "g1" {
		t.Errorf("freed id not reused: %q", c)
	}
	if GroupNameError(all, a, "") == "" || GroupNameError(all, a, "其他流量") == "" || GroupNameError(all, a, "全新") != "" {
		t.Error("group name check")
	}
}

// TestStrategiesKeepTheirSettings: switching strategy loses nothing, and
// picking a node under any strategy fixes on it.
func TestStrategiesKeepTheirSettings(t *testing.T) {
	var p Plan
	p.ChangePolicy("ai", func(q *Policy) {
		q.Mode, q.Regions, q.Order = ModeAuto, []string{"JP"}, []string{"a", "b"}
	})
	p.SetStrategy("ai", ModeFixed, "cur")
	q := p.Policy("ai")
	if q.Mode != ModeFixed || q.Fixed != "cur" || !slices.Equal(q.Regions, []string{"JP"}) || len(q.Order) != 2 {
		t.Errorf("fixed: %+v", q)
	}
	p.SetStrategy("ai", ModeAuto, "other")
	p.PickNode("ai", "n")
	q = p.Policy("ai")
	if q.Mode != ModeFixed || q.Fixed != "n" || !slices.Equal(q.Regions, []string{"JP"}) {
		t.Errorf("picked: %+v", q)
	}
}

// TestClaim: a rule belongs to one service, so adding it elsewhere takes
// it from where a built-in had it.
func TestClaim(t *testing.T) {
	var p Plan
	r := UserRule{Kind: KindSuffix, Value: "claude.ai", Route: "dev", Service: ""}
	p.Claim(nil, r)
	users := []UserRule{r}
	all := Build(p, users)
	s, ok := FindRule(all, "DOMAIN-SUFFIX,claude.ai")
	if !ok || s.Group != "dev" {
		t.Errorf("claim landed in %+v", s)
	}
	if l := lineTexts(all, users); slices.Contains(l, "DOMAIN-SUFFIX,claude.ai→ai") {
		t.Errorf("the built-in kept it: %q", l)
	}
}

func TestFindMatched(t *testing.T) {
	all := Build(Plan{}, nil)
	for _, c := range []struct{ typ, payload, want string }{
		{"DomainSuffix", "claude.ai", "claude"},
		{"GeoSite", "cn", "cn"},
		{"GeoIP", "CN", "cn"},
	} {
		if s, ok := FindMatched(all, c.typ, c.payload); !ok || s.ID != c.want {
			t.Errorf("%s,%s = %+v %v", c.typ, c.payload, s, ok)
		}
	}
	if _, ok := FindMatched(all, "Match", ""); ok {
		t.Error("Match has a home")
	}
}

// TestRejectGroup: a plan saved before the group existed gets it, services
// can move in, and it can be neither renamed nor deleted.
func TestRejectGroup(t *testing.T) {
	p := Plan{Groups: []GroupConf{{ID: Default}, {ID: "ai"}, {ID: "g1", Name: "拦截"}, {ID: "direct"}}}
	r := groupOf(t, Build(p, nil), Reject)
	if !r.Reject || !r.Direct {
		t.Fatalf("group %+v", r)
	}
	if err := p.Check(); err != nil {
		t.Errorf("a group the user named the same: %v", err)
	}
	p.MoveService(nil, "openai", Reject)
	if svc := groupOf(t, Build(p, nil), Reject).Services; !slices.ContainsFunc(svc, func(s Service) bool { return s.ID == "openai" }) {
		t.Errorf("service didn't move in: %+v", svc)
	}
	p.RenameGroup(Reject, "x")
	if g := groupOf(t, Build(p, nil), Reject); g.Label != "" {
		t.Errorf("renamed: %q", g.Label)
	}
	if p.DeleteGroup(nil, Reject, Default) {
		t.Error("deleted")
	}
	if err := ValidTargets(Reject, []Target{{"a", "https://a.example"}}); err == nil {
		t.Error("a group with no node took test sites")
	}
}

func withRuleSet(t *testing.T, group string) (Plan, string) {
	t.Helper()
	var p Plan
	return p, p.AddRuleSet(nil, group, "AI list", "https://x.example/ai.yaml")
}

func TestRuleSetIsAServiceOfItsGroup(t *testing.T) {
	p, id := withRuleSet(t, "ai")
	all := Build(p, nil)
	s, ok := FindSource(all, "https://x.example/ai.yaml")
	if !ok || s.ID != id || s.Group != "ai" || s.Builtin {
		t.Fatalf("%+v %v", s, ok)
	}
	if got, ok := FindMatched(all, "RuleSet", RuleSetProvider(id)); !ok || got.ID != id {
		t.Errorf("a connection the core matched by the set: %+v %v", got, ok)
	}
	p.MoveService(nil, id, "media")
	if s, _ := FindSource(Build(p, nil), "https://x.example/ai.yaml"); s.Group != "media" {
		t.Errorf("moved to %s", s.Group)
	}
	p.ToggleService(id)
	if lines := Lines(Build(p, nil), nil); slices.ContainsFunc(lines, func(l Line) bool { return l.Set == id }) {
		t.Error("a set switched off still has a line")
	}
	if !p.Clone().findService(id).Off || p.findService(id).Source == "" {
		t.Errorf("%+v", p.findService(id))
	}
	if users := p.RemoveService(nil, id); len(users) != 0 || p.findService(id).ID != "" {
		t.Error("not removed")
	}
}

// With a set in use the user's rules come first, whatever kind, and what
// ships last. Without one the order is what it always was.
func TestLinesOrderWithARuleSet(t *testing.T) {
	users := []UserRule{{Kind: KindKeyword, Value: "corp", Route: "direct"}}
	plain := Lines(Build(Plan{}, users), users)
	if slices.IndexFunc(plain, func(l Line) bool { return l.Mine }) == 0 {
		t.Fatal("a keyword should sort after the built-in names when no set is in use")
	}
	p, id := withRuleSet(t, "ai")
	lines := Lines(Build(p, users), users)
	if !lines[0].Mine {
		t.Errorf("first: %+v", lines[0])
	}
	if lines[1].Set != id || lines[1].Text != "RULE-SET,"+RuleSetProvider(id)+",no-resolve" || lines[1].Route != "ai" {
		t.Errorf("second: %+v", lines[1])
	}
	if rest := lines[2:]; slices.ContainsFunc(rest, func(l Line) bool { return l.Mine || l.Set != "" }) {
		t.Error("what ships should be all that's left")
	}
}

func TestExplainLooksInsideRuleSets(t *testing.T) {
	p, id := withRuleSet(t, "media")
	all := Build(p, nil)
	match := func(set, host, ip string) (string, bool) {
		return "DOMAIN-SUFFIX,anthropic.com", set == id && host == "api.anthropic.com"
	}
	r, rule, set, mine, sure := ExplainSets("api.anthropic.com", nil, all, match)
	if r != "media" || rule != "DOMAIN-SUFFIX,anthropic.com" || set != id || mine || !sure {
		t.Errorf("%s %s %s %v %v", r, rule, set, mine, sure)
	}
	// A rule set says nothing: on to what ships.
	if r, _, set, _, _ := ExplainSets("chat.openai.com", nil, all, match); r != "ai" || set != "" {
		t.Errorf("%s %s", r, set)
	}
	// Without a way to look inside, as on mobile.
	if r, _, _, _ := Explain("api.anthropic.com", nil, all); r != "ai" {
		t.Errorf("%s", r)
	}
}

// TestGroupsGoDirect: a route group can go without a node and keeps its
// strategy for when it uses one again. The default route keeps its node
// groups too, for global mode; another keeps a group of its own, so that
// the core says whose the traffic was. 直连 and 拦截 have no such choice.
func TestGroupsGoDirect(t *testing.T) {
	var p Plan
	p.SetStrategy(Default, ModeFixed, "hk")
	p.ChangePolicy(Default, func(q *Policy) { q.Direct = true })
	p.SetStrategy("ai", ModeFixed, "jp")
	p.ChangePolicy("ai", func(q *Policy) { q.Direct = true })
	g := p.AddGroup(nil, "Apple")
	p.ChangePolicy(g, func(q *Policy) { q.Regions, q.Direct = []string{"US"}, true })
	for _, id := range []string{"direct", Reject} { // a hand-edited file
		p.ChangePolicy(id, func(q *Policy) { q.Direct = true })
	}
	all := Build(p, nil)
	route := func(id string) Route { return all[slices.IndexFunc(all, func(r Route) bool { return r.ID == id })] }
	def := route(Default)
	if !def.Direct || !def.NodeGroups() || def.Policy.Mode != ModeFixed || def.Policy.Fixed != "hk" {
		t.Errorf("default: %+v", def)
	}
	if def.OwnExit() {
		t.Error("the default route has a group for going direct: MATCH says whose the traffic is")
	}
	if ai := route("ai"); !ai.Direct || ai.NodeGroups() || !ai.OwnExit() || ai.Policy.Mode != ModeFixed || ai.Policy.Fixed != "jp" {
		t.Errorf("ai: %+v", ai)
	}
	if r := route(g); !r.Direct || r.NodeGroups() || !r.OwnExit() || !slices.Equal(r.Policy.Regions, []string{"US"}) {
		t.Errorf("a group of the user's: %+v", r)
	}
	for _, id := range []string{"direct", Reject} {
		if r := route(id); !r.Direct || r.NodeGroups() || r.OwnExit() || ChoosesExit(id) {
			t.Errorf("%s: %+v", id, r)
		}
	}
	p.ChangePolicy("ai", func(q *Policy) { q.Direct = false })
	if ai := Build(p, nil)[slices.IndexFunc(all, func(r Route) bool { return r.ID == "ai" })]; ai.Direct || !ai.NodeGroups() || ai.OwnExit() || ai.Policy.Fixed != "jp" {
		t.Errorf("ai back on a node: %+v", ai)
	}
	if err := p.Check(); err != nil {
		t.Error(err)
	}
}

// TestDefaultRouteName: the interface says 其他流量; the core's group keeps
// its name, and a group the user already called that doesn't break the plan.
func TestDefaultRouteName(t *testing.T) {
	var p Plan
	id := p.AddGroup(nil, "其他流量 2")
	p.group(id).Name = "其他流量"
	all := Build(p, nil)
	def := all[slices.IndexFunc(all, func(r Route) bool { return r.ID == Default })]
	if Title(def) != "其他流量" || Group(def) != "默认" {
		t.Errorf("title %q, group %q", Title(def), Group(def))
	}
	if err := p.Check(); err != nil {
		t.Error(err)
	}
}

func groupIDs(p Plan) []string {
	var out []string
	for _, g := range p.Groups {
		out = append(out, g.ID)
	}
	return out
}

func setOrder(p Plan) []string {
	var out []string
	for _, l := range Lines(Build(p, nil), nil) {
		if l.Set != "" {
			out = append(out, l.Route+"/"+l.Set)
		}
	}
	return out
}

// TestMoveGroup: a group takes another's place, the ones between shifting
// by one; the fixed groups neither move nor give up their place after the
// others.
func TestMoveGroup(t *testing.T) {
	var p Plan
	g := p.AddGroup(nil, "OpenAI")
	want := func(ids ...string) {
		t.Helper()
		if got := groupIDs(p); !slices.Equal(got, ids) {
			t.Errorf("groups %v, want %v", got, ids)
		}
	}
	want(Default, "ai", "media", "dev", g, "direct", Reject)
	if !p.MoveGroup(g, "ai") {
		t.Error("moving up did nothing")
	}
	want(Default, g, "ai", "media", "dev", "direct", Reject)
	if !p.MoveGroup(g, "media") {
		t.Error("moving down did nothing")
	}
	want(Default, "ai", "media", g, "dev", "direct", Reject)
	if !p.MoveGroup("ai", "dev") {
		t.Error("moving to the end did nothing")
	}
	want(Default, "media", g, "dev", "ai", "direct", Reject)
	for _, c := range [][2]string{{g, g}, {g, "direct"}, {g, Reject}, {g, Default}, {"direct", "ai"}, {Reject, "ai"}, {Default, "ai"}, {g, "nope"}, {"nope", g}} {
		if p.MoveGroup(c[0], c[1]) {
			t.Errorf("%s moved to the place of %s", c[0], c[1])
		}
	}
	want(Default, "media", g, "dev", "ai", "direct", Reject)
	if err := p.Check(); err != nil {
		t.Error(err)
	}
}

// TestRuleSetsGoAsListed: rule sets apply in the order of the list, by
// their group and then their place in it, and the default route's last,
// as it is listed. Moving a group or a set changes it: of a broad set made
// first and a narrow one made later, the narrow one can be put first.
func TestRuleSetsGoAsListed(t *testing.T) {
	var p Plan
	other := p.AddRuleSet(nil, Default, "everything", "https://x.example/all.yaml")
	broad := p.AddRuleSet(nil, "ai", "AI", "https://x.example/ai.yaml")
	g := p.AddGroup(nil, "OpenAI")
	narrow := p.AddRuleSet(nil, g, "OpenAI", "https://x.example/openai.yaml")
	want := func(sets ...string) {
		t.Helper()
		if got := setOrder(p); !slices.Equal(got, sets) {
			t.Errorf("sets %v, want %v", got, sets)
		}
	}
	want("ai/"+broad, g+"/"+narrow, Default+"/"+other)
	p.MoveGroup(g, "ai")
	want(g+"/"+narrow, "ai/"+broad, Default+"/"+other)

	second := p.AddRuleSet(nil, "ai", "AI 2", "https://x.example/ai2.yaml")
	third := p.AddRuleSet(nil, "ai", "AI 3", "https://x.example/ai3.yaml")
	want(g+"/"+narrow, "ai/"+broad, "ai/"+second, "ai/"+third, Default+"/"+other)
	if !p.MoveRuleSet(third, broad) {
		t.Error("moving a set up did nothing")
	}
	want(g+"/"+narrow, "ai/"+third, "ai/"+broad, "ai/"+second, Default+"/"+other)
	if !p.MoveRuleSet(third, second) {
		t.Error("moving a set down did nothing")
	}
	want(g+"/"+narrow, "ai/"+broad, "ai/"+second, "ai/"+third, Default+"/"+other)
	svc := p.AddService(nil, "ai", "mine")
	for _, c := range [][2]string{{third, third}, {third, narrow}, {narrow, third}, {third, svc}, {svc, third}, {third, "openai"}, {"nope", third}} {
		if p.MoveRuleSet(c[0], c[1]) {
			t.Errorf("%s moved to the place of %s", c[0], c[1])
		}
	}
	want(g+"/"+narrow, "ai/"+broad, "ai/"+second, "ai/"+third, Default+"/"+other)

	// What decides between two sets is their order alone.
	inSet := func(id, host, ip string) (string, bool) {
		return "DOMAIN-SUFFIX,openai.com", (id == broad || id == narrow) && host == "api.openai.com"
	}
	if r, _, set, _, _ := ExplainSets("api.openai.com", nil, Build(p, nil), inSet); r != g || set != narrow {
		t.Errorf("with the narrow set first: %s %s", r, set)
	}
	p.MoveGroup(g, "dev")
	if r, _, set, _, _ := ExplainSets("api.openai.com", nil, Build(p, nil), inSet); r != "ai" || set != broad {
		t.Errorf("with the broad set first: %s %s", r, set)
	}
}

// TestGroupOrderBreaksTies: rules stay sorted by how specific they are
// wherever their groups are; of two equally specific ones, the group listed
// first has its rule first.
func TestGroupOrderBreaksTies(t *testing.T) {
	var p Plan
	before := Lines(Build(p, nil), nil)
	p.MoveGroup("dev", "ai")
	after := Lines(Build(p, nil), nil)
	if len(before) != len(after) {
		t.Fatalf("%d lines became %d", len(before), len(after))
	}
	for i := 1; i < len(after); i++ {
		ca, la := rank(after[i-1].Text)
		cb, lb := rank(after[i].Text)
		if ca > cb || ca == cb && la > lb {
			t.Fatalf("%q is before the more specific %q", after[i-1].Text, after[i].Text)
		}
		if ca == cb && la == lb && after[i-1].Route == "ai" && after[i].Route == "dev" {
			t.Fatalf("%q of AI is before the equally specific %q of dev, which is listed first", after[i-1].Text, after[i].Text)
		}
	}
}
