package main

// Rule sets in 路由组: a service whose rules come from a link or a file. It
// moves, switches off and goes like any other; only its rules are someone
// else's. Design: prototype/groups.html.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/ruleset"
)

// ruleSetDraft is the 导入规则集 panel.
type ruleSetDraft struct {
	open      bool
	text      string
	input     ui.Handle
	focus     bool
	asked     string // what was last sent to be checked
	gen       int    // an answer to an older question is dropped
	checking  bool
	checked   *ruleset.Checked
	err       string
	dup       *routes.Service
	importing bool
}

func (u *UI) openRuleSetImport() {
	d := &u.routes.rs
	*d = ruleSetDraft{open: true, focus: true, gen: d.gen + 1}
}

func (u *UI) closeRuleSetImport() {
	d := &u.routes.rs
	d.open, d.checking = false, false
	d.gen++
}

// checkDraft looks at the source as typed, a moment after typing stops.
func (u *UI) checkDraft(all []routes.Route) {
	d := &u.routes.rs
	src := strings.TrimSpace(d.text)
	if src == d.asked {
		return
	}
	d.gen++
	gen := d.gen
	d.asked, d.checked, d.err, d.dup, d.checking = src, nil, "", nil, false
	if sv, dup := routes.FindSource(all, src); src != "" && dup {
		d.dup = &sv
		return
	}
	if src == "" || u.app == nil {
		return
	}
	d.checking = true
	go func() {
		time.Sleep(400 * time.Millisecond)
		alive := make(chan bool, 1)
		u.win.Update(func() { alive <- d.gen == gen })
		if !<-alive {
			return
		}
		c, err := u.app.checkRuleSet(context.Background(), src)
		u.win.Update(func() {
			if d.gen != gen {
				return
			}
			d.checking, d.checked = false, c
			if err != nil {
				d.err = err.Error()
			}
		})
	}()
}

// ruleSetImport is the panel: a source, what it holds, and 导入, which is the
// confirmation.
func (u *UI) ruleSetImport(c *ui.Context, p *pal, all []routes.Route, r routes.Route) {
	st := &u.routes
	d := &st.rs
	u.checkDraft(all)
	if d.focus {
		d.focus = false
		d.input.Focus(c)
	}
	ready := d.checked != nil && !d.importing
	add := func() {
		if !ready {
			return
		}
		checked, group := d.checked, r.ID
		d.importing = true
		go func() {
			id, err := u.app.importRuleSet(checked, group)
			u.win.Update(func() {
				d.importing = false
				if err != nil {
					d.err, d.checked = err.Error(), nil
					return
				}
				u.closeRuleSetImport()
				st.svc = id
				u.clearRuleDraft()
			})
		}()
	}
	ui.Column(c.Key("import")).Gap(6).Padding(12, 14).Margin(0, 0, 10, 0).Radius(p.r(10)).Background(p.card).Border(1, p.hair).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			in := ui.TextInput(c.Key("src"), &d.text).Bind(&d.input).Label(i18n.T("规则集来源", "Rule set source")).
				Placeholder("https://example.com/rules.yaml").
				Padding(5, 9).Radius(p.r(7)).Background(p.paper).Border(1, p.hair).FontSize(12.5).Font(actMono).Grow(1).Basis(0).MinWidth(0)
			if in.Submitted() {
				add()
			}
			btn(c.Key("file"), p, i18n.T("选择文件…", "Choose file…")).OnClick(func() {
				if u.app == nil {
					return
				}
				go func() {
					path, err := u.app.pickRuleSetFile(bg())
					if err != nil {
						u.toast(err.Error())
					} else if path != "" {
						u.win.Update(func() { d.text = path })
					}
				}()
			})
		})
		switch {
		case d.checking:
			shimmer(ui.Text(c, i18n.T("正在下载并检查…", "Downloading and checking…")).FontSize(11.5).TextColor(p.mute), true, p).Shrink(0)
		case d.dup != nil:
			sv := *d.dup
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf(i18n.T("已经导入到「%s」了", "Already imported to %s"), u.routeName(sv.Group))).FontSize(11.5).TextColor(p.ink2)
				ui.ButtonBase(c.Key("see")).OnClick(func() { u.closeRuleSetImport(); u.openRoutes(sv.Group, sv.ID) }).Children(func() {
					ui.Text(c, i18n.T("查看", "View")).FontSize(11.5).FontWeight(600).Underline().TextColor(p.ink).NoWrap()
				})
			})
		case d.err != "":
			ui.Text(c, d.err).FontSize(11.5).TextColor(p.signal)
		case d.checked != nil:
			set := d.checked.Set
			ui.Text(c, ruleSetCount(len(set.Rules), set.Skipped)+" · "+fmt.Sprintf(i18n.T("来自 %s", "from %s"), ruleSetWhere(d.checked.Source))).FontSize(11.5).FontWeight(600).TextColor(p.ink2)
			ui.Text(c, fmt.Sprintf(i18n.T("命中的流量走「%s」。优先级：你的规则 → 规则集 → 内置规则。", "Matching traffic goes by %s. Order: your rules → rule sets → built-in rules."), u.routeName(r.ID))).FontSize(12).TextColor(p.ink2)
		default:
			ui.Text(c, i18n.T("粘贴规则集链接，或选择本地 .yaml / .list 文件。", "Paste a link to a rule set, or choose a local .yaml or .list file.")).FontSize(11.5).TextColor(p.faint)
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(2, 0, 0, 0).Children(func() {
			ui.Spacer(c)
			btn(c.Key("cancel"), p, i18n.T("取消", "Cancel")).OnClick(u.closeRuleSetImport)
			b := btn(c.Key("go"), p, i18n.T("导入", "Import"))
			if ready {
				b.OnClick(add)
			} else {
				b.Opacity(.45)
			}
		})
	})
}

