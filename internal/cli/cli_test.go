package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/mcp"
)

// fake serves tools on a socket as the app does, and returns the socket.
// calls collects what was called, as "tool args".
func fake(t *testing.T, tools map[string]func(args map[string]any) (any, error)) (socket string, calls *[]string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "bc") // short: unix socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	socket = filepath.Join(dir, "cli.sock")
	l, err := mcp.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	calls = new([]string)
	srv := &mcp.Server{Name: "fake"}
	for name, fn := range tools {
		srv.Tools = append(srv.Tools, mcp.Tool{Name: name, Run: func(_ context.Context, _ *mcp.Session, raw json.RawMessage) (any, error) {
			*calls = append(*calls, name+" "+string(raw))
			args := map[string]any{}
			_ = json.Unmarshal(raw, &args)
			return fn(args)
		}})
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Accept(ctx, l)
	t.Cleanup(func() { cancel(); os.RemoveAll(dir) })
	return socket, calls
}

// run runs a command line in English and returns what it printed.
func run(t *testing.T, env Env, args ...string) (code int, out, errOut string) {
	t.Helper()
	i18n.Set("en")
	t.Cleanup(func() { i18n.Set("zh") })
	var o, e bytes.Buffer
	env.Args, env.Stdout, env.Stderr = args, &o, &e
	if env.Version == "" {
		env.Version = "1.2.3"
	}
	code = Run(env)
	return code, o.String(), e.String()
}

func TestWantsGUI(t *testing.T) {
	for args, want := range map[string]bool{"": true, "-psn_0_123": true, "bowel://x": true, "status": false, "--help": false, "statsu": false} {
		var a []string
		if args != "" {
			a = []string{args}
		}
		if got := WantsGUI(a); got != want {
			t.Errorf("WantsGUI(%q) = %v", args, got)
		}
	}
}

func TestDataDir(t *testing.T) {
	rest, dir := DataDir([]string{"--data-dir", "/x", "status", "--json"})
	if dir != "/x" || strings.Join(rest, " ") != "status --json" {
		t.Errorf("%v %q", rest, dir)
	}
	rest, dir = DataDir([]string{"status", "--data-dir=/y"})
	if dir != "/y" || strings.Join(rest, " ") != "status" {
		t.Errorf("%v %q", rest, dir)
	}
}

// TestParse: commands, their flags wherever they stand, and arguments.
func TestParse(t *testing.T) {
	parsed := func(args ...string) (*Ctx, error) {
		c := &Ctx{flags: map[string]string{}}
		return c, parse(tree(), args, c)
	}
	c, err := parsed("conns", "--filter=git", "-n", "5", "--watch", "--json")
	if err != nil || pathName(c.path) != "bowel conns" || c.flag("filter") != "git" || c.intFlag("limit", 30) != 5 || !c.has("watch") || !c.has("json") {
		t.Errorf("conns: %v %v %v", err, c.flags, pathName(c.path))
	}
	c, err = parsed("rules", "add", "--kind", "domain", "example.com", "ai")
	if err != nil || pathName(c.path) != "bowel rules add" || strings.Join(c.args, " ") != "example.com ai" || c.flag("kind") != "domain" {
		t.Errorf("rules add: %v %v %v", err, c.args, c.flags)
	}
	// A group runs its default; an alias is its command.
	if c, err = parsed("providers"); err != nil || pathName(c.path) != "bowel providers list" {
		t.Errorf("providers: %v %s", err, pathName(c.path))
	}
	if c, err = parsed("rule", "rm", "x.com"); err != nil || pathName(c.path) != "bowel rules remove" {
		t.Errorf("alias: %v %s", err, pathName(c.path))
	}
	// After --, and for a number, a dash is no flag.
	if c, err = parsed("rules", "test", "--", "-weird"); err != nil || c.arg(0) != "-weird" {
		t.Errorf("--: %v %v", err, c.args)
	}
	// A raw command keeps its arguments.
	if c, err = parsed("devtools", "off", "--x"); err != nil || strings.Join(c.args, " ") != "off --x" {
		t.Errorf("raw: %v %v", err, c.args)
	}
	for _, bad := range [][]string{
		{"statsu"}, {"mode", "globl"}, {"mode", "rule", "extra"}, {"route", "use", "ai"}, {"conns", "--limit", "x"},
		{"conns", "--bogus"}, {"logs", "--level", "loud"}, {"conns", "--filter"}, {"tun"}, {"completion", "tcsh"},
	} {
		_, err := parsed(bad...)
		var ue *usageError
		if !errors.As(err, &ue) {
			t.Errorf("%v: want a usage error, got %v", bad, err)
		}
	}
	for _, help := range [][]string{{"--help"}, {"help"}, {"help", "rules", "add"}, {"rules", "add", "-h"}, {"mode", "--help", "bogus-arg"}} {
		if _, err := parsed(help...); !errors.Is(err, errHelp) {
			t.Errorf("%v: want help, got %v", help, err)
		}
	}
}

