package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oboo/moho/internal/i18n"
)

func TestParseKeys(t *testing.T) {
	got := parseKeys([]byte("j\x1b[A\x1b[B\x1b[5~\x1b[Z\r\t\x7f\x03香\x1b"))
	want := []string{"rune:j", "up", "down", "pgup", "backtab", "enter", "tab", "backspace", "ctrl-c", "rune:香", "esc"}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i, k := range got {
		s := k.name
		if k.name == "rune" {
			s += ":" + string(k.r)
		}
		if s != want[i] {
			t.Errorf("key %d: %s, want %s", i, s, want[i])
		}
	}
}

func TestClip(t *testing.T) {
	if got := clip("abcdef", 4); got != "abc…" {
		t.Errorf("%q", got)
	}
	if got := clip("香港节点", 5); got != "香港…" || visWidth(got) > 5 {
		t.Errorf("%q", got)
	}
	colored := "\x1b[32mgreen text\x1b[0m"
	if got := clip(colored, 6); visWidth(got) != 6 || !strings.HasSuffix(got, "\x1b[0m") || stripANSI(got) != "green…" {
		t.Errorf("%q", got)
	}
	if got := clip(colored, 40); got != colored {
		t.Errorf("%q", got)
	}
}

// screen is a tui on a fake bowel, in English, 100 by 24.
func screen(t *testing.T) (*tui, *[]string) {
	t.Helper()
	i18n.Set("en")
	t.Cleanup(func() { i18n.Set("zh") })
	mode := "rule"
	socket, calls := fake(t, map[string]func(map[string]any) (any, error){
		"get_status": func(map[string]any) (any, error) {
			st := map[string]any{}
			for k, v := range statusJSON {
				st[k] = v
			}
			st["mode"] = mode
			return st, nil
		},
		"set_mode": func(a map[string]any) (any, error) { mode = a["mode"].(string); return "ok", nil },
		"list_nodes": func(map[string]any) (any, error) {
			return map[string]any{"current": "hk1", "providers": map[string]string{"p1": "Sub"}, "nodes": []any{
				map[string]any{"name": "hk1", "label": "🇭🇰 香港 01", "provider": "p1", "type": "ss", "delayMs": 45},
				map[string]any{"name": "jp", "label": "JP-Tokyo-02", "provider": "p1", "type": "vmess", "delayMs": 800},
			}}, nil
		},
		"select_node": func(map[string]any) (any, error) { return "ok", nil },
		"list_connections": func(map[string]any) (any, error) {
			return map[string]any{"total": 1, "connections": []any{map[string]any{"id": "c1", "app": "curl", "host": "example.com", "port": 443,
				"route": "default", "node": "香港 01", "status": "active", "start": time.Unix(1700000000, 0)}}}, nil
		},
		"tail_logs": func(a map[string]any) (any, error) {
			if a["after"].(float64) > 0 {
				return map[string]any{"logs": []any{}, "last": 2}, nil
			}
			return map[string]any{"last": 2, "logs": []any{
				map[string]any{"seq": 1, "level": "info", "text": "started", "time": time.Unix(1700000000, 0)},
				map[string]any{"seq": 2, "level": "error", "text": "dial failed", "time": time.Unix(1700000001, 0)},
			}}, nil
		},
		"list_rules": func(map[string]any) (any, error) {
			return map[string]any{"rules": []any{map[string]any{"kind": "suffix", "value": "example.org", "route": "direct", "matches": "example.org and subdomains"}}}, nil
		},
		"remove_rule":    func(map[string]any) (any, error) { return map[string]any{}, nil },
		"add_rule":       func(map[string]any) (any, error) { return map[string]any{}, nil },
		"list_providers": func(map[string]any) (any, error) { return map[string]any{"providers": []any{}}, nil },
		"provider_add":   func(map[string]any) (any, error) { return map[string]any{}, nil },
		"set_takeover":   func(map[string]any) (any, error) { return map[string]any{}, nil },
		"set_gateway":    func(map[string]any) (any, error) { return map[string]any{}, nil },
		"get_settings": func(map[string]any) (any, error) {
			return map[string]any{"settings": map[string]any{"logLevel": "info", "mixedPort": 7899, "allowLan": false, "mcpPolicy": "ask", "mode": "rule"},
				"editable": []string{"mixedPort", "logLevel", "allowLan"}, "choices": map[string][]string{"logLevel": {"debug", "info"}}}, nil
		},
		"update_settings": func(map[string]any) (any, error) { return map[string]any{"changed": []string{"x"}}, nil },
	})
	c := &Ctx{env: Env{Socket: socket, Getenv: func(string) string { return "" }}, flags: map[string]string{}}
	t.Cleanup(func() {
		if c.cl != nil {
			c.cl.close()
		}
	})
	ui := &tui{c: c, w: 100, h: 24}
	ui.poll(true)
	return ui, calls
}

