package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/settings"
)

func TestRegionLists(t *testing.T) {
	counts := map[string]int{"HK": 9, "JP": 8, "SG": 7, "US": 6, "KR": 5, "GB": 4, "PH": 3, "DE": 3}
	for _, tt := range []struct {
		name         string
		saved, keep  []string
		common, rest []string
	}{
		{"most nodes first, ties by code", nil, nil, []string{"HK", "JP", "SG", "US", "KR", "GB"}, []string{"DE", "PH"}},
		{"the user's order", []string{"US", "HK"}, nil, []string{"US", "HK"}, []string{"DE", "GB", "JP", "KR", "PH", "SG"}},
		{"a common region with no node waits", []string{"ZZ", "HK"}, nil, []string{"HK"}, []string{"DE", "GB", "JP", "KR", "PH", "SG", "US"}},
		{"unless it's kept", []string{"ZZ", "HK"}, []string{"ZZ"}, []string{"ZZ", "HK"}, []string{"DE", "GB", "JP", "KR", "PH", "SG", "US"}},
		{"a chosen one with no node stays", []string{"HK"}, []string{"YY"}, []string{"HK"}, []string{"DE", "GB", "JP", "KR", "PH", "SG", "US", "YY"}},
		{"listed twice", []string{"HK", "HK"}, nil, []string{"HK"}, []string{"DE", "GB", "JP", "KR", "PH", "SG", "US"}},
	} {
		common, rest := regionLists(tt.saved, tt.keep, counts)
		if !slices.Equal(common, tt.common) || !slices.Equal(rest, tt.rest) {
			t.Errorf("%s: common %v, rest %v", tt.name, common, rest)
		}
	}
	// A few regions fold nothing away.
	common, rest := regionLists(nil, nil, map[string]int{"HK": 1, "JP": 1})
	if len(common) != 2 || len(rest) != 0 {
		t.Errorf("few: common %v, rest %v", common, rest)
	}
}

// regionsFixture has nine regions, more nodes the earlier in the list.
func regionsFixture(tt *testing.T) (*UI, *ui.Tester) {
	tt.Helper()
	u := routesFixture()
	u.snap.Nodes = nil
	for i, cc := range []string{"HK", "JP", "SG", "US", "KR", "GB", "PH", "DE", "FR"} {
		for n := 0; n < 9-i; n++ {
			name := fmt.Sprintf("%s %d", cc, n)
			u.snap.Nodes = append(u.snap.Nodes, NodeView{Name: name, Display: name, Provider: "a", Tested: true,
				Where: Located{Place: &Place{Country: cc}, Source: "name"}})
		}
	}
	u.selectGroup("ai")
	u.routes.pol = true
	te := ui.NewTester(u.view, 1600, 760)
	te.Advance(time.Second)
	return u, te
}

func clickRegion(t *testing.T, te *ui.Tester, label string) {
	t.Helper()
	if err := te.Click(label); err != nil {
		t.Fatalf("%v: %q", err, te.Texts())
	}
	te.Advance(100 * time.Millisecond)
}

func wantChips(t *testing.T, te *ui.Tester, shown, hidden []string) {
	t.Helper()
	for _, s := range shown {
		if !te.HasText(s) {
			t.Errorf("no %q: %q", s, te.Texts())
		}
	}
	for _, s := range hidden {
		if te.HasText(s) {
			t.Errorf("%q shows: %q", s, te.Texts())
		}
	}
}

// TestRegionRow: the row leads with the common regions, folds the rest,
// and a chosen region never folds.
func TestRegionRow(t *testing.T) {
	u, te := regionsFixture(t)
	wantChips(t, te, []string{"HK 9", "GB 4", "更多 3"}, []string{"PH 3", "DE 2", "FR 1", "调整常用"})
	shot(t, te, "regions-folded")

	clickRegion(t, te, "更多 3")
	wantChips(t, te, []string{"HK 9", "PH 3", "FR 1", "收起", "调整常用"}, []string{"更多 3"})
	shot(t, te, "regions-more")

	clickRegion(t, te, "PH 3")
	if q := u.policy("ai"); !slices.Equal(q.Regions, []string{"PH"}) {
		t.Errorf("regions: %v", q.Regions)
	}
	clickRegion(t, te, "收起")
	wantChips(t, te, []string{"PH 3", "更多 2"}, []string{"DE 2", "FR 1"})
}

// TestCommonRegionsEdit: editing the list keeps what groups have chosen, and
// a subscription's new regions land behind 更多.
func TestCommonRegionsEdit(t *testing.T) {
	u, te := regionsFixture(t)
	clickRegion(t, te, "HK 9") // the group's range
	clickRegion(t, te, "更多 3")
	clickRegion(t, te, "调整常用")
	wantChips(t, te, []string{"点选常用地区", "完成"}, []string{"恢复默认", "收起"})
	shot(t, te, "regions-edit")

	clickRegion(t, te, "HK 9") // out of the common list
	if got := u.shownSettings().CommonRegions; !slices.Equal(got, []string{"JP", "SG", "US", "KR", "GB"}) {
		t.Errorf("after removing: %v", got)
	}
	clickRegion(t, te, "FR 1")
	if err := te.Click("FR 前移"); err != nil {
		t.Fatalf("%v: %q", err, te.Texts())
	}
	te.Advance(100 * time.Millisecond)
	if got := u.shownSettings().CommonRegions; !slices.Equal(got, []string{"JP", "SG", "US", "KR", "FR", "GB"}) {
		t.Errorf("after moving: %v", got)
	}
	if q := u.policy("ai"); !slices.Equal(q.Regions, []string{"HK"}) {
		t.Errorf("the group's range changed: %v", q.Regions)
	}
	clickRegion(t, te, "完成")
	clickRegion(t, te, "收起")
	wantChips(t, te, []string{"HK 9", "FR 1", "更多 2"}, []string{"PH 3", "完成"}) // HK is chosen, so it stays

	// The subscription brings a region nobody listed.
	u.snap.Nodes = append(u.snap.Nodes, NodeView{Name: "BR 0", Display: "BR 0", Provider: "a", Tested: true,
		Where: Located{Place: &Place{Country: "BR"}, Source: "name"}})
	te.Frame()
	wantChips(t, te, []string{"更多 3"}, []string{"BR 1"})

	clickRegion(t, te, "更多 3")
	clickRegion(t, te, "调整常用")
	clickRegion(t, te, "恢复默认")
	if got := u.shownSettings().CommonRegions; got != nil {
		t.Errorf("after reset: %v", got)
	}
}

// TestCommonRegionsSaved: settings from before the list have none, and the
// list changing never reloads the core.
func TestCommonRegionsSaved(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"language":"zh"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := settings.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := st.Get()
	if old.CommonRegions != nil {
		t.Errorf("old settings: %v", old.CommonRegions)
	}
	if _, err := st.Update(func(s *settings.Settings) { s.CommonRegions = []string{"US", "HK"} }); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var saved struct {
		CommonRegions []string `json:"commonRegions"`
	}
	if err := json.Unmarshal(b, &saved); err != nil || !slices.Equal(saved.CommonRegions, []string{"US", "HK"}) {
		t.Errorf("saved %v (%v)", saved.CommonRegions, err)
	}
	next := st.Get()
	if !appOnly(old, next) {
		t.Error("the common regions reload the core")
	}
	c := next.Clone()
	c.CommonRegions[0] = "ZZ"
	if next.CommonRegions[0] != "US" {
		t.Error("Clone shares the list")
	}
}
