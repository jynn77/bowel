package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/oboo/moho/internal/cli"
	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/mcp"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

// cliApp is mcpApp serving the command line on its socket. run runs a
// command against it as a person would, or as the agent named.
func cliApp(t *testing.T) (a *App, asks *int, run func(agent string, args ...string) (int, string, string)) {
	t.Helper()
	a, asks, _ = mcpApp(t)
	l, err := mcp.Listen(cliSocket(a.dataDir))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go a.cliServer().Accept(ctx, l)
	run = func(agent string, args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := cli.Run(cli.Env{
			Args: args, Stdout: &out, Stderr: &errOut, Socket: cliSocket(a.dataDir), Version: "test",
			Getenv: func(k string) string {
				if k == "BOWEL_AGENT" {
					return agent
				}
				return ""
			},
		})
		return code, out.String(), errOut.String()
	}
	return a, asks, run
}

// TestCLIRoundTrip: commands reach the app over its socket and come back,
// reading and changing, in words and as JSON.
func TestCLIRoundTrip(t *testing.T) {
	a, asks, run := cliApp(t)
	if _, err := a.settings.Update(func(s *settings.Settings) { s.Language = "en" }); err != nil {
		t.Fatal(err)
	}
	i18n.Set("en")

	code, out, errOut := run("", "status")
	if code != 0 || !strings.Contains(out, "Not taking over") || !strings.Contains(out, "Rule") {
		t.Fatalf("status: %d\n%s\n%s", code, out, errOut)
	}
	code, out, _ = run("", "status", "--json")
	var st struct {
		State, Mode string
		Routes      []struct{ ID string }
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil || code != 0 || st.State != "off" || st.Mode != "rule" || len(st.Routes) == 0 {
		t.Fatalf("status --json: %v\n%s", err, out)
	}

	// A change: the user's own command is not asked about, whatever the
	// policy for agents.
	setPolicy(t, a, "deny")
	if code, out, errOut = run("", "mode", "global"); code != 0 || a.settings.Get().Mode != "global" || !strings.Contains(out, "Global") {
		t.Fatalf("mode global: %d %q %q", code, out, errOut)
	}
	if _, out, _ = run("", "mode"); out != "global\n" {
		t.Errorf("mode: %q", out)
	}
	if code, _, _ = run("", "mode", "rule"); code != 0 || a.settings.Get().Mode != "rule" {
		t.Errorf("mode rule: %d", code)
	}

	// What the core has no part in: where an address would go, the
	// interface's language, and the policy, since it is the user who asks.
	_, out, _ = run("", "rules", "test", "api.openai.com", "--json")
	var ex struct {
		Route string
		Sure  bool
	}
	if err := json.Unmarshal([]byte(out), &ex); err != nil || ex.Route != "ai" || !ex.Sure {
		t.Errorf("rules test --json: %v %s", err, out)
	}
	if code, out, errOut = run("", "settings", "set", "language", "ja"); code != 0 || a.settings.Get().Language != "ja" || !strings.Contains(out, "language: en → ja") {
		t.Errorf("settings set: %d %q %q", code, out, errOut)
	}
	if _, out, _ = run("", "settings", "get", "language"); out != "ja\n" {
		t.Errorf("settings get: %q", out)
	}
	run("", "settings", "set", "language", "en")
	if code, _, errOut = run("", "settings", "set", "language", "xx"); code != cli.ExitFail || !strings.Contains(errOut, "Choices") {
		t.Errorf("a bad value: %d %q", code, errOut)
	}
	if code, _, errOut = run("", "settings", "set", "mcpPolicy", "ask"); code != 0 || a.settings.Get().Policy() != "ask" {
		t.Errorf("the policy: %d %q", code, errOut)
	}
	for _, args := range [][]string{{"routes"}, {"nodes"}, {"providers"}, {"conns"}, {"logs"}, {"settings"}, {"routes", "--json"}, {"nodes", "--json"}} {
		if code, _, errOut = run("", args...); code != 0 {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
	}
	if *asks != 0 || len(agentChanges()) != 0 {
		t.Errorf("the user's commands asked %d times and left %d agent changes", *asks, len(agentChanges()))
	}
}

// TestCLIAgent: a command an agent runs follows the policy for agents, as
// on the agents' socket, and the agents' socket has none of the command
// line's own tools.
func TestCLIAgent(t *testing.T) {
	a, asks, run := cliApp(t)
	i18n.Set("en")

	setPolicy(t, a, "deny")
	if code, out, _ := run("Robot", "status"); code != 0 || out == "" {
		t.Errorf("an agent may read: %d", code)
	}
	code, _, errOut := run("Robot", "mode", "global")
	if code != cli.ExitFail || a.settings.Get().Mode != "rule" || !strings.Contains(errOut, "not to let agents make changes") {
		t.Errorf("deny let an agent's command through: %d %q", code, errOut)
	}
	if code, _, _ := run("Robot", "stop"); code != cli.ExitFail {
		t.Errorf("deny let an agent stop bowel")
	}
	// The policy is not an agent's to change, by this way either.
	if code, _, _ := run("Robot", "settings", "set", "mcpPolicy", "allow"); code != cli.ExitFail || a.settings.Get().Policy() != "deny" {
		t.Errorf("an agent changed the policy")
	}
	if ch := agentChanges(); len(ch) == 0 || ch[0].Client != "Robot (bowel CLI) test" || ch[0].Outcome != "denied" {
		t.Errorf("the refusal is not listed: %+v", ch)
	}

	// Asked: the user's answer decides.
	setPolicy(t, a, "ask")
	if code, _, _ := run("Robot", "mode", "global"); code != cli.ExitFail || *asks != 1 || a.settings.Get().Mode != "rule" {
		t.Errorf("a refused change: %d, asks %d", code, *asks)
	}
	setPolicy(t, a, "allow")
	if code, _, errOut := run("Robot", "mode", "global"); code != 0 || a.settings.Get().Mode != "global" {
		t.Errorf("allow: %d %q", code, errOut)
	}

	// A session that never says who it is is nobody's: the policy holds.
	setPolicy(t, a, "deny")
	c, err := net.DialTimeout("unix", cliSocket(a.dataDir), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_mode","arguments":{"mode":"direct"}}}` + "\n"))
	buf := make([]byte, 4096)
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _ := c.Read(buf)
	if !strings.Contains(string(buf[:n]), `"isError":true`) || a.settings.Get().Mode != "global" {
		t.Errorf("a session without initialize changed the mode: %s", buf[:n])
	}

	// The agents' server has only the agents' tools.
	for _, tool := range a.mcpServer().Tools {
		for _, own := range a.cliTools() {
			if tool.Name == own.Name {
				t.Errorf("the agents' server offers %s", tool.Name)
			}
		}
	}
}

// TestCLIWithCore drives the commands that reload the core against a real
// one, on a port of its own, and follows its log.
func TestCLIWithCore(t *testing.T) {
	if testing.Short() {
		t.Skip("starts the core")
	}
	a, _, run := cliApp(t)
	if !exists(bundledCore()) {
		t.Skip("no core; run scripts/fetch-core.sh")
	}
	port, err := core.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.settings.Update(func(s *settings.Settings) { s.MixedPort, s.Language = port, "en" }); err != nil {
		t.Fatal(err)
	}
	i18n.Set("en")
	a.copyGeo()
	if err := a.applyConfig(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.shutdown()

	// Rules: add, list, explain, remove.
	code, out, errOut := run("", "rules", "add", "example.org", "direct")
	if code != 0 || !strings.Contains(out, "example.org now goes to direct") {
		t.Fatalf("rules add: %d %q %q", code, out, errOut)
	}
	if rs := a.settings.Get().Rules; len(rs) != 1 || rs[0].Route != "direct" || rs[0].Kind != routes.KindSuffix {
		t.Fatalf("rules: %+v", rs)
	}
	if _, out, _ := run("", "rules"); !strings.Contains(out, "example.org") || !strings.Contains(out, "direct") {
		t.Errorf("rules list:\n%s", out)
	}
	if _, out, _ := run("", "rules", "test", "https://www.example.org/x"); !strings.Contains(out, "www.example.org → direct") || !strings.Contains(out, "your rule") {
		t.Errorf("rules test:\n%s", out)
	}
	if code, _, errOut := run("", "rules", "remove", "example.org"); code != 0 || len(a.settings.Get().Rules) != 0 {
		t.Errorf("rules remove: %d %q", code, errOut)
	}
	if code, _, errOut := run("", "rules", "add", "example.org", "nowhere"); code != cli.ExitFail || !strings.Contains(errOut, "No such route") {
		t.Errorf("a bad route: %d %q", code, errOut)
	}

	if code, out, errOut := run("", "settings", "set", "logLevel", "debug"); code != 0 || a.settings.Get().LogLevel != "debug" || !strings.Contains(out, "logLevel: info → debug") {
		t.Errorf("settings set: %d %q %q", code, out, errOut)
	}
	if code, out, errOut := run("", "status"); code != 0 || !strings.Contains(out, "mihomo v") {
		t.Errorf("status: %d %q %q", code, out, errOut)
	}
	if code, _, errOut := run("", "dns", "flush"); code != 0 {
		t.Errorf("dns flush: %d %q", code, errOut)
	}
}
