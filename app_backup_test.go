package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/oboo/moho/internal/backup"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
)

// withBackupState gives a a provider of its own, a rule, and settings of
// its machine that are not the defaults.
func withBackupState(t *testing.T, a *App) string {
	t.Helper()
	p, err := a.providers.Add(context.Background(), nil, "trojan://pw@one.example:443#one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.settings.Update(func(s *settings.Settings) {
		s.Rules = []routes.UserRule{{Kind: "domain", Value: "kept.example", Route: "direct"}}
		s.LogLevel = "warning"
		s.TestURL = "https://kept.example/204"
		s.ExtSecret = "s3cret"
		s.Authentication = []string{"user:hunter2"}
		s.MCPPolicy = "deny"
	}); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// backupApp is an App with that state and no core to run: applying a
// config fails, as it does when the core refuses one.
func backupApp(t *testing.T) (*App, string) {
	t.Helper()
	t.Setenv("MOHO_CORE", filepath.Join(t.TempDir(), "no-core"))
	a, _, _ := mcpApp(t)
	return a, withBackupState(t, a)
}

// changeSince changes what an import would bring back, and the machine's
// settings it would not: another provider in place of the first, other
// rules.
func changeSince(t *testing.T, a *App, id string) (other string) {
	t.Helper()
	if err := a.providers.Delete(id); err != nil {
		t.Fatal(err)
	}
	p, err := a.providers.Add(context.Background(), nil, "trojan://pw@two.example:443#two")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.settings.Update(func(s *settings.Settings) {
		s.Rules = []routes.UserRule{{Kind: "domain", Value: "later.example", Route: "direct"}}
		s.LogLevel, s.TestURL = "debug", "https://later.example/204"
		s.ExtSecret = "another"
		s.MCPPolicy = "allow"
	}); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// coreHas says whether the core has a node of that name, from a provider.
func coreHas(a *App, name string) bool {
	a.refresh()
	for _, n := range a.snapshot().Nodes {
		if n.Display == name {
			return true
		}
	}
	return false
}

// TestConfigExportImport: with the real core, what is exported comes back
// on import, whatever was changed since, except the settings of the
// machine, which stay as they are; the core runs on it; and the import can
// be undone.
func TestConfigExportImport(t *testing.T) {
	a, _ := routingApp(t, nil)
	id := withBackupState(t, a)
	if err := a.applyConfig(); err != nil {
		t.Fatal(err)
	}
	if !coreHas(a, "one") {
		t.Fatal("the core does not have the provider's node")
	}
	port := a.settings.Get().MixedPort
	file := filepath.Join(t.TempDir(), "c."+backup.Ext)
	if err := a.exportConfig(file, "pw"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if !backup.Sealed(raw) || strings.Contains(string(raw), "one.example") {
		t.Fatal("the file with a password is not sealed")
	}
	b, err := backup.ReadFile(file, "pw")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"s3cret", "hunter2", strconv.Itoa(port), `"deny"`} {
		if strings.Contains(string(b.Settings), secret) {
			t.Errorf("the export holds %s, a setting of the machine", secret)
		}
	}
	if string(b.ProviderFiles[id]) != "trojan://pw@one.example:443#one" || len(b.ProviderFiles) != 2 {
		t.Errorf("the providers' files: %d, %q", len(b.ProviderFiles), b.ProviderFiles[id])
	}

	other := changeSince(t, a, id)
	if err := a.applyConfig(); err != nil {
		t.Fatal(err)
	}
	if coreHas(a, "one") || !coreHas(a, "two") {
		t.Fatal("the core still has the removed provider's node, or not the new one's")
	}

	// Without the password, and with a wrong one, nothing changes.
	for _, pw := range []string{"", "nope"} {
		if err := a.importConfig(file, pw); !backup.IsPassword(err) {
			t.Fatalf("password %q: %v", pw, err)
		}
	}
	if s := a.settings.Get(); s.LogLevel != "debug" || len(a.providers.List()) != 2 || a.canUndoImport() {
		t.Fatal("a file that did not open changed something")
	}

	if err := a.importConfig(file, "pw"); err != nil {
		t.Fatal(err)
	}
	s := a.settings.Get()
	if len(s.Rules) != 1 || s.Rules[0].Value != "kept.example" || s.LogLevel != "warning" || s.TestURL != "https://kept.example/204" {
		t.Errorf("what travels did not come back: %+v %s %s", s.Rules, s.LogLevel, s.TestURL)
	}
	if s.MixedPort != port || s.ExtSecret != "another" || s.MCPPolicy != "allow" || len(s.Authentication) != 1 {
		t.Errorf("the machine's settings changed: port %d, secret %q, policy %q", s.MixedPort, s.ExtSecret, s.MCPPolicy)
	}
	has := func(id string) bool {
		_, ok := a.providers.Get(id)
		return ok
	}
	if !has(id) || has(other) || len(a.providers.List()) != 2 {
		t.Fatalf("providers: %+v", a.providers.List())
	}
	if c, _ := a.providers.Content(id); string(c) != "trojan://pw@one.example:443#one" {
		t.Errorf("the provider's file: %q", c)
	}
	if _, err := os.Stat(a.providers.Path(other)); err == nil {
		t.Error("the replaced provider left its file")
	}
	if !coreHas(a, "one") {
		t.Error("the core does not have the imported provider's node")
	}
	if coreHas(a, "two") {
		t.Error("the core still has the replaced provider's node")
	}
	if !a.canUndoImport() || !a.snapshot().CanUndoImport {
		t.Fatal("nothing to undo after an import")
	}
	if fi, err := os.Stat(a.undoImportPath()); err != nil || fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("what the import replaced: %v %v", fi, err)
	}

	// Undone, it is as before the import; undone again, as after it.
	if err := a.undoImport(); err != nil {
		t.Fatal(err)
	}
	s = a.settings.Get()
	if has(id) || !has(other) || s.Rules[0].Value != "later.example" || s.LogLevel != "debug" || s.MixedPort != port {
		t.Fatalf("undone: %+v %+v", a.providers.List(), s.Rules)
	}
	if !coreHas(a, "two") || coreHas(a, "one") {
		t.Error("undone, the core does not have the node that was replaced, or still has the import's")
	}
	if err := a.undoImport(); err != nil {
		t.Fatal(err)
	}
	if !has(id) || has(other) || a.settings.Get().Rules[0].Value != "kept.example" {
		t.Fatalf("undone twice: %+v", a.providers.List())
	}
}

