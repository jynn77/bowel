package tun

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestInstalled(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(f, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 && Installed(f) {
		t.Error("a file of the user's counts as installed")
	}
	// The setuid bit alone is not enough: the owner must be root.
	if err := os.Chmod(f, 0o4755); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 && Installed(f) {
		t.Error("a setuid file of the user's counts as installed")
	}
	if Installed(filepath.Join(dir, "missing")) || Installed(dir) {
		t.Error("a missing file or a directory counts as installed")
	}
}

// vfsCaps builds a security.capability attribute of revision 2.
func vfsCaps(effective bool, caps ...int) []byte {
	b := make([]byte, 20)
	magic := uint32(0x02000000)
	if effective {
		magic |= 1
	}
	binary.LittleEndian.PutUint32(b, magic)
	for _, c := range caps {
		off := 4 + 8*(c/32)
		binary.LittleEndian.PutUint32(b[off:], binary.LittleEndian.Uint32(b[off:])|1<<(c%32))
	}
	return b
}

func TestCapsAllow(t *testing.T) {
	admin := unix.CAP_NET_ADMIN
	if !capsAllow(vfsCaps(true, admin, unix.CAP_NET_BIND_SERVICE, unix.CAP_NET_RAW), admin) {
		t.Error("what setcap writes doesn't count")
	}
	if capsAllow(vfsCaps(false, admin), admin) {
		t.Error("permitted but not effective counts")
	}
	if capsAllow(vfsCaps(true, unix.CAP_NET_RAW), admin) {
		t.Error("another capability counts")
	}
	if !capsAllow(vfsCaps(true, unix.CAP_BPF), unix.CAP_BPF) {
		t.Error("a capability of the second word doesn't count")
	}
	for _, b := range [][]byte{nil, {1, 0, 0, 2}, vfsCaps(true, admin)[:11]} {
		if capsAllow(b, admin) {
			t.Errorf("a short attribute counts: %v", b)
		}
	}
	// Revision 1 has one word: nothing of the second is in it.
	v1 := vfsCaps(true, admin)[:12]
	binary.LittleEndian.PutUint32(v1, 0x01000001)
	if !capsAllow(v1, admin) || capsAllow(v1, unix.CAP_BPF) {
		t.Error("revision 1 read wrong")
	}
}

// The copy becomes root's before it gets its rights (chown drops them),
// and takes its name last. With no setcap it is setuid instead.
func TestInstallStep(t *testing.T) {
	st := installStepWith("/usr/sbin/setcap", "/h/a b/.mihomo-1", "/h/a b/mihomo")
	got := script([]Step{st})
	for _, w := range []string{" root:root '/h/a b/.mihomo-1' && ", " 0755 '/h/a b/.mihomo-1' && /usr/sbin/setcap " + fileCaps + " '/h/a b/.mihomo-1' && ", " -f '/h/a b/.mihomo-1' '/h/a b/mihomo'"} {
		if !strings.Contains(got, w) {
			t.Errorf("script lacks %q:\n%s", w, got)
		}
	}
	if strings.Contains(got, "4755") {
		t.Errorf("setuid with setcap there:\n%s", got)
	}
	if i, j, k := strings.Index(got, "chown"), strings.Index(got, "setcap"), strings.Index(got, "mv"); !(i < j && j < k) {
		t.Errorf("order:\n%s", got)
	}
	if len(st)%2 != 0 || st[len(st)-1] != "/h/a b/mihomo" || st[1] != "/h/a b/.mihomo-1" {
		t.Errorf("step = %q", st)
	}
	suid := script([]Step{installStepWith("setcap", "/t", "/d")})
	if !strings.Contains(suid, " 4755 '/t'") || strings.Contains(suid, "setcap") {
		t.Errorf("without setcap:\n%s", suid)
	}
}

// Paths are one word each whatever is in them, several steps stop at the
// first failure, and sh accepts the whole (nothing runs: sh -n only reads).
func TestScript(t *testing.T) {
	s := script([]Step{{"/bin/echo ", `/a b/it's "x"\y`}, {"/bin/true ", "/c;d", " ", "$(e)"}})
	want := `/bin/echo '/a b/it'\''s "x"\y' && /bin/true '/c;d' '$(e)'`
	if s != want {
		t.Fatalf("script:\n got %s\nwant %s", s, want)
	}
	if out, err := exec.Command("/bin/sh", "-n", "-c", s).CombinedOutput(); err != nil {
		t.Errorf("sh -n: %v: %s", err, out)
	}
	out, err := exec.Command("/bin/sh", "-c", script([]Step{{"printf %s ", `it's $HOME "x"`}})).Output()
	if err != nil || string(out) != `it's $HOME "x"` {
		t.Errorf("quoting: %q, %v", out, err)
	}
	if err := Admin("p"); err != nil {
		t.Fatalf("Admin with no steps: %v", err)
	}
}

func TestAdminError(t *testing.T) {
	e := errors.New("exit status")
	if adminError(126, "Request dismissed", e) != ErrCanceled {
		t.Error("a dismissed dialog is not ErrCanceled")
	}
	if err := adminError(127, "", e); err == ErrCanceled || !strings.Contains(err.Error(), "exit status") {
		t.Errorf("127: %v", err)
	}
	if err := adminError(1, "chown: no", e); !strings.Contains(err.Error(), "chown: no") {
		t.Errorf("1: %v", err)
	}
}

// TestInstallStepKeepsCurrentCopy: until the authorization, the copy TUN
// runs on stays as it is, and the step only names a copy beside it.
func TestInstallStepKeepsCurrentCopy(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "new"), filepath.Join(dir, "bin", "mihomo")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	left := filepath.Join(dir, "bin", ".mihomo-left")
	for p, c := range map[string]string{src: "new", dst: "old", left: "x"} {
		if err := os.WriteFile(p, []byte(c), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if checkDir(filepath.Dir(dst)) != nil {
		t.Skip("the test's directory is mounted nosuid")
	}
	step, err := InstallStep(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "old" {
		t.Errorf("dst = %q before the authorization", b)
	}
	if _, err := os.Stat(left); err == nil {
		t.Error("the copy of a refused authorization is still there")
	}
	if b, _ := os.ReadFile(step[1]); string(b) != "new" {
		t.Errorf("copy = %q", b)
	}
	if step[len(step)-1] != dst {
		t.Errorf("step = %v", step)
	}
}

// With BOWEL_TUN_REAL naming a directory, the real thing: a copy that
// root owns and setcap has marked (as installStep's commands leave it)
// counts as installed, and one of them alone does not. The directory must
// hold "caps" (root's, with fileCaps), "plain" (root's, no rights) and
// "user" (the user's).
func TestInstalledReal(t *testing.T) {
	dir := os.Getenv("BOWEL_TUN_REAL")
	if dir == "" {
		t.Skip("BOWEL_TUN_REAL is not set")
	}
	if !Installed(filepath.Join(dir, "caps")) {
		t.Error("root's copy with capabilities doesn't count as installed")
	}
	if Installed(filepath.Join(dir, "plain")) {
		t.Error("root's copy with no rights counts as installed")
	}
	if os.Geteuid() != 0 && Installed(filepath.Join(dir, "user")) {
		t.Error("the user's copy counts as installed")
	}
}

// ruleMachine makes the machine one the rule is for: a user, setcap and
// resolvectl there, polkit reading rules from a directory of the test's.
func ruleMachine(t *testing.T) (rules string) {
	t.Helper()
	rules = t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "setcap"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	sd, si, su, sl, st := rulesDir, ruleUID, ruleUser, ruleLook, toolDirs
	t.Cleanup(func() {
		rulesDir, ruleUID, ruleUser, ruleLook, toolDirs = sd, si, su, sl, st
		SetDevice("")
	})
	rulesDir, toolDirs = rules, []string{bin}
	ruleUID = func() int { return 1000 }
	ruleUser = func() string { return "reeza" }
	ruleLook = func(string) (string, error) { return "/usr/bin/resolvectl", nil }
	SetDevice("")
	return rules
}

// The rule allows the four actions resolvectl asks for, to one user, and
// only for the TUN device where polkit is told the link. Names are quoted
// as JavaScript strings whatever is in them.
func TestDNSRule(t *testing.T) {
	r := dnsRule("reeza", "Meta")
	for _, w := range []string{
		`"org.freedesktop.resolve1.set-dns-servers"`, `"org.freedesktop.resolve1.set-domains"`,
		`"org.freedesktop.resolve1.set-default-route"`, `"org.freedesktop.resolve1.revert"`,
		`subject.user !== "reeza"`, `action.lookup("interface")`, `link && link !== "Meta"`, "polkit.Result.YES",
	} {
		if !strings.Contains(r, w) {
			t.Errorf("rule lacks %s:\n%s", w, r)
		}
	}
	if n := strings.Count(r, "org.freedesktop."); n != 4 {
		t.Errorf("%d actions, want 4:\n%s", n, r)
	}
	odd := dnsRule(`a"b\c`+"\n", `d"; return polkit.Result.YES; //`)
	if !strings.Contains(odd, `"a\"b\\c\n"`) || !strings.Contains(odd, `"d\"; return polkit.Result.YES; //"`) {
		t.Errorf("names not quoted:\n%s", odd)
	}
}

// The rule is for a machine where the core runs as a user and calls
// resolvectl, and polkit reads rules.d: nowhere else is one written or
// asked for.
func TestRuleApplies(t *testing.T) {
	ruleMachine(t)
	if !ruleApplies() {
		t.Fatal("a user with setcap, resolvectl and rules.d: the rule applies")
	}
	ruleUID = func() int { return 0 }
	if ruleApplies() {
		t.Error("root needs no rule")
	}
	ruleUID = func() int { return 1000 }
	ruleLook = func(string) (string, error) { return "", exec.ErrNotFound }
	if ruleApplies() {
		t.Error("no resolvectl: the core calls nothing")
	}
	ruleLook = func(string) (string, error) { return "/usr/bin/resolvectl", nil }
	saved := toolDirs
	toolDirs = []string{t.TempDir()}
	if ruleApplies() {
		t.Error("no setcap: the copy is setuid, and root is not asked")
	}
	toolDirs = saved
	rulesDir = filepath.Join(rulesDir, "none")
	if ruleApplies() {
		t.Error("no rules.d (polkit 0.105): nothing reads the rule")
	}
}

// An installation lacks the rule until its mark says the same as the rule
// that would be written now (the device's name is part of it) and the
// rule is there to be read, saying the same.
func TestNeedsRule(t *testing.T) {
	rules := ruleMachine(t)
	dst := filepath.Join(t.TempDir(), "mihomo")
	rule := filepath.Join(rules, "49-bowel-tun-1000.rules")
	want := dnsRule("reeza", "Meta")
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !NeedsRule(dst) {
		t.Fatal("no mark: the rule is missing")
	}
	write(ruleMarkPath(dst), want)
	if !NeedsRule(dst) {
		t.Error("the mark without the rule (deleted by hand) went unnoticed")
	}
	write(rule, want)
	if NeedsRule(dst) {
		t.Error("the mark and the rule are what would be written: nothing to ask")
	}
	write(rule, want+"// edited\n")
	if !NeedsRule(dst) {
		t.Error("a rule changed by hand went unnoticed")
	}
	write(rule, want)
	SetDevice("bowel0")
	if !NeedsRule(dst) {
		t.Error("another device than the rule names")
	}
	SetDevice("Meta")
	if NeedsRule(dst) {
		t.Error("the default device, named")
	}

	// polkit's directory closed to users: the mark is taken at its word.
	if os.Geteuid() != 0 {
		if err := os.Chmod(rules, 0); err != nil {
			t.Fatal(err)
		}
		if NeedsRule(dst) {
			t.Error("a rule that cannot be read is asked for again")
		}
		if err := os.Chmod(rules, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// A rule polkit's directory refused is not asked for every time.
	if err := os.Remove(rule); err != nil {
		t.Fatal(err)
	}
	write(ruleMarkPath(dst), want+refusedNote)
	if NeedsRule(dst) {
		t.Error("a refused rule is asked for again")
	}

	ruleLook = func(string) (string, error) { return "", exec.ErrNotFound }
	SetDevice("bowel0")
	if NeedsRule(dst) {
		t.Error("asked where no rule applies")
	}
}

// Uninstall leaves neither the copy, the mark nor the rule, under one
// authorisation; with no rule there it asks for none; and a canceled one
// keeps the mark, which still says a rule is there.
func TestUninstall(t *testing.T) {
	rules := ruleMachine(t)
	dir := filepath.Join(t.TempDir(), "a b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst, rule := filepath.Join(dir, "mihomo"), filepath.Join(rules, "49-bowel-tun-1000.rules")
	other := filepath.Join(rules, "49-bowel-tun-1001.rules")
	mark := ruleMarkPath(dst)
	want := dnsRule("reeza", "Meta")
	put := func(paths ...string) {
		t.Helper()
		for _, p := range paths {
			if err := os.WriteFile(p, []byte(want), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	gone := func(p string) bool {
		_, err := os.Lstat(p)
		return errors.Is(err, fs.ErrNotExist)
	}
	var asked []string
	var refuse error
	saved := adminRun
	t.Cleanup(func() { adminRun = saved })
	adminRun = func(_ string, steps []Step) error {
		sh := script(steps)
		asked = append(asked, sh)
		if refuse != nil {
			return refuse
		}
		return exec.Command("/bin/sh", "-c", sh).Run()
	}

	put(dst, mark, rule, other)
	if err := Uninstall(dst); err != nil {
		t.Fatal(err)
	}
	if !gone(dst) || !gone(mark) || !gone(rule) {
		t.Errorf("left behind: copy %v, mark %v, rule %v", !gone(dst), !gone(mark), !gone(rule))
	}
	if gone(other) {
		t.Error("another user's rule was removed")
	}
	if len(asked) != 1 || !strings.Contains(asked[0], " -f "+shellQuote(rule)) || strings.Contains(asked[0], shellQuote(dst)) {
		t.Errorf("authorisations: %q", asked)
	}

	// No rule: nothing is asked, as before there was one. A refused
	// rule's mark goes the same way.
	asked = nil
	put(dst)
	if err := os.WriteFile(mark, []byte(want+refusedNote), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(dst); err != nil || len(asked) != 0 || !gone(dst) || !gone(mark) {
		t.Errorf("no rule: %v, asked %q, copy gone %v, mark gone %v", err, asked, gone(dst), gone(mark))
	}
	if err := Uninstall(dst); err != nil || len(asked) != 0 {
		t.Errorf("nothing installed: %v, asked %q", err, asked)
	}

	// Canceled: the rule is still there, and so is the mark.
	put(dst, mark, rule)
	refuse = ErrCanceled
	if err := Uninstall(dst); !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled: %v", err)
	}
	if gone(mark) || gone(rule) {
		t.Error("a canceled removal dropped the mark or the rule")
	}
	refuse = nil

	// polkit's directory closed to users: the mark says a rule is there.
	if os.Geteuid() != 0 {
		asked = nil
		put(dst)
		if err := os.Chmod(rules, 0); err != nil {
			t.Fatal(err)
		}
		err := Uninstall(dst)
		if cerr := os.Chmod(rules, 0o755); cerr != nil {
			t.Fatal(cerr)
		}
		// The stand-in for root cannot enter the directory either.
		if len(asked) != 1 || !strings.Contains(asked[0], shellQuote(rule)) {
			t.Errorf("closed directory: %v, asked %q", err, asked)
		}
	}
}

// The rule rides in the installation's own step, after the copy has its
// name, under the same pkexec; its failure does not fail the installation,
// and the mark is left either way. Run for real with stand-ins for the
// root commands.
func TestInstallStepWithRule(t *testing.T) {
	rules := ruleMachine(t)
	dir := filepath.Join(t.TempDir(), "a b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tmp, dst := filepath.Join(dir, ".mihomo-1"), filepath.Join(dir, "mihomo")
	st, err := installStep(tmp, dst)
	if err != nil {
		t.Fatal(err)
	}
	got := script([]Step{st, {"/bin/true ", "/x"}})
	rule := filepath.Join(rules, "49-bowel-tun-1000.rules")
	if i, j, k := strings.Index(got, shellQuote(dst)), strings.Index(got, shellQuote(rule)), strings.Index(got, shellQuote(ruleMarkPath(dst))); !(0 < i && i < j && j < k) {
		t.Errorf("order:\n%s", got)
	}
	if !strings.Contains(got, " -m 0644 -o root -g root ") || !strings.HasSuffix(got, "; } && /bin/true '/x'") {
		t.Errorf("script:\n%s", got)
	}
	if out, err := exec.Command("/bin/sh", "-n", "-c", got).CombinedOutput(); err != nil {
		t.Errorf("sh -n: %v: %s", err, out)
	}
	staged, _ := filepath.Glob(filepath.Join(dir, ".mihomo-rule-*"))
	if len(staged) != 1 {
		t.Fatalf("staged rule: %v", staged)
	}
	if b, _ := os.ReadFile(staged[0]); string(b) != dnsRule("reeza", "Meta") {
		t.Errorf("staged rule:\n%s", b)
	}

	// The rule's own commands, with cp for install: the rule lands and
	// the mark is the same text.
	run := func(st Step) error {
		sh := "true" + script([]Step{st})
		sh = strings.ReplaceAll(sh, tool("install")+" -m 0644 -o root -g root ", "cp ")
		return exec.Command("/bin/sh", "-c", sh).Run()
	}
	refused, _ := filepath.Glob(filepath.Join(dir, ".mihomo-norule-*"))
	if len(refused) != 1 {
		t.Fatalf("staged refusal: %v", refused)
	}
	if err := run(ruleStepWith(staged[0], refused[0], rule, ruleMarkPath(dst))); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".mihomo-*rule-*")); len(left) != 0 {
		t.Errorf("staged files left: %v", left)
	}
	a, _ := os.ReadFile(rule)
	b, _ := os.ReadFile(ruleMarkPath(dst))
	if string(a) != dnsRule("reeza", "Meta") || string(b) != string(a) || NeedsRule(dst) {
		t.Errorf("rule %q, mark %q, needs %v", a, b, NeedsRule(dst))
	}
	// A directory that refuses the rule: the mark is left all the same.
	SetDevice("bowel0")
	st2, err := ruleStep(tmp, dst)
	if err != nil {
		t.Fatal(err)
	}
	st2[3] = filepath.Join(rules, "none", "x.rules")
	if err := run(st2); err != nil {
		t.Fatalf("a refused rule failed the step: %v", err)
	}
	if b, _ := os.ReadFile(ruleMarkPath(dst)); string(b) != dnsRule("reeza", "bowel0")+refusedNote || NeedsRule(dst) {
		t.Errorf("a refused rule: mark %q, needs %v", b, NeedsRule(dst))
	}

	// Where no rule applies the step is the installation's alone.
	ruleLook = func(string) (string, error) { return "", exec.ErrNotFound }
	plain, err := installStep(tmp, dst)
	if err != nil || plain[len(plain)-1] != dst {
		t.Errorf("step = %q, %v", plain, err)
	}
}
