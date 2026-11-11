package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oboo/moho/internal/i18n"
)

func providersUIFixture() *UI {
	u := fixtureUI()
	u.page = "providers"
	u.snap.Providers[0].Usage.Expire = time.Now().Add(48 * 24 * time.Hour).Unix()
	u.snap.Providers[1].Usage.Expire = time.Now().Add(5 * 24 * time.Hour).Unix()
	u.snap.Providers[3].Error = "订阅下载失败：403 Forbidden"
	u.snap.Providers[3].Enabled = false
	return u
}

func TestProviderList(t *testing.T) {
	for _, dark := range []bool{false, true} {
		u := providersUIFixture()
		tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{Dark: dark})
		tt.Advance(time.Second)
		suffix := map[bool]string{false: "", true: "-dark"}[dark]
		shot(t, tt, "providers"+suffix)
		for _, s := range []string{"提供商", "4 个来源", "62 个节点 · 2 个需要关注", "全部更新",
			"机场 A", "机场 B", "自建 · 洛杉矶 VPS", "订阅 · https://sub.example-a.com/…", "已用", "/ 100 GB", "48 天后到期",
			"不限流量 · 不过期", "4/5 可用", "每 12 小时", "关闭", "更新", "启用", "订阅下载失败：403 Forbidden"} {
			if !tt.HasText(s) {
				t.Errorf("list lacks %q: %q", s, tt.Texts())
			}
		}

		// A row opens to its nodes.
		if err := tt.Click("机场 A"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(time.Second)
		if u.prov.open != "a" {
			t.Fatalf("open = %q", u.prov.open)
		}
		shot(t, tt, "providers-open"+suffix)
		for _, s := range []string{"出口位置", "香港 03 IPLC", "实测", "按名称", "Shadowsocks", "其他流量", "流媒体", "41 ms", "超时", "还有 37 个节点…", "重命名", "停用", "删除"} {
			if !tt.HasText(s) {
				t.Errorf("open row lacks %q: %q", s, tt.Texts())
			}
		}
		if dark {
			continue
		}

		// The auto-update interval opens its options, without opening the row.
		u.prov.open = ""
		tt.Frame()
		tt.Advance(time.Second)
		if err := tt.Click("每 12 小时"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(time.Second)
		if !tt.HasText("每 24 小时") || u.prov.open != "" {
			t.Errorf("auto-update options did not open alone: open=%q %q", u.prov.open, tt.Texts())
		}
		shot(t, tt, "providers-auto")
		tt.Key(0, ui.KeyEscape)
		tt.Advance(time.Second)
		if tt.HasText("每 24 小时") {
			t.Errorf("Escape did not close the options")
		}
		u.prov.open = "a"
		tt.Frame()
		tt.Advance(time.Second)

		// Renaming shows a small input.
		if err := tt.Click("重命名"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(time.Second)
		if u.prov.renaming != "a" || !tt.HasText("回车保存 · Esc 取消") || !tt.Focused("名称") {
			t.Errorf("rename did not start: %q", tt.Texts())
		}
		tt.Type(" 新")
		tt.Frame()
		shot(t, tt, "providers-rename")
		if u.prov.newName != "机场 A 新" {
			t.Errorf("typed name = %q", u.prov.newName)
		}
		tt.Key(0, ui.KeyEscape)
		tt.Advance(time.Second)
		if u.prov.renaming != "" || tt.HasText("回车保存 · Esc 取消") {
			t.Errorf("Escape did not end renaming")
		}
		if u.prov.open != "a" {
			t.Errorf("Escape while renaming closed the row")
		}

		// 删除, in the open row, asks to be pressed again, and stops asking.
		u.prov.open = "a"
		tt.Frame()
		tt.Advance(time.Second)
		if err := tt.Click("删除"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !tt.HasText("确认删除") {
			t.Errorf("delete does not confirm: %q", tt.Texts())
		}
		shot(t, tt, "providers-confirm")
		tt.Advance(5 * time.Second)
		if tt.HasText("确认删除") {
			t.Errorf("confirm stays")
		}

		// Updating shimmers.
		u.snap.Pending = []string{"providers", "provider:c"}
		tt.Frame()
		tt.Advance(300 * time.Millisecond)
		if !tt.HasText("正在更新 2 个订阅…") || !tt.HasText("正在更新…") {
			t.Errorf("pending lacks its words: %q", tt.Texts())
		}
		shot(t, tt, "providers-updating")
	}
}

func TestProviderAddBar(t *testing.T) {
	u := providersUIFixture()
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	if tt.HasText("自动识别") {
		t.Error("the add bar explains itself: its placeholder says what goes in")
	}

	// ⌘V outside the input pastes into it.
	tt.SetClipboard("https://sub.example.org/link/abc123?clash=1\n")
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Advance(time.Second)
	if u.prov.input != "https://sub.example.org/link/abc123?clash=1" {
		t.Fatalf("paste gave %q", u.prov.input)
	}
	if !tt.HasText("订阅") || !tt.HasText("· 下载 sub.example.org 的节点，之后按设定自动更新") {
		t.Errorf("subscription not detected: %q", tt.Texts())
	}
	if !tt.Focused("添加提供商") {
		t.Errorf("the input does not have the focus")
	}
	shot(t, tt, "providers-add-sub")

	u.prov.input = "vmess://eyJhZGQiOiJoay5leGFtcGxlLmNvbSJ9\ntrojan://pw@jp.example.com:443#东京\nhysteria2://pw@us.example.com:443#洛杉矶"
	tt.Frame()
	tt.Advance(time.Second)
	if !tt.HasText("节点链接") || !tt.HasText("· 3 个节点（vmess · trojan · hysteria2）") {
		t.Errorf("links not detected: %q", tt.Texts())
	}
	shot(t, tt, "providers-add-links")

	u.prov.input = "proxies:\n  - name: a\n    type: ss"
	tt.Frame()
	if !tt.HasText("本地文件") {
		t.Errorf("yaml not detected: %q", tt.Texts())
	}
	u.prov.input = "hello"
	tt.Frame()
	if !tt.HasText("无法识别：请粘贴订阅链接、节点链接或 Clash 配置") {
		t.Errorf("junk not refused: %q", tt.Texts())
	}

	// A detector set by the test is used.
	u.prov.detect = func(string) (string, error) { return "file", nil }
	u.prov.input = "anything"
	tt.Frame()
	if !tt.HasText("本地文件") {
		t.Errorf("detector not used: %q", tt.Texts())
	}

	// Adding while pending shimmers.
	u.snap.Pending = []string{"add"}
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("添加中…") {
		t.Errorf("adding lacks its word: %q", tt.Texts())
	}
	shot(t, tt, "providers-adding")
}

func TestProviderEmpty(t *testing.T) {
	for _, dark := range []bool{false, true} {
		u := fixtureUI()
		u.page = "providers"
		u.snap.Providers = nil
		tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{Dark: dark})
		tt.Advance(time.Second)
		for _, s := range []string{"0 个来源", "还没有节点来源", "订阅", "节点链接", "本地文件", "trojan://password@hk.example.com:443#香港 01"} {
			if !tt.HasText(s) {
				t.Errorf("empty state lacks %q: %q", s, tt.Texts())
			}
		}
		if tt.HasText("全部更新") {
			t.Errorf("empty state offers 全部更新")
		}
		if !tt.Focused("添加提供商") {
			t.Errorf("the input does not have the focus")
		}
		shot(t, tt, "providers-empty"+map[bool]string{false: "", true: "-dark"}[dark])
	}
}

// Rows come and go with the snapshot.
func TestProviderRowsAnimate(t *testing.T) {
	u := providersUIFixture()
	tt := ui.NewTester(u.view, 1060, 760)
	tt.Advance(time.Second)
	gone := u.snap.Providers[1]
	u.snap.Providers = append(u.snap.Providers[:1:1], u.snap.Providers[2:]...)
	tt.Frame()
	tt.Advance(150 * time.Millisecond)
	shot(t, tt, "providers-row-leaving")
	tt.Advance(time.Second)
	if tt.HasText("机场 B") || !tt.HasText("3 个来源") {
		t.Errorf("removed row stays: %q", tt.Texts())
	}
	u.snap.Providers = append(u.snap.Providers, gone)
	tt.Frame()
	tt.Advance(time.Second)
	if !tt.HasText("机场 B") {
		t.Errorf("added row missing")
	}
}

// A subscription whose nodes are untested says so, instead of a row of
// stubs; and the row fits in English.
func TestProviderRowUntestedEn(t *testing.T) {
	i18n.Set("en")
	defer i18n.Set("zh")
	u := providersUIFixture()
	for i := range u.snap.Nodes {
		if u.snap.Nodes[i].Provider == "a" {
			u.snap.Nodes[i].Tested, u.snap.Nodes[i].Delay = false, 0
		}
	}
	tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{Dark: true})
	tt.Advance(time.Second)
	shot(t, tt, "providers-en-untested")
	for _, s := range []string{"Not tested", "Auto-update", "Every 12 h", "Update", "Enable"} {
		if !tt.HasText(s) {
			t.Errorf("row lacks %q: %q", s, tt.Texts())
		}
	}
	if tt.HasText("Delete") || tt.HasText("Disable") {
		t.Errorf("closed rows show Delete/Disable: %q", tt.Texts())
	}
}