func keys(ui *tui, s string) {
	for _, k := range parseKeys([]byte(s)) {
		ui.handle(k)
	}
}

func text(ui *tui) string { return strings.Join(ui.view(), "\n") }

// TestTUIPages: each page shows what bowel answered, in a screen of the
// terminal's size.
func TestTUIPages(t *testing.T) {
	ui, _ := screen(t)
	lines := ui.view()
	if len(lines) != ui.h {
		t.Fatalf("%d lines for %d rows", len(lines), ui.h)
	}
	for i, l := range lines {
		if visWidth(l) > ui.w {
			t.Errorf("line %d is %d wide: %q", i, visWidth(l), l)
		}
	}
	for _, want := range []string{"Bowel", "● Degraded", "1 Overview", "6 Settings", "●  Takeover", "●  System proxy", "○  TUN", "Allow LAN",
		"No subscription yet", "Press a to add one", "🇭🇰 香港 01 (Auto)", "[⏎] Toggle / choose node   [a] Add subscription   [m] Mode   [?] Help   [q] Quit"} {
		if !strings.Contains(text(ui), want) {
			t.Errorf("the overview lacks %q:\n%s", want, text(ui))
		}
	}
	keys(ui, "2")
	if s := text(ui); !strings.Contains(s, "curl") || !strings.Contains(s, "example.com:443") || !strings.Contains(s, "[/] Filter") {
		t.Errorf("connections:\n%s", s)
	}
	keys(ui, "\t")
	if s := text(ui); !strings.Contains(s, "dial failed") || !strings.Contains(s, "started") || !strings.Contains(s, "Level: all") {
		t.Errorf("logs:\n%s", s)
	}
	keys(ui, "vv") // info, then warning and worse
	if s := text(ui); strings.Contains(s, "started") || !strings.Contains(s, "dial failed") || !strings.Contains(s, "Level: warning") {
		t.Errorf("logs by level:\n%s", s)
	}
	keys(ui, "4")
	if s := text(ui); !strings.Contains(s, "example.org") || !strings.Contains(s, "[a] Add   [d] Remove") {
		t.Errorf("rules:\n%s", s)
	}
	keys(ui, "5")
	if s := text(ui); !strings.Contains(s, "No Providers yet") || !strings.Contains(s, "[a] Add   [?] Help   [q] Quit") || strings.Contains(s, "Update") {
		t.Errorf("providers:\n%s", s)
	}
	keys(ui, "6")
	if s := text(ui); !strings.Contains(s, "allowLan") || !strings.Contains(s, "debug | info") || !strings.Contains(s, "mcpPolicy") || strings.Contains(s, "\n mode ") {
		t.Errorf("settings:\n%s", s)
	}
	keys(ui, "5?")
	if s := text(ui); !strings.Contains(s, "Keys") || !strings.Contains(s, "asks Y/n first") ||
		!strings.Contains(s, "Providers         [a] Add   [u] Update   [U] Update all   [e] On/off   [d] Remove") || !strings.Contains(s, "Choosing a node   [⏎] Use this node") {
		t.Errorf("help:\n%s", s)
	}
	keys(ui, "x\x1b[Z") // any key leaves the help; back a page
	if ui.help || ui.tab != tabRules {
		t.Errorf("after help: help %v, tab %d", ui.help, ui.tab)
	}
	// A narrow terminal keeps whole hints, help and quit among them.
	ui.tab, ui.w = tabHome, 60
	if bar := ui.view()[ui.h-1]; visWidth(bar) > 60 || strings.Contains(bar, "…") || !strings.Contains(bar, "[q] Quit") || !strings.Contains(bar, "[⏎]") {
		t.Errorf("a narrow key bar: %q", bar)
	}
	ui.tab = tabRules
	// Small terminals say so instead of drawing nonsense.
	ui.w, ui.h = 10, 5
	if s := text(ui); !strings.Contains(s, "too small") {
		t.Errorf("a small window: %q", s)
	}
	// Colors only when they are on.
	ui.w, ui.h = 100, 24
	if strings.Contains(text(ui), "\x1b[") {
		t.Errorf("colors that are off")
	}
	ui.c.color = true
	keys(ui, "1")
	// The name is on the theme color: the nearest of 256, or the color
	// itself where the terminal has them all; without one it is bold.
	ui.set.Settings = map[string]json.RawMessage{"accent": json.RawMessage(`"#DDFB4C"`)}
	if h := ui.view()[0]; !strings.Contains(h, "\x1b[1;30;48;5;191m Bowel \x1b[0m") {
		t.Errorf("the name: %q", h)
	}
	ui.c.env.Getenv = func(k string) string { return map[string]string{"COLORTERM": "truecolor"}[k] }
	if h := ui.view()[0]; !strings.Contains(h, "\x1b[1;30;48;2;221;251;76m Bowel \x1b[0m") {
		t.Errorf("the name in true color: %q", h)
	}
	ui.set.Settings["accent"] = json.RawMessage(`"ink"`)
	if h := ui.view()[0]; !strings.Contains(h, "\x1b[1mBowel\x1b[0m") {
		t.Errorf("the name without a theme color: %q", h)
	}
	if s := text(ui); !strings.Contains(s, "\x1b[33m● Degraded") || !strings.Contains(s, "▌") || !strings.Contains(s, chosenGround) {
		t.Errorf("no colors or no chosen row:\n%q", s)
	}
}