// TestExitCodes: 0 done, 1 failed, 2 a wrong command line; and what is
// said with each.
func TestExitCodes(t *testing.T) {
	socket, _ := fake(t, map[string]func(map[string]any) (any, error){
		"set_mode": func(a map[string]any) (any, error) {
			if a["mode"] == "direct" {
				return nil, errors.New("the core refused")
			}
			return "The mode is now " + a["mode"].(string) + ".", nil
		},
	})
	env := Env{Socket: socket}
	if code, out, _ := run(t, env, "mode", "global"); code != ExitOK || !strings.Contains(out, "✓ The proxy mode is Global") {
		t.Errorf("ok: %d %q", code, out)
	}
	if code, out, errOut := run(t, env, "mode", "direct"); code != ExitFail || out != "" || !strings.Contains(errOut, "the core refused") {
		t.Errorf("fail: %d %q %q", code, out, errOut)
	}
	code, out, errOut := run(t, env, "statsu")
	if code != ExitUsage || out != "" || !strings.Contains(errOut, "Unknown command statsu. Did you mean status?") || !strings.Contains(errOut, "bowel --help") {
		t.Errorf("usage: %d %q %q", code, out, errOut)
	}
	if code, _, errOut := run(t, env, "mode", "globl"); code != ExitUsage || !strings.Contains(errOut, "Did you mean global?") {
		t.Errorf("choice: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, env, "conns", "--wathc"); code != ExitUsage || !strings.Contains(errOut, "Did you mean --watch?") {
		t.Errorf("flag: %d %q", code, errOut)
	}
	// Not running: what to do about it, and 1.
	code, _, errOut = run(t, Env{Socket: filepath.Join(t.TempDir(), "none.sock")}, "status")
	if code != ExitFail || !strings.Contains(errOut, "bowel isn't running") || !strings.Contains(errOut, "bowel daemon") {
		t.Errorf("not running: %d %q", code, errOut)
	}
	// A tool the running bowel lacks is an older bowel.
	if code, _, errOut := run(t, env, "dns", "flush"); code != ExitFail || !strings.Contains(errOut, "older") {
		t.Errorf("old: %d %q", code, errOut)
	}
	// Commands of this process, and their failure.
	env.Local = map[string]func([]string) error{"devtools": func(a []string) error {
		if len(a) != 1 || a[0] != "off" {
			return errors.New("usage: bowel devtools off")
		}
		return nil
	}}
	if code, _, _ := run(t, env, "devtools", "off"); code != ExitOK {
		t.Errorf("local: %d", code)
	}
	if code, _, _ := run(t, env, "devtools"); code != ExitFail {
		t.Errorf("local failure: %d", code)
	}
}

func TestHelp(t *testing.T) {
	code, out, _ := run(t, Env{}, "--help")
	for _, want := range []string{"Usage:", "bowel <command>", "status", "daemon", "--json", "--data-dir", "Examples:", "bowel mode global"} {
		if code != ExitOK || !strings.Contains(out, want) {
			t.Errorf("help lacks %q (code %d):\n%s", want, code, out)
		}
	}
	_, out, _ = run(t, Env{}, "rules", "add", "--help")
	for _, want := range []string{"Send a domain or IP to a route", "bowel rules add <target> <route> [options]", "--kind suffix|domain|keyword|ip", "bowel rules add openai.com ai"} {
		if !strings.Contains(out, want) {
			t.Errorf("rules add help lacks %q:\n%s", want, out)
		}
	}
	// Every command says what it is and shows an example, in every language.
	var walk func(cmd *Command, path string)
	walk = func(cmd *Command, path string) {
		if cmd.Short.zh == "" || cmd.Short.en == "" {
			t.Errorf("%s: no summary", path)
		}
		if len(cmd.Sub) == 0 && len(cmd.Examples) == 0 {
			t.Errorf("%s: no example", path)
		}
		if len(cmd.Sub) == 0 && cmd.Run == nil {
			t.Errorf("%s: nothing to run", path)
		}
		for _, f := range cmd.Flags {
			if f.Help.zh == "" || f.Help.en == "" {
				t.Errorf("%s --%s: no help", path, f.Name)
			}
		}
		for _, s := range cmd.Sub {
			walk(s, path+" "+s.Name)
		}
	}
	walk(tree(), "bowel")
	i18n.Set("zh")
	var o bytes.Buffer
	Run(Env{Args: []string{"help", "logs"}, Stdout: &o, Stderr: &o})
	if !strings.Contains(o.String(), "查看内核日志") || !strings.Contains(o.String(), "用法：") {
		t.Errorf("Chinese help:\n%s", o.String())
	}
}

var statusJSON = map[string]any{
	"state": "degraded", "issue": map[string]any{"level": "warn", "text": "AI has no node"},
	"core": map[string]any{"state": "running", "version": "v1.19.0"}, "mode": "rule", "version": "0.2.2", "headless": true,
	"takeover": map[string]any{"on": true, "port": 7899,
		"system": map[string]any{"wanted": true, "status": "on"}, "tun": map[string]any{"wanted": false, "status": "off"}},
	"routes": []any{
		map[string]any{"id": "default", "name": "其他流量", "node": "hk1", "nodeLabel": "🇭🇰 香港 01", "auto": true},
		map[string]any{"id": "direct", "name": "Direct", "direct": true},
	},
	"activeConnections": 3,
	"traffic":           map[string]any{"downBps": 2048, "upBps": 10, "todayDown": 5 << 30, "todayUp": 1 << 20},
}

// TestRender: a table lines up whatever its text is, colors only on a
// terminal that wants them, and --json is the tool's answer.
func TestRender(t *testing.T) {
	slow := 800
	socket, _ := fake(t, map[string]func(map[string]any) (any, error){
		"get_status": func(map[string]any) (any, error) { return statusJSON, nil },
		"list_nodes": func(map[string]any) (any, error) {
			return map[string]any{"route": "default", "current": "hk1", "providers": map[string]string{"p1": "Sub"},
				"nodes": []any{
					map[string]any{"name": "hk1", "label": "🇭🇰 香港 01", "provider": "p1", "type": "ss", "delayMs": 45},
					map[string]any{"name": "jp", "label": "JP-Tokyo-02", "provider": "p1", "type": "vmess", "delayMs": slow},
					map[string]any{"name": "us", "label": "US", "provider": "p1", "type": "trojan", "delayMs": 0},
					map[string]any{"name": "sg", "label": "SG", "provider": "p1", "type": "trojan"},
				}}, nil
		},
	})
	env := Env{Socket: socket}

	code, out, _ := run(t, env, "status")
	for _, want := range []string{"● Degraded  AI has no node", "bowel 0.2.2 · mihomo v1.19.0 · no window", "Rule",
		"system proxy on · TUN off · port 7899", "↓ 2.00 KB/s", "today ↓ 5.00 GB", "3 connections", "🇭🇰 香港 01 (Auto)", "Direct"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("colors without a terminal:\n%q", out)
	}

	// The columns line up by what a terminal shows: 香 is two wide.
	_, out, _ = run(t, env, "nodes")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("nodes:\n%s", out)
	}
	col := func(line, word string) int { return dispWidth(line[:strings.Index(line, word)]) }
	if a, b, c := col(lines[0], "Delay"), col(lines[1], "45 ms"), col(lines[2], "800 ms"); a != b || b != c {
		t.Errorf("the delay column is at %d, %d, %d:\n%s", a, b, c, out)
	}
	if !strings.HasPrefix(lines[1], "●") || !strings.Contains(lines[3], "timeout") || !strings.Contains(lines[4], "—") {
		t.Errorf("nodes:\n%s", out)
	}

	// On a terminal: the light and the delays in their colors.
	env.TTY = true
	_, out, _ = run(t, env, "status")
	if !strings.Contains(out, "\x1b[33m● Degraded\x1b[0m") {
		t.Errorf("no yellow light:\n%q", out)
	}
	_, out, _ = run(t, env, "nodes")
	for _, want := range []string{"\x1b[32m45 ms", "\x1b[31m800 ms", "\x1b[31mtimeout", "\x1b[90m—"} {
		if !strings.Contains(out, want) {
			t.Errorf("nodes lack %q:\n%q", want, out)
		}
	}
	// NO_COLOR, --no-color and --json take them away.
	for _, e := range []struct {
		env  Env
		args []string
	}{
		{Env{Socket: socket, TTY: true, Getenv: func(k string) string { return map[string]string{"NO_COLOR": "1"}[k] }}, []string{"status"}},
		{env, []string{"status", "--no-color"}},
		{env, []string{"status", "--json"}},
	} {
		if _, out, _ := run(t, e.env, e.args...); strings.Contains(out, "\x1b[") {
			t.Errorf("%v: colors:\n%q", e.args, out)
		}
	}

	// --json: the answer itself, as JSON.
	_, out, _ = run(t, env, "status", "--json")
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["state"] != "degraded" || got["mode"] != "rule" {
		t.Errorf("status --json: %v\n%s", err, out)
	}
}

