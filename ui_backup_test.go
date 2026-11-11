package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/settings"
)

// TestBackupRows: 关于 ends with the configuration's rows, in every
// language; undoing is offered only once there is an import to undo; and
// the password has its field.
func TestBackupRows(t *testing.T) {
	defer i18n.Set("zh")
	for _, lang := range i18n.Languages {
		i18n.Set(lang)
		u := fixtureUI()
		u.snap.Settings = settings.Defaults.Clone()
		tt := ui.NewTesterWith(u.settingsView, 760, 900, ui.TesterOptions{})
		tt.Advance(time.Second)
		if err := tt.Click(i18n.T("关于", "About")); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		for _, w := range []string{
			i18n.T("配置文件密码", "Configuration file password"), i18n.T("导出配置", "Export configuration"),
			i18n.T("导入配置", "Import configuration"), i18n.T("导出…", "Export…"), i18n.T("导入…", "Import…"),
		} {
			if !tt.HasText(w) {
				t.Errorf("%s: 关于 lacks %q: %q", lang, w, tt.Texts())
			}
		}
		undo := i18n.T("撤销上次导入", "Undo last import")
		if tt.HasText(undo) {
			t.Errorf("%s: undo is offered with nothing to undo", lang)
		}
		u.snap.CanUndoImport = true
		tt.Frame()
		if !tt.HasText(undo) {
			t.Errorf("%s: undo is not offered", lang)
		}
		if _, ok := tt.AccessByRole(ui.RoleTextField, i18n.T("配置文件密码", "Configuration file password")); !ok {
			t.Errorf("%s: no password field", lang)
		}
		if lang == "zh" || lang == "en" {
			shot(t, tt, "settings-backup-"+lang)
		}
	}
}
