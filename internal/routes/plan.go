package routes

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/oboo/moho/internal/i18n"
)

// The strategies of a route group.
const (
	ModeAuto  = "auto"  // the fastest node of a range, moving on when it fails
	ModeOrder = "order" // the first that works of an ordered list
	ModeFixed = "fixed" // one node, and nothing automatic
)

// Modes are the strategies, in the order the interface offers them.
var Modes = []string{ModeAuto, ModeOrder, ModeFixed}

// Policy is how a route group chooses its node. Each strategy keeps its
// settings while another is in use.
type Policy struct {
	// Mode is empty until the core has said what the group was on before
	// this field existed (see App.adoptSelections); until then it acts as
	// ModeAuto.
	Mode      string   `json:"mode,omitempty"`
	Regions   []string `json:"regions,omitempty"`   // ModeAuto: country codes; none is all
	Providers []string `json:"providers,omitempty"` // ModeAuto: provider ids; none is all
	Order     []string `json:"order,omitempty"`     // ModeOrder: node names, first preferred
	Fixed     string   `json:"fixed,omitempty"`     // ModeFixed: a node name
}

// Scoped says whether the automatic group picks from less than every node.
func (p Policy) Scoped() bool { return len(p.Regions) > 0 || len(p.Providers) > 0 }

func (p Policy) clone() Policy {
	p.Regions, p.Providers, p.Order = slices.Clone(p.Regions), slices.Clone(p.Providers), slices.Clone(p.Order)
	return p
}

// GroupConf is a route group as the user left it.
type GroupConf struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"` // what the user calls it; "" keeps a built-in's
	Policy Policy `json:"policy"`
}

// ServiceConf is what differs from what Moho ships for a built-in service,
// or the whole of a service the user made.
type ServiceConf struct {
	ID       string   `json:"id"`
	Custom   bool     `json:"custom,omitempty"`
	Name     string   `json:"name,omitempty"`     // Custom
	Group    string   `json:"group,omitempty"`    // where it lives; "" is a built-in's own group
	Off      bool     `json:"off,omitempty"`      // switched off: its traffic falls through
	OffRules []string `json:"offRules,omitempty"` // a built-in's rules switched off
	// Source makes a Custom service a rule set: its rules come from this
	// link or file (internal/ruleset), and its id names the set.
	Source string `json:"source,omitempty"`
}

// Plan is what the user changed about routing. Built-in services stay in
// the code, so that what a new version ships arrives; only differences
// from them are kept.
type Plan struct {
	// Groups are every route group, in display order; empty until the
	// first change, when they are the template's.
	Groups   []GroupConf   `json:"groups,omitempty"`
	Services []ServiceConf `json:"services,omitempty"`
}

// Clone is a deep copy.
func (p Plan) Clone() Plan {
	p.Groups, p.Services = slices.Clone(p.Groups), slices.Clone(p.Services)
	for i := range p.Groups {
		p.Groups[i].Policy = p.Groups[i].Policy.clone()
	}
	for i := range p.Services {
		p.Services[i].OffRules = slices.Clone(p.Services[i].OffRules)
	}
	return p
}

func (p *Plan) ensure() {
	if len(p.Groups) == 0 {
		for _, t := range Template() {
			p.Groups = append(p.Groups, GroupConf{ID: t.ID})
		}
	}
	p.Groups = withFixed(p.Groups)
}

// fixedGroups always exist and keep their names.
var fixedGroups = []string{Default, "direct", Reject}

func fixedGroup(id string) bool { return slices.Contains(fixedGroups, id) }

// withFixed is confs with the fixed groups it lacks, even if the file says
// otherwise or was saved before there was one, at the end.
func withFixed(confs []GroupConf) []GroupConf {
	for _, id := range fixedGroups {
		if !slices.ContainsFunc(confs, func(g GroupConf) bool { return g.ID == id }) {
			confs = append(slices.Clone(confs), GroupConf{ID: id})
		}
	}
	return confs
}

func (p *Plan) group(id string) *GroupConf {
	p.ensure()
	for i := range p.Groups {
		if p.Groups[i].ID == id {
			return &p.Groups[i]
		}
	}
	return nil
}

func (p *Plan) service(id string) *ServiceConf {
	for i := range p.Services {
		if p.Services[i].ID == id {
			return &p.Services[i]
		}
	}
	p.Services = append(p.Services, ServiceConf{ID: id})
	return &p.Services[len(p.Services)-1]
}

func (p Plan) findService(id string) ServiceConf {
	for _, s := range p.Services {
		if s.ID == id {
			return s
		}
	}
	return ServiceConf{}
}

