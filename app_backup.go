package main

// Exporting the configuration to a file and importing it again
// (internal/backup): the settings that travel, the providers and the rule
// sets. What belongs to this machine (settings.KeepMachine) is neither
// written nor taken, and nothing else in the data folder is: the core's
// config is built from these, the rest is this machine's state.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"

	"github.com/oboo/moho/internal/backup"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/ruleset"
	"github.com/oboo/moho/internal/settings"
)

// undoImportPath is where the configuration as it was before the last
// import is kept, for undoing it. It is not sealed: it never leaves the
// data folder, where the same things already lie open.
func (a *App) undoImportPath() string {
	return filepath.Join(a.dataDir, "before-import."+backup.Ext)
}

// canUndoImport says whether there is an import to undo.
func (a *App) canUndoImport() bool {
	_, err := os.Stat(a.undoImportPath())
	return err == nil
}

// configBundle is the configuration as it is now.
func (a *App) configBundle() (backup.Bundle, error) {
	s := a.settings.Get()
	b := backup.Bundle{Manifest: backup.Manifest{Version: mygo.App.Version(), OS: runtime.GOOS, CreatedAt: time.Now().UTC().Truncate(time.Second)}}
	var err error
	if b.Settings, err = json.MarshalIndent(settings.Portable(s), "", "  "); err != nil {
		return b, err
	}
	items, files := a.providers.Snapshot()
	if b.Providers, err = json.MarshalIndent(items, "", "  "); err != nil {
		return b, err
	}
	b.ProviderFiles = files
	meta, sets := a.ruleSets.Snapshot(ruleSetIDs(s))
	if b.RuleSets, err = json.MarshalIndent(meta, "", "  "); err != nil {
		return b, err
	}
	b.RuleSetFiles = sets
	return b, nil
}

// ruleSetIDs are the rule sets s's services name.
func ruleSetIDs(s settings.Settings) map[string]bool {
	ids := map[string]bool{}
	for _, sv := range s.Routing.Services {
		if sv.Source != "" {
			ids[sv.ID] = true
		}
	}
	return ids
}

// exportConfig writes the configuration to path, sealed with password
// unless it is "".
func (a *App) exportConfig(path, password string) error {
	b, err := a.configBundle()
	if err != nil {
		return err
	}
	return backup.WriteFile(path, b, password)
}

// imported is a file's contents as the stores take them.
type importedConfig struct {
	settings  settings.Settings
	providers []provider.Provider
	files     map[string][]byte
	sets      map[string]ruleset.Meta
	setFiles  map[string][]byte
}