func ruleSetCount(n, skipped int) string {
	s := fmt.Sprintf(i18n.T("%d 条规则", "%d rules"), n)
	if skipped > 0 {
		s += fmt.Sprintf(i18n.T("，跳过 %d 条不支持的", ", %d unsupported skipped"), skipped)
	}
	return s
}

// ruleSetWhere is a source in a word or two: a link's host.
func ruleSetWhere(source string) string {
	if ruleset.Local(source) {
		return i18n.T("本地文件", "Local file")
	}
	_, rest, _ := strings.Cut(source, "://")
	host, _, _ := strings.Cut(rest, "/")
	return host
}

func ruleSetUpdated(v RuleSetView) string {
	return fmt.Sprintf(i18n.T("%s更新", "updated %s"), strings.ToLower(ago(v.UpdatedAt)))
}

// ruleSetRowWord is a rule set in its row, under its name.
func (u *UI) ruleSetRowWord(s routes.Service) (string, bool) {
	v, ok := u.snap.RuleSets[s.ID]
	switch {
	case s.Off:
		return i18n.T("已停用，这些规则不再生效", "Off: these rules no longer apply"), false
	case !ok:
		return i18n.T("本地副本不见了，点开后更新", "The saved copy is gone. Open it and update"), true
	case v.Error != "":
		return i18n.T("更新失败，继续使用上次的规则", "Update failed; still using the last rules"), true
	}
	return fmt.Sprintf(i18n.T("%d 条规则", "%d rules"), v.Count) + " · " + ruleSetUpdated(v) + " · " + ruleSetWhere(s.Source), false
}

// ruleSetBody is an open rule set: where it comes from, how that is going,
// and a taste of what it holds.
func (u *UI) ruleSetBody(c *ui.Context, p *pal, s routes.Service) {
	local := ruleset.Local(s.Source)
	busy := u.pending("ruleset:" + s.ID)
	v, have := u.snap.RuleSets[s.ID]
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(4, 0, 0, 0).Children(func() {
		ui.Text(c, s.Source).Font(actMono).FontSize(12).TextColor(p.ink2).SingleLine().Grow(1).Basis(0).MinWidth(0).Tooltip(s.Source)
		label := i18n.T("更新", "Update")
		if local {
			label = i18n.T("重新读取", "Read again")
		}
		b := btn(c.Key("update"), p, label)
		if !busy && u.app != nil {
			b.OnClick(func() { u.do(func() error { return u.app.updateRuleSet(bg(), s.ID) }) })
		}
	})
	switch {
	case busy:
		shimmer(ui.Text(c, i18n.T("正在更新…", "Updating…")).FontSize(11.5).TextColor(p.mute), true, p).Shrink(0)
	case !have:
		ui.Text(c, i18n.T("本地副本不见了，点更新重新获取", "The saved copy is gone. Update to fetch it again")).FontSize(11.5).TextColor(p.signal)
	case v.Error != "":
		ui.Text(c, fmt.Sprintf(i18n.T("更新失败，继续使用上次的 %d 条规则：%s", "Update failed; still using the last %d rules: %s"), v.Count, v.Error)).FontSize(11.5).TextColor(p.signal)
	default:
		how := i18n.T("每天自动更新", "updates daily")
		if local {
			how = i18n.T("本地文件，不自动更新", "local file, doesn't update by itself")
		}
		ui.Text(c, ruleSetCount(v.Count, v.Skipped)+" · "+ruleSetUpdated(v)+" · "+how).FontSize(11.5).TextColor(p.mute)
	}
	if !have {
		return
	}
	ui.Column(c).Children(func() {
		for _, l := range v.Preview {
			ui.Row(c.Key(l.Text)).Gap(10).Padding(5, 0).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.hair2).Children(func() {
				ui.Text(c, l.Shown()).Font(actMono).FontSize(12.5).TextColor(p.ink).SingleLine().Grow(1).Basis(0).MinWidth(0)
				ui.Text(c, routes.KindName(l.Kind)).FontSize(11).TextColor(p.mute).NoWrap().Shrink(0)
			})
		}
	})
	if more := v.Count - len(v.Preview); more > 0 {
		ui.Text(c, fmt.Sprintf(i18n.T("还有 %d 条，由来源维护，这里不能单独改。", "%d more, kept by the source; they can't be edited here."), more)).FontSize(11.5).TextColor(p.mute)
	}
}

// matchRuleSet lets 测试一个地址 look inside the rule sets.
func (u *UI) matchRuleSet() func(id, host, ip string) (string, bool) {
	if u.app == nil {
		return nil
	}
	return u.app.matchRuleSet
}