// tidy forgets a built-in's entry once nothing differs.
func (p *Plan) tidy() {
	p.Services = slices.DeleteFunc(p.Services, func(s ServiceConf) bool {
		return !s.Custom && s.Group == "" && !s.Off && len(s.OffRules) == 0
	})
}

func builtinOf(id string) (builtin, bool) {
	for _, b := range builtins {
		if b.id == id {
			return b, true
		}
	}
	return builtin{}, false
}

// Build is the routing as it stands: every route group with its services
// and rules, built-ins merged with the user's changes. The routes' Rules
// are the live built-in rules; users are the user's own.
func Build(p Plan, users []UserRule) []Route {
	tpl := Template()
	confs := p.Groups
	if len(confs) == 0 {
		for _, t := range tpl {
			confs = append(confs, GroupConf{ID: t.ID})
		}
	}
	confs = withFixed(confs)
	out := make([]Route, 0, len(confs))
	builtinGroup := map[string]bool{}
	for _, g := range confs {
		r := Route{ID: g.ID, Name: g.ID, Custom: true}
		if i := slices.IndexFunc(tpl, func(t Route) bool { return t.ID == g.ID }); i >= 0 {
			r = tpl[i]
			r.Rules = nil
			builtinGroup[g.ID] = true
		}
		r.Label, r.Policy = g.Name, g.Policy.clone()
		if r.Policy.Mode == "" {
			r.Policy.Mode = ModeAuto
		}
		out = append(out, r)
	}
	at := func(id string) int { return slices.IndexFunc(out, func(r Route) bool { return r.ID == id }) }
	moved := map[string]bool{}
	for _, b := range builtins {
		cf := p.findService(b.id)
		i := at(cmp.Or(cf.Group, b.group))
		if i < 0 {
			i = at(Default)
		}
		if out[i].ID != b.group {
			moved[out[i].ID], moved[b.group] = true, true
		}
		if cf.Off {
			moved[out[i].ID] = true
		}
		s := Service{ID: b.id, Name: b.name, Group: out[i].ID, Builtin: true, Off: cf.Off,
			Changed: cf.Off || out[i].ID != b.group || len(cf.OffRules) > 0}
		for _, text := range b.rules {
			r := ruleOf(text)
			r.Builtin, r.Off = true, slices.Contains(cf.OffRules, text)
			s.Rules = append(s.Rules, r)
		}
		out[i].Services = append(out[i].Services, s)
	}
	for _, cf := range p.Services {
		if !cf.Custom {
			continue
		}
		i := at(cf.Group)
		if i < 0 {
			i = at(Default)
		}
		moved[out[i].ID] = true
		out[i].Services = append(out[i].Services, Service{ID: cf.ID, Name: cf.Name, Group: out[i].ID, Off: cf.Off, Source: cf.Source})
	}
	for i := range out {
		out[i].Services = append(out[i].Services, Service{ID: MineID(out[i].ID), Group: out[i].ID, Mine: true})
	}
	for _, u := range users {
		i := at(u.Route)
		if i < 0 {
			continue
		}
		list := out[i].Services
		j := slices.IndexFunc(list, func(s Service) bool { return s.ID == u.Service })
		if j < 0 {
			j = len(list) - 1 // 我添加的
		}
		list[j].Rules = append(list[j].Rules, Rule{Text: u.Mihomo(), Kind: u.Kind, Value: u.Value})
		if list[j].Builtin {
			list[j].Changed = true
		}
	}
	for i := range out {
		r := &out[i]
		var names []string
		for _, s := range r.Services {
			if !s.Builtin || s.Off {
				if !s.Mine && !s.Off {
					names = append(names, ServiceTitle(s))
				}
				continue
			}
			names = append(names, ServiceTitle(s))
			for _, l := range s.Live() {
				if l.Builtin { // the user's own come from the rules themselves
					r.Rules = append(r.Rules, l.Text)
				}
			}
		}
		if r.ID != Default && (r.Custom || moved[r.ID]) {
			r.Summary = i18n.T("还没有服务", "No services yet")
			if len(names) > 0 {
				r.Summary = strings.Join(names[:min(3, len(names))], " · ")
				if len(names) > 3 {
					r.Summary += i18n.T(" 等", " and more")
				}
			}
		}
	}
	return out
}