// same says whether a's configuration is what changeSince left.
func sameSince(a *App, other string) bool {
	s, ps := a.settings.Get(), a.providers.List()
	c, _ := a.providers.Content(other)
	return len(ps) == 1 && ps[0].ID == other && string(c) == "trojan://pw@two.example:443#two" &&
		len(s.Rules) == 1 && s.Rules[0].Value == "later.example" && s.LogLevel == "debug" && s.ExtSecret == "another"
}

// TestConfigImportRollsBack: when the core will not run on what a file
// holds, the providers and the settings are as before, files and all, and
// there is nothing to undo.
func TestConfigImportRollsBack(t *testing.T) {
	a, id := backupApp(t)
	file := filepath.Join(t.TempDir(), "c."+backup.Ext)
	if err := a.exportConfig(file, ""); err != nil {
		t.Fatal(err)
	}
	other := changeSince(t, a, id)
	if err := a.importConfig(file, ""); err == nil {
		t.Fatal("imported with no core to run it")
	}
	if !sameSince(a, other) {
		t.Fatalf("not as before: %+v %+v", a.providers.List(), a.settings.Get().Rules)
	}
	if _, err := os.Stat(a.providers.Path(id)); err == nil {
		t.Error("the file's provider left its file")
	}
	if a.canUndoImport() {
		t.Error("a failed import left something to undo")
	}
	if left, _ := filepath.Glob(filepath.Join(a.dataDir, "*.new")); len(left) > 0 {
		t.Errorf("left behind: %v", left)
	}
	if err := a.undoImport(); err == nil {
		t.Error("undid an import that never was")
	}
}

// TestConfigImportRefuses: a file that is not a configuration, or whose
// settings would not be valid here, changes nothing and leaves nothing to
// undo.
func TestConfigImportRefuses(t *testing.T) {
	a, id := backupApp(t)
	dir := t.TempDir()
	good, err := a.configBundle()
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, edit func(*backup.Bundle)) string {
		b := good
		b.ProviderFiles = map[string][]byte{id: good.ProviderFiles[id]}
		edit(&b)
		path := filepath.Join(dir, name)
		if err := backup.WriteFile(path, b, ""); err != nil {
			t.Fatal(err)
		}
		return path
	}
	notOne := filepath.Join(dir, "config.yaml")
	os.WriteFile(notOne, []byte("mixed-port: 7890\n"), 0o600)
	for name, path := range map[string]string{
		"not a configuration": notOne,
		"missing":             filepath.Join(dir, "none"),
		"a rule to no route": write("rule", func(b *backup.Bundle) {
			b.Settings = []byte(`{"rules":[{"kind":"domain","value":"x.example","route":"nowhere"}]}`)
		}),
		"settings cut short":   write("cut", func(b *backup.Bundle) { b.Settings = b.Settings[:len(b.Settings)/2] }),
		"a provider, no file":  write("nofile", func(b *backup.Bundle) { b.ProviderFiles = nil }),
		"providers not a list": write("list", func(b *backup.Bundle) { b.Providers = []byte(`{"id":"x"}`) }),
		"a provider's id is a path": write("path", func(b *backup.Bundle) {
			b.Providers = []byte(`[{"id":"../../x","name":"x","kind":"link","enabled":true}]`)
		}),
	} {
		err := a.importConfig(path, "")
		if err == nil || strings.Contains(err.Error(), "配置检查失败") {
			t.Errorf("%s: got as far as the core: %v", name, err)
		}
		s := a.settings.Get()
		if ps := a.providers.List(); len(ps) != 1 || ps[0].ID != id || len(s.Rules) != 1 || s.Rules[0].Value != "kept.example" || a.canUndoImport() {
			t.Fatalf("%s: something changed", name)
		}
	}

	// A file whose imported routing's provider is not in it: the routing
	// is bowel's own again, not a name for nothing.
	path := write("routing", func(b *backup.Bundle) { b.Settings = []byte(`{"importedRouting":"ffffff"}`) })
	c, err := readImport(path, "", a.settings.Get())
	if err != nil || c.settings.ImportedRouting != "" {
		t.Errorf("importedRouting = %q, %v", c.settings.ImportedRouting, err)
	}
	path = write("routing2", func(b *backup.Bundle) { b.Settings = []byte(`{"importedRouting":"` + id + `"}`) })
	if c, err = readImport(path, "", a.settings.Get()); err != nil || c.settings.ImportedRouting != id {
		t.Errorf("importedRouting = %q, %v", c.settings.ImportedRouting, err)
	}
}