// readImport reads and checks the file at path against the settings of
// this machine, changing nothing.
func readImport(path, password string, local settings.Settings) (importedConfig, error) {
	var c importedConfig
	b, err := backup.ReadFile(path, password)
	if err != nil {
		return c, err
	}
	bad := func(err error) (importedConfig, error) {
		return c, fmt.Errorf(i18n.T("配置文件读不了：%v", "The configuration can't be read: %v"), err)
	}
	s, err := settings.Decode(b.Settings)
	if err != nil {
		return bad(err)
	}
	if len(b.Providers) > 0 {
		if err := json.Unmarshal(b.Providers, &c.providers); err != nil {
			return bad(err)
		}
	}
	if len(b.RuleSets) > 0 {
		if err := json.Unmarshal(b.RuleSets, &c.sets); err != nil {
			return bad(err)
		}
	}
	c.files, c.setFiles = b.ProviderFiles, b.RuleSetFiles
	known := map[string]bool{}
	for i, p := range c.providers {
		if p.Kind == provider.File {
			// Where it came from on the machine that wrote this file; here it
			// is not a file anyone chose, so 重新读取 asks instead of reading it.
			c.providers[i].Source = ""
		}
		if !backup.ValidID(p.ID) || known[p.ID] {
			return bad(fmt.Errorf("provider id %q", p.ID))
		}
		if _, ok := c.files[p.ID]; !ok {
			return bad(fmt.Errorf(i18n.T("Provider %s 的内容不完整", "Provider %s is incomplete"), p.Name))
		}
		known[p.ID] = true
	}
	for id := range c.sets {
		if !backup.ValidID(id) {
			return bad(fmt.Errorf("rule set id %q", id))
		}
	}
	if s.ImportedRouting != "" && !known[s.ImportedRouting] {
		s.ImportedRouting = "" // its provider is not in the file
	}
	c.settings = settings.KeepMachine(local, s)
	if err := c.settings.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

// importConfig makes the configuration that of the file at path: its
// providers, rule sets and settings replace these, except the settings of
// this machine. The configuration as it was is kept for undoImport. If the
// core will not run on what the file holds, everything is as before.
func (a *App) importConfig(path, password string) error {
	c, err := readImport(path, password, a.settings.Get())
	if err != nil {
		return err
	}
	done := a.begin("import")
	defer done()
	before, err := a.configBundle()
	if err != nil {
		return err
	}
	// What is there now is written before anything changes, and takes its
	// place only once the import is through: until then the file there may
	// be the one being imported, when an import is undone, and an import
	// that fails leaves nothing new to undo.
	undo := a.undoImportPath()
	if err := backup.WriteFile(undo+".new", before, ""); err != nil {
		return err
	}
	defer os.Remove(undo + ".new")
	oldItems, oldFiles := a.providers.Snapshot()
	oldSets, oldSetFiles := a.ruleSets.Snapshot(ruleSetIDs(a.settings.Get()))
	back := func() {
		_ = a.providers.Replace(oldItems, oldFiles)
		_ = a.ruleSets.Replace(oldSets, oldSetFiles)
	}
	if err := a.providers.Replace(c.providers, c.files); err != nil {
		back()
		return err
	}
	if err := a.ruleSets.Replace(c.sets, c.setFiles); err != nil {
		back()
		return err
	}
	err = a.updateSettingsAnd(func(s *settings.Settings) {
		// This machine's settings as they are now, should they have changed
		// since the file was checked.
		*s = settings.KeepMachine(*s, c.settings)
	}, true)
	if err != nil {
		// updateSettings put the settings back; the core now runs on them
		// with the file's providers, so those go back too.
		back()
		_ = a.applyConfig()
		return err
	}
	a.keepRuleSets()
	_ = os.Rename(undo+".new", undo)
	a.refresh()
	return nil
}

// undoImport puts the configuration back as it was before the last import.
// Undoing twice is the import again.
func (a *App) undoImport() error {
	path := a.undoImportPath()
	if _, err := os.Stat(path); err != nil {
		return errors.New(i18n.T("没有可以撤销的导入", "There is no import to undo"))
	}
	return a.importConfig(path, "")
}

// exportName is what an exported file is called unless the user says.
func exportName(now time.Time) string {
	return "bowel-" + now.Format("2006-01-02") + "." + backup.Ext
}

var configFilter = func() []mygo.FileFilter {
	return []mygo.FileFilter{{Name: i18n.T("bowel 配置", "bowel configuration"), Extensions: []string{backup.Ext}}}
}

// exportConfigDialog asks where, and exports there. It is "" when the user
// chose nowhere.
func (a *App) exportConfigDialog(ctx context.Context, password string) (string, error) {
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: i18n.T("导出配置", "Export configuration"),
		DefaultPath: exportName(time.Now()), Filters: configFilter(),
	})
	if err != nil || path == "" {
		return "", err
	}
	return path, a.exportConfig(path, password)
}

// importConfigDialog asks which file, and imports it. It is "" when the
// user chose none.
func (a *App) importConfigDialog(ctx context.Context, password string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: i18n.T("导入配置", "Import configuration"), Filters: configFilter(),
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], a.importConfig(paths[0], password)
}