func TestWidth(t *testing.T) {
	for s, want := range map[string]int{"abc": 3, "香港": 4, "🇭🇰 香港 01": 10, "日本語": 6, "é": 1, "—": 1} {
		if got := dispWidth(s); got != want {
			t.Errorf("dispWidth(%q) = %d, want %d", s, got, want)
		}
	}
	if got := fmtBytes(1536); got != "1.50 KB" {
		t.Error(got)
	}
}

// TestChanges: what a change says, in words and as JSON.
func TestChanges(t *testing.T) {
	socket, calls := fake(t, map[string]func(map[string]any) (any, error){
		"set_takeover": func(a map[string]any) (any, error) {
			if a["enabled"] == false {
				return "Nothing to change.", nil
			}
			return map[string]any{"changed": []string{"enabled: false → true"}, "takeover": map[string]any{"on": true, "port": 7899,
				"system": map[string]any{"wanted": true, "status": "error", "detail": "no permission"}}}, nil
		},
		"get_settings": func(map[string]any) (any, error) {
			return map[string]any{"settings": map[string]any{"logLevel": "info", "mixedPort": 7899, "allowLan": false, "bypass": []string{"a"}},
				"choices": map[string][]string{"logLevel": {"debug", "info"}}}, nil
		},
		"update_settings": func(a map[string]any) (any, error) {
			return map[string]any{"changed": []string{"x"}}, nil
		},
	})
	env := Env{Socket: socket}
	code, out, errOut := run(t, env, "on")
	if code != 0 || !strings.Contains(out, "Taking over; the proxy port is 7899") || !strings.Contains(errOut, "system proxy: error (no permission)") {
		t.Errorf("on: %d %q %q", code, out, errOut)
	}
	if _, out, _ := run(t, env, "off"); !strings.Contains(out, "Not taking over already") {
		t.Errorf("off: %q", out)
	}
	_, out, _ = run(t, env, "on", "--json")
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["ok"] != true || got["takeover"] == nil {
		t.Errorf("on --json: %v %s", err, out)
	}
	_, out, _ = run(t, env, "off", "--json")
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["ok"] != true || got["message"] != "Nothing to change." {
		t.Errorf("off --json: %v %s", err, out)
	}

	// A setting's value goes as the kind it has: a word as a string, a
	// number, a switch and a list as JSON.
	*calls = nil
	for _, kv := range [][2]string{{"logLevel", "debug"}, {"mixedPort", "7900"}, {"allowLan", "true"}, {"bypass", `["x","y"]`}} {
		if code, _, errOut := run(t, env, "settings", "set", kv[0], kv[1]); code != 0 {
			t.Errorf("set %v: %s", kv, errOut)
		}
	}
	sent := strings.Join(*calls, "\n")
	for _, want := range []string{`{"logLevel":"debug"}`, `{"mixedPort":7900}`, `{"allowLan":true}`, `{"bypass":["x","y"]}`} {
		if !strings.Contains(sent, want) {
			t.Errorf("settings set did not send %s:\n%s", want, sent)
		}
	}
	if code, _, errOut := run(t, env, "settings", "set", "mode", "global"); code != ExitUsage || !strings.Contains(errOut, "bowel mode") {
		t.Errorf("set mode: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, env, "settings", "get", "logLevle"); code != ExitFail || !strings.Contains(errOut, "Did you mean logLevel?") {
		t.Errorf("get typo: %d %q", code, errOut)
	}
	if _, out, _ := run(t, env, "settings", "get", "logLevel"); out != "info\n" {
		t.Errorf("get: %q", out)
	}
}

// TestFollow: logs -f goes on with the new lines, and with --json each is
// an object on a line of its own.
func TestFollow(t *testing.T) {
	polls := 0
	socket, _ := fake(t, map[string]func(map[string]any) (any, error){
		"tail_logs": func(a map[string]any) (any, error) {
			polls++
			line := func(seq int, text string) map[string]any {
				return map[string]any{"seq": seq, "time": time.Unix(1700000000, 0), "level": "info", "text": text}
			}
			switch after := a["after"].(float64); {
			case after == 0:
				return map[string]any{"logs": []any{line(1, "one"), line(2, "two")}, "last": 2}, nil
			case after == 2:
				return map[string]any{"logs": []any{line(3, "three")}, "last": 3}, nil
			}
			return map[string]any{"logs": []any{}, "last": 3}, nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	env := Env{Socket: socket, Context: ctx, Poll: 20 * time.Millisecond}
	code, out, _ := run(t, env, "logs", "-f", "--json")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 3 || polls < 3 {
		t.Fatalf("logs -f --json: code %d, %d polls:\n%s", code, polls, out)
	}
	for i, l := range lines {
		var v struct {
			Seq  int
			Text string
		}
		if err := json.Unmarshal([]byte(l), &v); err != nil || v.Seq != i+1 {
			t.Errorf("line %d: %v %q", i, err, l)
		}
	}
	// Not following: once, and as one object with --json.
	_, out, _ = run(t, Env{Socket: socket}, "logs", "--json")
	var all struct{ Logs []any }
	if err := json.Unmarshal([]byte(out), &all); err != nil || len(all.Logs) != 2 {
		t.Errorf("logs --json: %v %s", err, out)
	}
	if _, out, _ = run(t, Env{Socket: socket}, "logs"); !strings.Contains(out, "info    one") {
		t.Errorf("logs: %q", out)
	}
}

func TestAgent(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if a := Agent(env(nil)); a != "" {
		t.Errorf("a person: %q", a)
	}
	if a := Agent(env(map[string]string{"CLAUDECODE": "1"})); a != "Claude Code" {
		t.Errorf("Claude Code: %q", a)
	}
	if a := Agent(env(map[string]string{"AI_AGENT": "some-agent"})); a != "some-agent" {
		t.Errorf("AI_AGENT: %q", a)
	}
}

func TestCompletion(t *testing.T) {
	for _, sh := range shells {
		code, out, _ := run(t, Env{}, "completion", sh)
		if code != 0 || !strings.Contains(out, "providers") || !strings.Contains(out, "rule global direct") || !strings.Contains(out, "--watch") {
			t.Errorf("%s:\n%s", sh, out)
		}
	}
	if s := suggest("xyzzy", []string{"status", "mode"}); s != "" {
		t.Errorf("suggest: %q", s)
	}
}
