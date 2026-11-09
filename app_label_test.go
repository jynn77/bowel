package main

import (
	"context"
	"regexp"
	"testing"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/geo"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/settings"
)

// A node that is all of a pasted link provider goes by the provider's name,
// so renaming the provider renames it wherever nodes show.
func TestNodeLabelFollowsLinkProvider(t *testing.T) {
	ps, err := provider.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	one, err := ps.Add(ctx, nil, "trojan://pw@hk.example.com:443#HK01")
	if err != nil {
		t.Fatal(err)
	}
	two, err := ps.Add(ctx, nil, "trojan://pw@a.example.com:443#A\ntrojan://pw@b.example.com:443#B")
	if err != nil {
		t.Fatal(err)
	}
	a := &App{providers: ps}
	n1, nA := "HK01"+core.NodeSep+one.ID, "A"+core.NodeSep+two.ID
	a.provNodes = map[string][]string{one.ID: {n1}, two.ID: {nA, "B" + core.NodeSep + two.ID}}

	if got := a.display(n1); got != "HK01" {
		t.Errorf("before rename: %q", got)
	}
	if _, err := ps.Rename(one.ID, "Ohio"); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Rename(two.ID, "Pair"); err != nil {
		t.Fatal(err)
	}
	if got := a.display(n1); got != "Ohio" {
		t.Errorf("one-node link provider renamed: node shows %q", got)
	}
	if got := a.display(nA); got != "A" {
		t.Errorf("a node of several keeps its name, got %q", got)
	}
	if got := a.display("DIRECT"); got != "DIRECT" {
		t.Errorf("DIRECT = %q", got)
	}
}

// A subscription writes zero-width characters between the letters of its
// names so that they cannot be copied intact. They are invisible, so in the
// interface they only read as gaps. What names are shown without is the
// user's to say: each rule of the setting drops what it matches, plain text
// by itself and /…/ as a regular expression, and a list that has none drops
// nothing. The name the core goes by keeps what is matched.
func TestDisplayDropsConfigured(t *testing.T) {
	a := &App{}
	rules, bad := settings.CompileRules([]string{geo.InvisibleRule})
	if len(bad) != 0 {
		t.Fatalf("the default rule does not compile: %v", bad)
	}
	a.strip.Store(&rules)
	for _, tc := range []struct{ in, want string }{
		{"[Vless] H\u200bK\u200b \u200bT\u200bG:@channel", "[Vless] HK TG:@channel"},
		{"日\u200b本\u200b 01", "日本 01"},
		{"\ufeffNK\u200b 02", "NK 02"},
		{"plain name", "plain name"},
	} {
		if got := a.stripped(display(tc.in)); got != tc.want {
			t.Errorf("stripped(display(%q)) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// The rules are the user's: a plain rule drops itself, a /…/ rule drops
	// what its expression matches, and an empty list leaves the name alone.
	rules, _ = settings.CompileRules([]string{"IPLC", "/\\[.*\\]/", "/@\\w+/"})
	a.strip.Store(&rules)
	for _, tc := range []struct{ in, want string }{
		{"HK IPLC 01", "HK  01"},
		{"[Vless] HK TG", " HK TG"},
		{"日本 @channel", "日本 "},
		{"日本 02", "日本 02"},
	} {
		if got := a.stripped(display(tc.in)); got != tc.want {
			t.Errorf("stripped(display(%q)) = %q, want %q", tc.in, got, tc.want)
		}
	}
	a.strip.Store(&[]*regexp.Regexp{})
	if got := a.stripped(display("日\u200b本 01")); got != "日\u200b本 01" {
		t.Errorf("an empty list drops nothing: %q", got)
	}
}
