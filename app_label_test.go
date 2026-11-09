package main

import (
	"context"
	"testing"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/provider"
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
// interface they only read as gaps — the name is shown without them.
func TestDisplayDropsInvisible(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"[Vless] H\u200bK\u200b \u200bT\u200bG:@channel", "[Vless] HK TG:@channel"},
		{"日\u200b本\u200b 01", "日本 01"},
		{"\ufeffNK\u200b 02", "NK 02"},
		{"plain name", "plain name"},
	} {
		if got := display(tc.in); got != tc.want {
			t.Errorf("display(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