// Active are the user's rules that count now: those of a service switched
// off, or of a group that is gone, do not.
func Active(all []Route, users []UserRule) []UserRule {
	out := make([]UserRule, 0, len(users))
	for _, u := range users {
		i := slices.IndexFunc(all, func(r Route) bool { return r.ID == u.Route })
		if i < 0 {
			continue
		}
		j := slices.IndexFunc(all[i].Services, func(s Service) bool { return s.ID == u.Service })
		if j >= 0 && all[i].Services[j].Off {
			continue
		}
		out = append(out, u)
	}
	return out
}

// Line is one rule in the order the core tries them.
type Line struct {
	Text  string
	Route string
	Mine  bool
	Set   string // the id of the rule set this line stands for, if it does
}

// RuleSetProvider is the name of a rule set's rule-provider in the core.
func RuleSetProvider(id string) string { return "rs-" + id }

// rank orders rules so that there is no priority to manage: the more
// specific rule goes first. A whole name, then longer suffixes, longer
// keywords, the built-in domain lists, longer address prefixes, the
// built-in address lists.
func rank(text string) (class, length int) {
	kind, rest, _ := strings.Cut(text, ",")
	value, _, _ := strings.Cut(rest, ",")
	switch kind {
	case "DOMAIN":
		return 0, 0
	case "DOMAIN-SUFFIX":
		return 1, -len(value)
	case "DOMAIN-KEYWORD":
		return 2, -len(value)
	case "GEOSITE":
		return 3, 0
	case "IP-CIDR", "IP-CIDR6":
		_, bits, _ := strings.Cut(value, "/")
		n, _ := strconv.Atoi(bits)
		return 4, -n
	}
	return 5, 0
}

// Lines are all rules in the order the core tries them. Of two equally
// specific rules the user's goes first, then the routes' in their order.
//
// A rule set's lines can't be sorted in by how specific they are, so with
// one in use the order is the user's rules, the sets, what ships. Without
// one nothing changes for anybody.
func Lines(all []Route, users []UserRule) []Line {
	var mine, sets, shipped []Line
	for _, u := range users {
		if slices.ContainsFunc(all, func(r Route) bool { return r.ID == u.Route }) {
			mine = append(mine, Line{Text: u.Mihomo(), Route: u.Route, Mine: true})
		}
	}
	for _, r := range all {
		for _, t := range r.Rules {
			shipped = append(shipped, Line{Text: t, Route: r.ID})
		}
		for _, s := range r.Services {
			if s.Source != "" && !s.Off {
				sets = append(sets, Line{Text: "RULE-SET," + RuleSetProvider(s.ID) + ",no-resolve", Route: r.ID, Set: s.ID})
			}
		}
	}
	if len(sets) == 0 {
		return sorted(append(mine, shipped...))
	}
	return slices.Concat(sorted(mine), sets, sorted(shipped))
}

func sorted(lines []Line) []Line {
	slices.SortStableFunc(lines, func(a, b Line) int {
		ca, la := rank(a.Text)
		cb, lb := rank(b.Text)
		return cmp.Or(cmp.Compare(ca, cb), cmp.Compare(la, lb))
	})
	return lines
}

// FindSource is the rule set that comes from source.
func FindSource(all []Route, source string) (Service, bool) {
	for _, r := range all {
		for _, s := range r.Services {
			if s.Source == source {
				return s, true
			}
		}
	}
	return Service{}, false
}

// FindRule finds the service a rule is in, off or not.
func FindRule(all []Route, text string) (Service, bool) {
	for _, r := range all {
		for _, s := range r.Services {
			if slices.ContainsFunc(s.Rules, func(l Rule) bool { return l.Text == text && !(l.Builtin && l.Off) }) {
				return s, true
			}
		}
	}
	return Service{}, false
}

// FindMatched is the service holding the rule the core reports having
// matched, by the core's name for its type: DomainSuffix, GeoIP…
func FindMatched(all []Route, ruleType, payload string) (Service, bool) {
	t := strings.ToLower(ruleType)
	for _, r := range all {
		for _, s := range r.Services {
			if t == "ruleset" && s.Source != "" && payload == RuleSetProvider(s.ID) {
				return s, true
			}
			for _, l := range s.Rules {
				if l.Off && l.Builtin {
					continue
				}
				geo := l.Kind == KindGeo && t == "geosite" || l.Kind == KindGeoIP && t == "geoip"
				if geo && strings.EqualFold(payload, l.Value) || !geo && (UserRule{Kind: l.Kind, Value: l.Value}).Matched(ruleType, payload) {
					return s, true
				}
			}
		}
	}
	return Service{}, false
}

// ---- Changes ----