// TestConfigCLI: the command line exports and imports; an agent that runs
// it may do neither, whatever the policy.
func TestConfigCLI(t *testing.T) {
	t.Setenv("MOHO_CORE", filepath.Join(t.TempDir(), "no-core"))
	a, _, run := cliApp(t)
	if _, err := a.settings.Update(func(s *settings.Settings) { s.Language, s.LogLevel, s.MCPPolicy = "en", "warning", "allow" }); err != nil {
		t.Fatal(err)
	}
	i18n.Set("en")
	file := filepath.Join(t.TempDir(), "c."+backup.Ext)
	code, out, errOut := run("", "config", "export", file)
	if code != 0 || !strings.Contains(out, "Exported to") || !strings.Contains(errOut, "not encrypted") {
		t.Fatalf("export: %d %q %q", code, out, errOut)
	}
	if _, err := backup.ReadFile(file, ""); err != nil {
		t.Fatal(err)
	}
	// With no core the import fails as it would on a config the core
	// refuses: the command says so, and nothing has changed.
	if _, err := a.settings.Update(func(s *settings.Settings) { s.LogLevel = "debug" }); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut = run("", "config", "import", file); code == 0 || errOut == "" || a.settings.Get().LogLevel != "debug" {
		t.Fatalf("import: %d %q", code, errOut)
	}
	if code, _, errOut = run("", "config", "import", "--undo"); code == 0 || !strings.Contains(errOut, "no import to undo") {
		t.Fatalf("undo: %d %q", code, errOut)
	}
	if code, _, _ = run("", "config", "import"); code == 0 {
		t.Error("import with neither a file nor --undo ran")
	}
	if code, _, _ = run("", "config", "import", file, "--undo"); code == 0 {
		t.Error("import with both a file and --undo ran")
	}

	other := filepath.Join(t.TempDir(), "agent."+backup.Ext)
	for _, args := range [][]string{{"config", "export", other}, {"config", "import", file}, {"config", "import", "--undo"}} {
		if code, _, errOut = run("claude", args...); code == 0 || !strings.Contains(errOut, "Only the user") {
			t.Errorf("an agent's %v: %d %q", args, code, errOut)
		}
	}
	if _, err := os.Stat(other); err == nil {
		t.Error("an agent exported the configuration")
	}
	// Nor are the tools on the agents' own socket.
	for _, tool := range a.mcpServer().Tools {
		if strings.HasPrefix(tool.Name, "config_") {
			t.Errorf("agents have %s", tool.Name)
		}
	}
}

// TestConfigImportForgetsFilePaths: where a local file came from is a path
// on the machine that exported it. Here nobody chose it, so it is not kept
// for 重新读取 to read; a subscription's link stays.
func TestConfigImportForgetsFilePaths(t *testing.T) {
	a, _ := backupApp(t)
	src := filepath.Join(t.TempDir(), "nodes.yaml")
	if err := os.WriteFile(src, []byte("proxies:\n  - {name: A, type: direct}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := a.providers.AddFile(src)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "c."+backup.Ext)
	if err := a.exportConfig(file, ""); err != nil {
		t.Fatal(err)
	}
	c, err := readImport(file, "", a.settings.Get())
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, q := range c.providers {
		switch {
		case q.ID == p.ID:
			seen = true
			if q.Source != "" {
				t.Errorf("the path came along: %q", q.Source)
			}
		case q.Kind == provider.Subscription && q.Source == "":
			t.Errorf("%s lost its link", q.Name)
		}
	}
	if !seen {
		t.Error("the file's provider is not in the export")
	}
}
