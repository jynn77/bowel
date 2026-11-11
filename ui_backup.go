package main

// The 设置 window's rows for exporting and importing the configuration
// (app_backup.go), at the end of 关于, after 恢复默认.

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/backup"
	"github.com/oboo/moho/internal/i18n"
)

func (u *UI) backupRows(c *ui.Context, p *pal) {
	pass := u.set.field("backup-pass")
	optRow(c, p, i18n.T("配置文件密码", "Configuration file password"),
		i18n.T("可选。填了，导出的文件就用它加密，导入加密的文件时也填在这里；不填，导出的文件不加密，里面有订阅链接和节点信息",
			"Optional. With one, an exported file is encrypted with it, and it opens an encrypted file on import; without, an exported file is not encrypted and holds your subscription links and nodes"), func() {
			in := ui.TextInput(c.Key("backup-pass"), &pass.text).Bind(&pass.h).Label(i18n.T("配置文件密码", "Configuration file password")).
				Padding(5, 9).Radius(p.r(7)).Background(p.card).Border(1, p.hair).FontSize(12.5).Font("SF Mono, Menlo").Width(170).
				Placeholder(i18n.T("不加密", "No encryption")).Password()
			if pass.h.Focused(c) {
				in.BorderColor(p.ink)
			}
		})
	optRow(c, p, i18n.T("导出配置", "Export configuration"),
		i18n.T("把设置、Provider、规则和规则集存成一个文件，换电脑或重装时用；端口、局域网、TUN 等本机设置不在里面",
			"Saves the settings, Providers, rules and rule sets as one file, for another computer or a reinstall; this machine's own settings (ports, LAN, TUN…) are left out"), func() {
			u.backupAction(c, p, "config-export", i18n.T("导出…", "Export…"), func() (string, error) {
				path, err := u.app.exportConfigDialog(bg(), pass.text)
				if err != nil || path == "" {
					return "", err
				}
				if pass.text == "" {
					return fmt.Sprintf(i18n.T("已导出到 %s（未加密）", "Exported to %s (not encrypted)"), filepath.Base(path)), nil
				}
				return fmt.Sprintf(i18n.T("已导出到 %s（已加密）", "Exported to %s (encrypted)"), filepath.Base(path)), nil
			})
		})
	optRow(c, p, i18n.T("导入配置", "Import configuration"),
		i18n.T("用文件里的配置替换现在的设置、Provider、规则和规则集；本机设置保持不变，导入后可以撤销",
			"Replaces the settings, Providers, rules and rule sets with a file's; this machine's own settings stay, and the import can be undone"), func() {
			if u.snap.CanUndoImport {
				u.backupAction(c, p, "config-undo", i18n.T("撤销上次导入", "Undo last import"), func() (string, error) {
					return i18n.T("已撤销上次导入", "Undid the last import"), u.app.undoImport()
				})
			}
			u.backupAction(c, p, "config-import", i18n.T("导入…", "Import…"), func() (string, error) {
				path, err := u.app.importConfigDialog(bg(), pass.text)
				switch {
				case errors.Is(err, backup.ErrNeedPassword):
					return "", errors.New(i18n.T("这个文件设了密码，请先填上面的「配置文件密码」再导入", "This file has a password: fill in “Configuration file password” above, then import again"))
				case err != nil || path == "":
					return "", err
				}
				return i18n.T("已导入；本机的端口、局域网和接管设置没有变", "Imported; this machine's ports, LAN and takeover settings are as they were"), nil
			})
		})
}

// backupAction is optAction for an action that may come to nothing, a
// dialog the user closed: it then says nothing.
func (u *UI) backupAction(c *ui.Context, p *pal, key, label string, run func() (string, error)) {
	btn(c.Key(key), p, label).OnClick(func() {
		if u.app == nil || u.set.busy[key] {
			return
		}
		if u.set.busy == nil {
			u.set.busy = map[string]bool{}
		}
		u.set.busy[key] = true
		go func() {
			msg, err := run()
			if err != nil {
				msg = err.Error()
			}
			u.settingsUpdate(func() {
				delete(u.set.busy, key)
				if msg != "" {
					u.set.toasts = append(u.set.toasts, msg)
				}
			})
		}()
	})
}
