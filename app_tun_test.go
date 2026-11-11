package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/settings"
	"github.com/oboo/moho/internal/tun"
)

func TestTunNeedsInstall(t *testing.T) {
	saved := osTun
	defer func() { osTun = saved }()
	installed := true
	osTun.installed = func(string) bool { return installed }
	a := &App{}
	if a.tunNeedsInstall() {
		t.Error("current copy: no authorization needed")
	}
	a.tunStale.Store(true)
	if !a.tunNeedsInstall() {
		t.Error("stale copy: needs one")
	}
	a.tunStale.Store(false)
	installed = false
	if !a.tunNeedsInstall() {
		t.Error("no copy: needs one")
	}
	// Linux, issue 71: a copy from before the polkit rule is asked for
	// once more, and not again once the rule is there.
	installed = true
	ruled, asked := false, 0
	osTun.needsRule = func(string) bool { return !ruled }
	osTun.install = func(string, string) error { asked++; ruled = true; return nil }
	m, err := core.New("/old", filepath.Join(t.TempDir(), "core"))
	if err != nil {
		t.Fatal(err)
	}
	a = &App{core: m, tunBin: "/tun"}
	for i := 0; i < 2; i++ {
		if a.tunNeedsInstall() {
			if err := a.installTun(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if asked != 1 {
		t.Errorf("asked %d times for the rule, want 1", asked)
	}
}

// TestRefusedTunUpdateKeepsCore: refusing leaves the warning and the core.
func TestRefusedTunUpdateKeepsCore(t *testing.T) {
	saved := osTun
	defer func() { osTun = saved }()
	osTun.install = func(string, string) error { return errors.New("refused") }
	m, err := core.New("/old", filepath.Join(t.TempDir(), "core"))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{core: m, tunBin: "/tun"}
	a.tunStale.Store(true)
	if err := a.installTun(); err == nil {
		t.Fatal("no error")
	}
	if !a.tunStale.Load() || m.Bin != "/old" {
		t.Errorf("stale %v, bin %s", a.tunStale.Load(), m.Bin)
	}
	osTun.install = func(string, string) error { return nil }
	if err := a.installTun(); err != nil {
		t.Fatal(err)
	}
	if a.tunStale.Load() || m.Bin != "/tun" {
		t.Errorf("stale %v, bin %s", a.tunStale.Load(), m.Bin)
	}
}

// TestStaleTunIsANoticeNotAFault: the light stays green, and it only shows
// to those who use TUN.
func TestStaleTunIsANoticeNotAFault(t *testing.T) {
	a := &App{}
	a.tunStale.Store(true)
	snap := Snapshot{Nodes: []NodeView{{Name: "n"}}}
	st, is := a.judge(true, settings.Settings{Tun: true}, core.Status{State: core.Running}, Takeover{}, snap, nil)
	if st != StateOK || is == nil || is.Action != "tun-update" {
		t.Fatalf("state %v, issue %+v", st, is)
	}
	if _, is := a.judge(true, settings.Settings{}, core.Status{State: core.Running}, Takeover{}, snap, nil); is != nil {
		t.Errorf("TUN off: %+v", is)
	}
}

// TestPortTakenIsAFault: the core runs and its controller answers, but
// nothing listens on the proxy port, so the light must not stay green.
func TestPortTakenIsAFault(t *testing.T) {
	a := &App{}
	snap := Snapshot{Nodes: []NodeView{{Name: "n"}}}
	st, is := a.judge(true, settings.Settings{}, core.Status{State: core.Running, Error: "端口 7899 被占用"}, Takeover{}, snap, nil)
	if st != StateError || is == nil || is.Detail != "端口 7899 被占用" {
		t.Fatalf("state %v, issue %+v", st, is)
	}
}

// On Windows the files the config names live inside the core's directory,
// where the task's core reads them with no SAFE_PATHS, and what an earlier
// version kept beside it is moved in with everything in it. Elsewhere
// nothing moves.
func TestStoreDir(t *testing.T) {
	data := t.TempDir()
	old := filepath.Join(data, "providers")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.json", "p-1.txt"} {
		if err := os.WriteFile(filepath.Join(old, f), []byte(f), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, goos := range []string{"darwin", "linux"} {
		if got := storeDir(data, "providers", goos); got != old {
			t.Fatalf("%s: %s", goos, got)
		}
	}
	if _, err := os.Stat(filepath.Join(old, "p-1.txt")); err != nil {
		t.Fatal("the files moved on a system that needs no move")
	}

	in := filepath.Join(data, "core", "providers")
	for i := range 2 { // the second start finds them moved
		if got := storeDir(data, "providers", "windows"); got != in {
			t.Fatalf("start %d: %s", i, got)
		}
		for _, f := range []string{"index.json", "p-1.txt"} {
			if b, err := os.ReadFile(filepath.Join(in, f)); err != nil || string(b) != f {
				t.Fatalf("start %d: %s: %q, %v", i, f, b, err)
			}
		}
	}
	if _, err := os.Lstat(old); err == nil {
		t.Error("the old directory is still there")
	}
	// The core's own check: inside its -d, as mihomo's IsSafePath asks.
	if rel, err := filepath.Rel(filepath.Join(data, "core"), filepath.Join(in, "p-1.txt")); err != nil || !filepath.IsLocal(rel) {
		t.Errorf("not inside the core's directory: %s", rel)
	}
	// A new installation has nothing to move.
	if got := storeDir(data, "rulesets", "windows"); got != filepath.Join(data, "core", "rulesets") {
		t.Errorf("new: %s", got)
	}
}

// A move that fails leaves the files where they are, in use.
func TestStoreDirKeepsWhatItCannotMove(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory that refuses a rename")
	}
	data := t.TempDir()
	old := filepath.Join(data, "providers")
	core := filepath.Join(data, "core")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(core, 0o500); err != nil { // nothing can be put in it
		t.Fatal(err)
	}
	defer os.Chmod(core, 0o700)
	if got := storeDir(data, "providers", "windows"); got != old {
		t.Fatalf("got %s", got)
	}
	_ = os.Chmod(core, 0o700)
	if got := storeDir(data, "providers", "windows"); got != filepath.Join(core, "providers") {
		t.Fatalf("the next start did not move it: %s", got)
	}
}

func TestTunEntry(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		wanted, on, installed bool
		fault                 string
		state                 core.State
		status, detail        string
	}{
		{"off", false, true, true, "", core.Running, "off", ""},
		{"paused", true, false, true, "", core.Running, "off", ""},
		{"on", true, true, true, "", core.Running, "on", ""},
		{"needs authorization", true, true, false, "", core.Running, "install", ""},
		{"core down", true, true, true, "", core.Failed, "error", "内核没有运行"},
		// The bundled core runs in its place: TUN is still not on, and
		// says why its own did not start.
		{"its core did not start", true, true, true, "计划任务没有把内核启动起来", core.Running, "error", "计划任务没有把内核启动起来"},
		{"its core did not start, unwanted", false, true, true, "x", core.Running, "off", ""},
		{"task deleted", true, true, false, tun.ErrGone.Error(), core.Running, "install", ""},
	} {
		e := tunEntry(tc.wanted, tc.on, tc.installed, tc.fault, tc.state)
		if e.Wanted != tc.wanted || e.Status != tc.status || !strings.Contains(e.Detail, tc.detail) || (tc.detail == "") != (e.Detail == "") {
			t.Errorf("%s: %+v", tc.name, e)
		}
	}
}

// Only a core that did not come up, and only the copy TUN runs on, is
// left for the bundled one; a deleted task also makes the installation
// one to make again, which making it clears.
func TestLeaveTunCore(t *testing.T) {
	saved := osTun
	defer func() { osTun = saved }()
	osTun.installed = func(string) bool { return true }
	osTun.install = func(string, string) error { return nil }
	newA := func(bin string) *App {
		m, err := core.New(bin, filepath.Join(t.TempDir(), "core"))
		if err != nil {
			t.Fatal(err)
		}
		return &App{core: m, coreBin: "/bundle", tunBin: "/tun"}
	}
	start := &core.StartError{Err: errors.New("the task didn't start the core")}

	a := newA("/tun")
	if a.leaveTunCore(nil) || a.leaveTunCore(errors.New("the config was refused")) {
		t.Fatal("left for an error that is not a start's")
	}
	if b := newA("/bundle"); b.leaveTunCore(start) || b.tunFault.Load() != nil {
		t.Fatal("the bundled core has nothing to fall back to")
	}
	if !a.leaveTunCore(start) || a.core.Bin != "/bundle" || a.tunFault.Load() == nil || *a.tunFault.Load() != start.Error() {
		t.Fatalf("bin %s, fault %v", a.core.Bin, a.tunFault.Load())
	}
	if a.tunGone.Load() || a.tunNeedsInstall() {
		t.Error("a task that is there needs no new authorization")
	}
	if a.leaveTunCore(start) {
		t.Error("left twice: the bundled core's own failure is final")
	}
	a.retryTunCore()
	if a.core.Bin != "/tun" || a.tunFault.Load() != nil {
		t.Fatalf("retry: bin %s, fault %v", a.core.Bin, a.tunFault.Load())
	}

	// The mark says installed and the task is gone.
	if !a.leaveTunCore(&core.StartError{Err: fmt.Errorf("spawn: %w", tun.ErrGone)}) || !a.tunGone.Load() || !a.tunNeedsInstall() {
		t.Fatal("a deleted task still counts as installed")
	}
	if err := a.installTun(); err != nil {
		t.Fatal(err)
	}
	if a.tunGone.Load() || a.tunFault.Load() != nil || a.tunNeedsInstall() || a.core.Bin != "/tun" {
		t.Errorf("after installing: gone %v, fault %v, bin %s", a.tunGone.Load(), a.tunFault.Load(), a.core.Bin)
	}
}

// TestTunCoreFailureKeepsACore is the report in issue 59, end to end with
// a real core: the copy TUN runs on passes the config check and then dies
// (as Windows' task core did), and the app must still have a core, say
// that TUN failed and why, and try again when TUN is asked for again.
func TestTunCoreFailureKeepsACore(t *testing.T) {
	if testing.Short() {
		t.Skip("starts the core")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for the failing core is a shell script")
	}
	a, _, run := cliApp(t)
	if !exists(bundledCore()) {
		t.Skip("no core; run scripts/fetch-core.sh")
	}
	saved := osTun
	defer func() { osTun = saved }()
	osTun.installed = func(string) bool { return true }
	osTun.install = func(string, string) error { t.Error("asked for an authorization"); return nil }

	// A core that checks a config as the real one does and never runs.
	runs := filepath.Join(a.dataDir, "tun-runs")
	bad := filepath.Join(a.dataDir, "tun-core")
	script := fmt.Sprintf("#!/bin/sh\nfor x in \"$@\"; do [ \"$x\" = -t ] && exec %q \"$@\"; done\necho run >> %q\necho 'level=fatal msg=\"Parse config error\"'\nexit 1\n", bundledCore(), runs)
	if err := os.WriteFile(bad, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	attempts := func() int {
		b, _ := os.ReadFile(runs)
		return strings.Count(string(b), "run")
	}
	a.tunBin = bad
	a.core.SetBin(bad)

	port, err := core.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.settings.Update(func(s *settings.Settings) {
		s.MixedPort, s.Language, s.Tun, s.Paused, s.SystemProxy = port, "en", true, false, false
	}); err != nil {
		t.Fatal(err)
	}
	i18n.Set("en")
	a.copyGeo()
	if err := a.applyConfig(); err != nil {
		t.Fatalf("no core at all: %v", err)
	}
	defer a.shutdown()

	check := func(when string, want int) {
		t.Helper()
		if cs := a.core.Status(); cs.State != core.Running || a.core.Bin != a.coreBin {
			t.Fatalf("%s: core %+v, running %s", when, cs, a.core.Bin)
		}
		tk := a.snapshot().Takeover.Tun
		if tk.Status != "error" || !strings.Contains(tk.Detail, "quit unexpectedly") {
			t.Fatalf("%s: TUN entry %+v", when, tk)
		}
		if n := attempts(); n != want {
			t.Fatalf("%s: TUN's core was started %d times, want %d", when, n, want)
		}
		cfg, err := os.ReadFile(a.core.ConfigPath())
		if err != nil || strings.Contains(string(cfg), "auto-route") {
			t.Fatalf("%s: the bundled core was given TUN: %v", when, err)
		}
	}
	check("first start", 1)

	// "tun on" in the error state is a retry, not "already on".
	code, out, errOut := run("", "tun", "on")
	if code != 0 || strings.Contains(out, "already") {
		t.Fatalf("tun on: %d %q %q", code, out, errOut)
	}
	if !strings.Contains(errOut+out, "quit unexpectedly") {
		t.Errorf("tun on does not say why TUN is not on: %q %q", out, errOut)
	}
	check("after tun on", 2)

	// And off is off: nothing is tried, and nothing is wrong.
	if code, _, errOut := run("", "tun", "off"); code != 0 {
		t.Fatalf("tun off: %d %q", code, errOut)
	}
	if tk := a.snapshot().Takeover.Tun; tk.Status != "off" || attempts() != 2 || a.core.Status().State != core.Running {
		t.Fatalf("after tun off: %+v, %d attempts, core %+v", tk, attempts(), a.core.Status())
	}
}