// Policy is route group id's policy as stored; zero if there is none.
func (p Plan) Policy(id string) Policy {
	for _, g := range p.Groups {
		if g.ID == id {
			return g.Policy
		}
	}
	return Policy{}
}

// ChangePolicy edits route group id's policy.
func (p *Plan) ChangePolicy(id string, fn func(*Policy)) {
	if g := p.group(id); g != nil {
		fn(&g.Policy)
	}
}

// SetStrategy switches group id to mode. A strategy that has nothing to
// work with yet starts from the node in use.
func (p *Plan) SetStrategy(id, mode, current string) {
	p.ChangePolicy(id, func(q *Policy) {
		if mode == ModeFixed && q.Fixed == "" {
			q.Fixed = current
		}
		if mode == ModeOrder && len(q.Order) == 0 && current != "" {
			q.Order = []string{current}
		}
		q.Mode = mode
	})
}

// PickNode pins group id to node; "" goes back to the fastest.
func (p *Plan) PickNode(id, node string) {
	p.ChangePolicy(id, func(q *Policy) {
		if node == "" {
			q.Mode = ModeAuto
			return
		}
		q.Mode, q.Fixed = ModeFixed, node
	})
}

func (p *Plan) freeID(prefix string, taken func(string) bool) string {
	for i := 1; ; i++ {
		if id := prefix + strconv.Itoa(i); !taken(id) {
			return id
		}
	}
}

// uniqueName is name, numbered if another has it.
func uniqueName(name string, taken func(string) bool) string {
	n := name
	for i := 2; taken(n); i++ {
		n = fmt.Sprintf("%s %d", name, i)
	}
	return n
}

// groupTaken says whether a group other than id is called name.
func groupTaken(all []Route, id, name string) bool {
	return slices.ContainsFunc(all, func(r Route) bool { return r.ID != id && Title(r) == name })
}

// insertGroup adds a group before 直连, whose rules are the last resort.
func (p *Plan) insertGroup(name string, q Policy) string {
	p.ensure()
	id := p.freeID("g", func(s string) bool {
		return slices.ContainsFunc(p.Groups, func(g GroupConf) bool { return g.ID == s })
	})
	at := slices.IndexFunc(p.Groups, func(g GroupConf) bool { return g.ID == "direct" })
	if at < 0 {
		at = len(p.Groups)
	}
	p.Groups = slices.Insert(p.Groups, at, GroupConf{ID: id, Name: name, Policy: q})
	return id
}

// AddGroup makes a route group: the fastest of all nodes. It returns its id.
func (p *Plan) AddGroup(users []UserRule, name string) string {
	all := Build(*p, users)
	return p.insertGroup(uniqueName(name, func(n string) bool { return groupTaken(all, "", n) }), Policy{Mode: ModeAuto})
}

// GroupNameError says what is wrong with name for group id, "" if nothing.
func GroupNameError(all []Route, id, name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return i18n.T("名字不能为空", "The name can't be empty")
	}
	if groupTaken(all, id, s) {
		return i18n.T("已经有同名的路由组", "There's already a route group by that name")
	}
	return ""
}

// ServiceNameError says what is wrong with name for service id, "" if nothing.
func ServiceNameError(all []Route, id, name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return i18n.T("名字不能为空", "The name can't be empty")
	}
	if serviceTaken(all, id, s) {
		return i18n.T("已经有同名的服务", "There's already a service by that name")
	}
	return ""
}

func serviceTaken(all []Route, id, name string) bool {
	for _, r := range all {
		if slices.ContainsFunc(r.Services, func(s Service) bool { return s.ID != id && !s.Mine && ServiceTitle(s) == name }) {
			return true
		}
	}
	return false
}

// RenameGroup names a group; the fixed groups keep theirs.
func (p *Plan) RenameGroup(id, name string) {
	if g := p.group(id); g != nil && !fixedGroup(id) {
		g.Name = strings.TrimSpace(name)
	}
}

// DeleteGroup removes a group. Its services move to group to, and the
// user's rules go with them; those in 我添加的 join to's. It reports
// whether it did: the fixed groups stay.
func (p *Plan) DeleteGroup(users []UserRule, id, to string) bool {
	if fixedGroup(id) || id == to || p.group(to) == nil || p.group(id) == nil {
		return false
	}
	for _, r := range Build(*p, users) {
		if r.ID != id {
			continue
		}
		for _, s := range r.Services {
			if !s.Mine {
				p.place(s.ID, to)
			}
		}
	}
	for i := range users {
		if users[i].Route == id {
			users[i].Route = to
		}
	}
	p.Groups = slices.DeleteFunc(p.Groups, func(g GroupConf) bool { return g.ID == id })
	p.tidy()
	return true
}

