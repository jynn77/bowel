package main

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

// commonRegionCount is how many regions lead the 地区 row until the user
// chooses their own.
const commonRegionCount = 6

// regionCounts is how many nodes leave from each country.
func regionCounts(nodes []NodeView) map[string]int {
	counts := map[string]int{}
	for _, n := range nodes {
		if pl := n.Where.Place; pl != nil {
			counts[pl.Country]++
		}
	}
	return counts
}

// commonOf is the common list: the saved one, or the regions with the most
// nodes.
func commonOf(saved []string, counts map[string]int) []string {
	if len(saved) > 0 {
		return slices.Clone(saved)
	}
	out := slices.Collect(maps.Keys(counts))
	slices.SortFunc(out, func(a, b string) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b))
	})
	return out[:min(len(out), commonRegionCount)]
}

// regionLists sorts the regions into the common ones, in the list's order,
// and the rest, by code. A region with no node now is left out unless it is
// in keep, so one the subscription dropped comes back with its nodes.
func regionLists(saved, keep []string, counts map[string]int) (common, rest []string) {
	for _, r := range commonOf(saved, counts) {
		if !slices.Contains(common, r) && (counts[r] > 0 || slices.Contains(keep, r)) {
			common = append(common, r)
		}
	}
	for r := range counts {
		if !slices.Contains(common, r) {
			rest = append(rest, r)
		}
	}
	for _, r := range keep {
		if counts[r] == 0 && !slices.Contains(common, r) && !slices.Contains(rest, r) {
			rest = append(rest, r)
		}
	}
	slices.Sort(rest)
	return common, rest
}

// movedEarlier is list with v one place nearer the front.
func movedEarlier(list []string, v string) []string {
	if i := slices.Index(list, v); i > 0 {
		list[i-1], list[i] = list[i], list[i-1]
	}
	return list
}

// regionChips is the 地区 row's choices: all, the common regions and any
// other that is chosen, with the remaining ones behind 更多. Which regions
// are common only decides what is shown: q.Regions is the group's range.
func (u *UI) regionChips(c *ui.Context, p *pal, rid string, q routes.Policy, label func(string) (string, string)) {
	saved := u.shownSettings().CommonRegions
	counts := regionCounts(u.snap.Nodes)
	if u.home.regionsEdit {
		u.commonEditor(c, p, saved, counts, label)
		return
	}
	common, rest := regionLists(saved, q.Regions, counts)
	folded := slices.DeleteFunc(slices.Clone(rest), func(r string) bool { return slices.Contains(q.Regions, r) })
	shown := rest
	if !u.home.regionsMore {
		shown = slices.DeleteFunc(slices.Clone(rest), func(r string) bool { return slices.Contains(folded, r) })
	}
	plain := func(key, text string, on bool, onClick func()) { chip(c, p, key, text, text, on, onClick) }
	plain("all", i18n.T("全部", "All"), len(q.Regions) == 0, func() { u.changePolicy(rid, func(q *routes.Policy) { q.Regions = nil }) })
	for _, r := range append(common, shown...) {
		text, widest := label(r)
		chip(c, p, r, text, widest, slices.Contains(q.Regions, r), func() {
			u.changePolicy(rid, func(q *routes.Policy) { q.Regions = toggled(q.Regions, r) })
		})
	}
	switch {
	case u.home.regionsMore:
		plain("less", i18n.T("收起", "Less"), false, func() { u.home.regionsMore = false })
		plain("edit", i18n.T("调整常用", "Edit common"), false, func() { u.home.regionsEdit = true })
	case len(folded) > 0:
		plain("more", fmt.Sprintf(i18n.T("更多 %d", "%d more"), len(folded)), false, func() { u.home.regionsMore = true })
	case len(saved) > 0: // every region is common: still the way to change that
		plain("more", i18n.T("更多", "More"), false, func() { u.home.regionsMore = true })
	}
}

// commonEditor is the 地区 row while the common regions are being chosen. A
// change applies to every route group at once. The first one turns the
// regions picked by node count into the user's own list; taking out the last
// goes back to picking.
func (u *UI) commonEditor(c *ui.Context, p *pal, saved []string, counts map[string]int, label func(string) (string, string)) {
	auto := commonOf(nil, counts)
	change := func(fn func([]string) []string) {
		u.changeRouting(func(s *settings.Settings) {
			list := s.CommonRegions
			if len(list) == 0 {
				list = auto
			}
			s.CommonRegions = fn(slices.Clone(list))
		}, nil)
	}
	common, rest := regionLists(saved, saved, counts)
	ui.Text(c, i18n.T("点选常用地区", "Pick common regions")).FontSize(12).TextColor(p.mute).NoWrap().Margin(0, 6, 0, 0)
	for i, r := range common {
		text, widest := label(r)
		chip(c, p, r, text, widest, true, func() {
			change(func(l []string) []string { return toggled(l, r) })
		})
		if i > 0 {
			u.moveChip(c, p, r, func() {
				change(func(l []string) []string { return movedEarlier(l, r) })
			})
		}
	}
	for _, r := range rest {
		text, widest := label(r)
		chip(c, p, r, text, widest, false, func() {
			change(func(l []string) []string { return toggled(l, r) })
		})
	}
	if len(saved) > 0 {
		reset := i18n.T("恢复默认", "Reset")
		chip(c, p, "reset", reset, reset, false, func() {
			u.changeRouting(func(s *settings.Settings) { s.CommonRegions = nil }, nil)
		})
	}
	done := i18n.T("完成", "Done")
	chip(c, p, "done", done, done, false, func() { u.home.regionsEdit = false })
}

// moveChip moves common region r one place nearer the front.
func (u *UI) moveChip(c *ui.Context, p *pal, r string, onClick func()) {
	name := fmt.Sprintf(i18n.T("%s 前移", "Move %s earlier"), r)
	b := ui.ButtonBase(c.Key("move"+r)).Padding(3, 4).Radius(p.r(99)).Shrink(0).Label(name).Tooltip(name)
	col := p.mute
	if b.Hovered() {
		b.Background(p.hover)
		col = p.ink
	}
	b.OnClick(onClick).Children(func() { ui.Text(c, "←").FontSize(12).TextColor(col).NoWrap() })
}
