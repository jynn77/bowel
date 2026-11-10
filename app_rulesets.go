package main

// Rule sets at run time: the files (internal/ruleset), the services that
// name them (settings.Routing), and telling the core when a file changes.
// A new file needs no reload: the core reads a rule-provider again on its
// own, so an update closes no connection.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/ruleset"
	"github.com/oboo/moho/internal/settings"
)

// keepRuleSets clears out the files of sets no service names any more.
func (a *App) keepRuleSets() {
	ids := map[string]bool{}
	for _, sv := range a.settings.Get().Routing.Services {
		if sv.Source != "" {
			ids[sv.ID] = true
		}
	}
	a.ruleSets.Keep(ids)
}

// ruleSetRefs are the sets the core should load.
func (a *App) ruleSetRefs(s settings.Settings) []core.RuleSetRef {
	var out []core.RuleSetRef
	for _, sv := range s.Routing.Services {
		if sv.Source == "" || sv.Off {
			continue
		}
		if m, ok := a.ruleSets.Get(sv.ID); ok {
			out = append(out, core.RuleSetRef{ID: sv.ID, Path: a.ruleSets.Path(sv.ID), Behavior: m.Behavior})
		}
	}
	return out
}

func (a *App) ruleSetViews(s settings.Settings) map[string]RuleSetView {
	out := map[string]RuleSetView{}
	for _, sv := range s.Routing.Services {
		if sv.Source == "" {
			continue
		}
		if m, ok := a.ruleSets.Get(sv.ID); ok {
			out[sv.ID] = RuleSetView{Count: m.Count, Skipped: m.Skipped, UpdatedAt: m.UpdatedAt, Error: m.Error, Preview: a.ruleSets.Preview(sv.ID)}
		}
	}
	return out
}

func (a *App) matchRuleSet(id, host, ip string) (string, bool) {
	return a.ruleSets.Match(id, host, ip)
}

// checkRuleSet fetches a source and reads it, saving nothing.
func (a *App) checkRuleSet(ctx context.Context, source string) (*ruleset.Checked, error) {
	return ruleset.Fetch(ctx, a.http, source)
}

// pickRuleSetFile asks for a file; "" if none was chosen.
func (a *App) pickRuleSetFile(ctx context.Context) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: i18n.T("导入规则集", "Import rule set"),
		Filters: []mygo.FileFilter{{Name: i18n.T("规则集", "Rule set"), Extensions: []string{"yaml", "yml", "list", "txt"}}},
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// importRuleSet makes a checked set a service of group, and closes the open
// connections it now sends there, so that it shows at once. It returns the
// service's id.
func (a *App) importRuleSet(c *ruleset.Checked, group string) (string, error) {
	if sv, dup := routes.FindSource(a.routes(), c.Source); dup {
		return "", fmt.Errorf(i18n.T("已经导入到「%s」了", "Already imported to %s"), a.routeTitle(sv.Group))
	}
	var id string
	var saveErr error
	err := a.updateSettings(func(s *settings.Settings) {
		id = s.Routing.AddRuleSet(s.Rules, group, c.Name(), c.Source)
		// Before the config is written: it may only name a file that's there.
		saveErr = a.ruleSets.Save(id, c)
	})
	if err = errors.Join(err, saveErr); err != nil {
		if id != "" {
			a.ruleSets.Delete(id)
		}
		return "", err
	}
	go a.rerouteBy(func(host, ip string) bool { _, ok := a.ruleSets.Match(id, host, ip); return ok }, group)
	return id, nil
}

func (a *App) routeTitle(id string) string {
	for _, r := range a.routes() {
		if r.ID == id {
			return routes.Title(r)
		}
	}
	return id
}

// updateRuleSet fetches a set again. A failure leaves the last good list in
// use and shows on its row.
func (a *App) updateRuleSet(ctx context.Context, id string) error {
	done := a.begin("ruleset:" + id)
	defer done()
	defer a.emit()
	var conf *routes.ServiceConf
	s := a.settings.Get()
	for i := range s.Routing.Services {
		if sv := &s.Routing.Services[i]; sv.ID == id && sv.Source != "" {
			conf = sv
		}
	}
	if conf == nil {
		return errors.New(i18n.T("没有这个规则集", "No such rule set"))
	}
	_, had := a.ruleSets.Get(id)
	if err := a.ruleSets.Update(ctx, a.http, id, conf.Source); err != nil {
		return err
	}
	switch {
	case conf.Off:
		return nil
	case !had: // the core's config doesn't name it yet
		return a.applyConfig()
	}
	return a.coreCall(5*time.Second, func(ctx context.Context, c *mihomo.Client) error {
		return c.ReloadRuleSet(ctx, routes.RuleSetProvider(id))
	})
}

// autoUpdateRuleSets downloads the sets from links that are due.
func (a *App) autoUpdateRuleSets() {
	now := time.Now()
	for _, sv := range a.settings.Get().Routing.Services {
		if sv.Source == "" || sv.Off || ruleset.Local(sv.Source) || !a.ruleSets.Stale(sv.ID, now) {
			continue
		}
		if err := a.updateRuleSet(context.Background(), sv.ID); err != nil {
			log.Println("rule set:", sv.Name, err)
		}
	}
}

// mcpRuleSets are the rule sets as an agent reads them.
func (a *App) mcpRuleSets() []map[string]any {
	out := []map[string]any{}
	for _, r := range a.routes() {
		for _, sv := range r.Services {
			if sv.Source == "" {
				continue
			}
			set := map[string]any{"name": sv.Name, "route": r.ID, "source": sv.Source, "off": sv.Off}
			if m, ok := a.ruleSets.Get(sv.ID); ok {
				set["rules"], set["updatedAt"], set["skipped"] = m.Count, m.UpdatedAt, m.Skipped
				if m.Error != "" {
					set["lastError"] = m.Error
				}
			}
			out = append(out, set)
		}
	}
	return out
}