// place records that service id lives in group to.
func (p *Plan) place(id, to string) {
	cf := p.service(id)
	if b, ok := builtinOf(id); ok && b.group == to {
		cf.Group = ""
		return
	}
	cf.Group = to
}

// MoveService moves a service, and the user's rules in it, to group to.
func (p *Plan) MoveService(users []UserRule, id, to string) {
	if p.group(to) == nil {
		return
	}
	p.place(id, to)
	for i := range users {
		if users[i].Service == id {
			users[i].Route = to
		}
	}
	p.tidy()
}

// SplitService gives a service a route group of its own, on the strategy
// of the group it was in. It returns the new group's id.
func (p *Plan) SplitService(users []UserRule, id string) string {
	all := Build(*p, users)
	for _, r := range all {
		for _, s := range r.Services {
			if s.ID != id {
				continue
			}
			name := uniqueName(ServiceTitle(s), func(n string) bool { return groupTaken(all, "", n) })
			g := p.insertGroup(name, r.Policy.clone())
			p.MoveService(users, id, g)
			return g
		}
	}
	return ""
}

// AddService makes an empty service in a group. It returns its id.
func (p *Plan) AddService(users []UserRule, group, name string) string {
	all := Build(*p, users)
	id := p.freeID("s", func(s string) bool {
		return slices.ContainsFunc(p.Services, func(c ServiceConf) bool { return c.ID == s })
	})
	name = uniqueName(name, func(n string) bool { return serviceTaken(all, "", n) })
	p.Services = append(p.Services, ServiceConf{ID: id, Custom: true, Name: name, Group: group})
	return id
}

// AddRuleSet makes a service whose rules come from source. It returns its id.
func (p *Plan) AddRuleSet(users []UserRule, group, name, source string) string {
	id := p.AddService(users, group, name)
	p.service(id).Source = source
	return id
}

// RenameService names a service the user made.
func (p *Plan) RenameService(id, name string) {
	if cf := p.service(id); cf.Custom {
		cf.Name = strings.TrimSpace(name)
	}
	p.tidy()
}

// ToggleService switches a built-in service off or back on.
func (p *Plan) ToggleService(id string) {
	cf := p.service(id)
	cf.Off = !cf.Off
	p.tidy()
}

// ToggleRule switches one of a built-in service's rules off or back on.
func (p *Plan) ToggleRule(id, text string) {
	cf := p.service(id)
	if i := slices.Index(cf.OffRules, text); i >= 0 {
		cf.OffRules = slices.Delete(cf.OffRules, i, i+1)
	} else {
		cf.OffRules = append(cf.OffRules, text)
	}
	p.tidy()
}

// RemoveService takes out a service the user made, or puts a built-in
// back as shipped; either way the rules the user added to it go. It
// returns users without them.
func (p *Plan) RemoveService(users []UserRule, id string) []UserRule {
	p.Services = slices.DeleteFunc(p.Services, func(s ServiceConf) bool { return s.ID == id })
	return slices.DeleteFunc(users, func(u UserRule) bool { return u.Service == id })
}

// Claim makes r the one rule for its traffic: the same rule in a built-in
// service is switched off, since a rule belongs to one service. A rule of
// the user's elsewhere is replaced by Upsert.
func (p *Plan) Claim(users []UserRule, r UserRule) {
	all := Build(*p, users)
	for _, g := range all {
		for _, s := range g.Services {
			if !s.Builtin || s.ID == r.Service {
				continue
			}
			if slices.ContainsFunc(s.Rules, func(l Rule) bool { return l.Builtin && !l.Off && l.Text == r.Mihomo() }) {
				p.ToggleRule(s.ID, r.Mihomo())
			}
		}
	}
}

// Check says what is wrong with a plan.
func (p Plan) Check() error {
	all := Build(p, nil)
	seen := map[string]bool{}
	for _, r := range all {
		t := Title(r)
		if strings.TrimSpace(t) == "" {
			return errors.New(i18n.T("名字不能为空", "The name can't be empty"))
		}
		// 拦截 arrived after people had named groups, so it can share a name.
		if seen[t] && r.ID != Reject {
			return fmt.Errorf(i18n.T("已经有同名的路由组: %s", "There's already a route group called %s"), t)
		}
		seen[t] = true
		if m := r.Policy.Mode; !slices.Contains(Modes, m) {
			return fmt.Errorf(i18n.T("未知的节点策略: %s", "Unknown node strategy: %s"), m)
		}
	}
	return nil
}