// TestTUIChanges: a change asks first and goes only on y; choosing a node,
// adding and removing a rule call the tools the commands call.
func TestTUIChanges(t *testing.T) {
	ui, calls := screen(t)
	called := func(want string) bool {
		for _, c := range *calls {
			if strings.HasPrefix(c, want) {
				return true
			}
		}
		return false
	}
	// The mode is taken from a list: the one in use is marked, Esc leaves
	// it as it is.
	keys(ui, "m")
	if ui.choose == nil || !strings.Contains(text(ui), "Proxy mode") || !strings.Contains(text(ui), "●  Rule") || !strings.Contains(text(ui), "[⏎] Choose") {
		t.Fatalf("m did not open the list:\n%s", text(ui))
	}
	keys(ui, "\x1b")
	if called("set_mode") || ui.choose != nil {
		t.Errorf("Esc let the mode change")
	}
	keys(ui, "m\r") // the one in use: nothing to do
	if called("set_mode") {
		t.Errorf("choosing the mode in use called the tool")
	}
	keys(ui, "mj\r")
	if !called(`set_mode {"mode":"global"}`) || ui.st.Mode != "global" || !strings.Contains(text(ui), "✓ The proxy mode is Global") {
		t.Errorf("the mode: %v\n%s", *calls, text(ui))
	}

	// The switches: Enter or Space asks, and y turns the one in front.
	keys(ui, "\r")
	if ui.confirm == nil || !strings.Contains(text(ui), "Turn Takeover off?  Y / n") {
		t.Fatalf("Enter on a switch did not ask:\n%s", text(ui))
	}
	keys(ui, "nj y")
	if called(`set_takeover {"enabled"`) || !strings.Contains(text(ui), "System proxy is off") || !called(`set_takeover {"systemProxy":false}`) || !strings.Contains(text(ui), "✓ System proxy is off") {
		t.Errorf("the system proxy switch: %v\n%s", *calls, text(ui))
	}
	keys(ui, "j\r\r") // TUN, which is off; Enter is yes
	if !called(`set_takeover {"tun":true}`) {
		t.Errorf("the TUN switch: %v", *calls)
	}
	keys(ui, "j yj y")
	if !called(`set_gateway {"enabled":true}`) || !called(`update_settings {"changes":{"allowLan":true}}`) {
		t.Errorf("the gateway and LAN switches: %v", *calls)
	}
	// A subscription is added from the overview too.
	keys(ui, "ahttps://example.com/sub\r")
	if !called(`provider_add {"source":"https://example.com/sub"}`) {
		t.Errorf("provider_add: %v", *calls)
	}

	// The first route's nodes: the one in use is marked, Enter takes one.
	keys(ui, "j\r")
	if ui.pick == nil || !strings.Contains(text(ui), "Choose a node for 其他流量") || !strings.Contains(text(ui), "45 ms") {
		t.Fatalf("the picker:\n%s", text(ui))
	}
	keys(ui, "j\r")
	if ui.pick != nil || !called(`select_node {"node":"jp","route":"default"}`) {
		t.Errorf("choosing a node: %v", *calls)
	}
	// A direct route has none to choose.
	keys(ui, "j\r")
	if ui.pick != nil || !strings.Contains(text(ui), "uses no node") {
		t.Errorf("a direct route:\n%s", text(ui))
	}

	// Rules: typed in, and removed after a y.
	keys(ui, "4a")
	if ui.input == nil {
		t.Fatal("a did not open the line")
	}
	keys(ui, "q x.com aix\x7f\r") // q is a letter here, not quit; three words are no rule
	if ui.quit {
		t.Errorf("q quit while typing")
	}
	if called("add_rule") {
		t.Errorf("three words went through as a rule: %v", *calls)
	}
	if !strings.Contains(text(ui), "Write it as: target route") {
		t.Errorf("no hint for a bad rule:\n%s", text(ui))
	}
	keys(ui, "aopenai.com ai\r")
	if !called(`add_rule {"route":"ai","target":"openai.com"}`) {
		t.Errorf("add_rule: %v", *calls)
	}
	keys(ui, "d")
	if ui.confirm == nil || !strings.Contains(text(ui), "Remove the rule for example.org?") {
		t.Fatalf("d did not ask:\n%s", text(ui))
	}
	keys(ui, "y")
	if !called(`remove_rule {"kind":"suffix","target":"example.org"}`) {
		t.Errorf("remove_rule: %v", *calls)
	}
	// Settings: a choice is taken from a list, a number is typed.
	*calls = nil
	keys(ui, "6j\r")
	if ui.choose == nil || !strings.Contains(text(ui), "●  info") || !strings.Contains(text(ui), "debug") {
		t.Fatalf("a choice did not open its list:\n%s", text(ui))
	}
	keys(ui, "k\rjj\r")
	if !called(`update_settings {"changes":{"logLevel":"debug"}}`) || ui.input == nil || string(ui.input.buf) != "7899" {
		t.Fatalf("settings: %v, input %+v", *calls, ui.input)
	}
	keys(ui, "\x7f\x7f00\r")
	if !called(`update_settings {"changes":{"mixedPort":7800}}`) {
		t.Errorf("a typed setting: %v", *calls)
	}
	// A filter is typed, kept, and cleared by Esc.
	keys(ui, "2/git\r")
	if ui.filter != "git" || !called(`list_connections {"filter":"git","limit":200}`) || !strings.Contains(text(ui), "Filter: git") {
		t.Errorf("the filter: %q %v", ui.filter, *calls)
	}
	keys(ui, "\x1b")
	if ui.filter != "" {
		t.Errorf("Esc kept the filter")
	}
	keys(ui, "q")
	if !ui.quit {
		t.Errorf("q did not quit")
	}
}

// TestTUIGone: when bowel goes away the screen says so, and comes back
// when it does.
func TestTUIGone(t *testing.T) {
	ui, _ := screen(t)
	good := ui.c.env.Socket
	ui.c.env.Socket = good + ".gone"
	ui.c.cl.close()
	ui.c.cl = nil
	ui.poll(false)
	if ui.err == nil || !strings.Contains(text(ui), "bowel isn't running") {
		t.Fatalf("no word of it:\n%s", text(ui))
	}
	ui.c.env.Socket = good
	ui.poll(false)
	if ui.err != nil || !strings.Contains(text(ui), "Degraded") {
		t.Errorf("it did not come back: %v", ui.err)
	}
}
