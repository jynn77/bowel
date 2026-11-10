package core

import (
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/oboo/moho/internal/devnet"
	"github.com/oboo/moho/internal/routes"
)

func ruleSetInput(t *testing.T, with bool) Input {
	t.Helper()
	var p routes.Plan
	id := p.AddRuleSet(nil, "ai", "list", "https://x.example/a.yaml")
	in := Input{
		Routes: routes.Build(p, nil), Mode: "rule", MixedPort: 7899, LogLevel: "info", TestURL: "http://x/", ProbePort: 1,
		UserRules: []routes.UserRule{{Kind: routes.KindKeyword, Value: "corp", Route: "direct"}},
	}
	if with {
		in.RuleSets = []RuleSetRef{{ID: id, Path: "/data/rulesets/" + id + ".yaml", Behavior: "classical"}}
	}
	return in
}

func rulesOf(t *testing.T, in Input) (map[string]any, []string) {
	t.Helper()
	b, err := BuildConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	var rules []string
	for _, r := range cfg["rules"].([]any) {
		rules = append(rules, r.(string))
	}
	return cfg, rules
}

func TestRuleSetInConfig(t *testing.T) {
	cfg, rules := rulesOf(t, ruleSetInput(t, true))
	p := cfg["rule-providers"].(map[string]any)["rs-s1"].(map[string]any)
	if p["type"] != "file" || p["behavior"] != "classical" || p["format"] != "yaml" || p["path"] != "/data/rulesets/s1.yaml" {
		t.Errorf("%v", p)
	}
	if rules[0] != "DOMAIN-KEYWORD,corp,DIRECT" {
		t.Errorf("the user's rule first: %s", rules[0])
	}
	if rules[1] != "RULE-SET,rs-s1,AI 服务,no-resolve" {
		t.Errorf("then the set, its target before no-resolve: %s", rules[1])
	}
}

// A RULE-SET naming a provider that isn't there fails the whole config.
func TestRuleSetWithoutItsFileIsLeftOut(t *testing.T) {
	cfg, rules := rulesOf(t, ruleSetInput(t, false))
	if _, ok := cfg["rule-providers"]; ok {
		t.Error("a provider with no file")
	}
	if slices.ContainsFunc(rules, func(r string) bool { return strings.HasPrefix(r, "RULE-SET") }) {
		t.Errorf("%v", rules[:3])
	}
}

// A remote list must not be able to take this machine's own networks.
func TestDevNetworksComeBeforeRuleSets(t *testing.T) {
	in := ruleSetInput(t, true)
	in.Dev = devnet.Compile(devnet.Options{}, devEnv())
	_, rules := rulesOf(t, in)
	at := slices.IndexFunc(rules, func(r string) bool { return strings.HasPrefix(r, "RULE-SET") })
	if at < 0 {
		t.Fatal("no RULE-SET")
	}
	for _, r := range in.Dev.Rules {
		if i := slices.Index(rules, withTarget(r.Text, r.Target)); i < 0 || i > at {
			t.Errorf("%s at %d, the set at %d", r.Text, i, at)
		}
	}
}
