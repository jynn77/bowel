package tun

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalled(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(f, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if Installed(f) {
		t.Error("a file of the user's counts as installed")
	}
	// The setuid bit alone is not enough: the owner must be root.
	if err := os.Chmod(f, 0o4755); err != nil {
		t.Fatal(err)
	}
	if Installed(f) {
		t.Error("a setuid file of the user's counts as installed")
	}
	if Installed(filepath.Join(dir, "missing")) || Installed(dir) {
		t.Error("a missing file or a directory counts as installed")
	}
}

func TestScripts(t *testing.T) {
	dst := `/Users/a b/it's "x"\y/mihomo`
	got := installScript("/Users/a b/.tmp", dst)
	q := `quoted form of "/Users/a b/it's \"x\"\\y/mihomo"`
	tq := `quoted form of "/Users/a b/.tmp"`
	want := `do shell script "/usr/sbin/chown root:admin " & ` + tq + ` & " && /bin/chmod 4755 " & ` + tq +
		` & " && /bin/mv -f " & ` + tq + ` & " " & ` + q + ` with prompt "bowel 需要授权以启用 TUN 模式" with administrator privileges`
	if got != want {
		t.Fatalf("installScript:\n got %s\nwant %s", got, want)
	}
	un := uninstallScript(dst)
	if !strings.HasPrefix(un, `do shell script "/bin/rm -f " & `+q+" with prompt") {
		t.Fatalf("uninstallScript: %s", un)
	}
	// The scripts compile (osacompile only compiles; nothing runs).
	if _, err := exec.LookPath("osacompile"); err != nil {
		t.Skip("no osacompile")
	}
	for _, s := range []string{got, un} {
		out := filepath.Join(t.TempDir(), "s.scpt")
		if b, err := exec.Command("osacompile", "-o", out, "-e", s).CombinedOutput(); err != nil {
			t.Errorf("osacompile: %v: %s\n%s", err, b, s)
		}
	}
}

// Several steps run under one authorisation, joined by &&, each path
// quoted on its own; the script compiles (nothing runs).
func TestAdminScriptSteps(t *testing.T) {
	st, _ := installStep("/a b/.t", "/a b/mihomo")
	s := adminScript("p", st, Step{"/bin/echo ", "/c'd"})
	want := `do shell script "/usr/sbin/chown root:admin " & quoted form of "/a b/.t" & " && /bin/chmod 4755 " & quoted form of "/a b/.t" & " && /bin/mv -f " & quoted form of "/a b/.t" & " " & quoted form of "/a b/mihomo" & " && " & "/bin/echo " & quoted form of "/c'd" with prompt "p" with administrator privileges`
	if s != want {
		t.Fatalf("adminScript:\n got %s\nwant %s", s, want)
	}
	if err := Admin("p"); err != nil {
		t.Fatalf("Admin with no steps: %v", err)
	}
	if _, err := exec.LookPath("osacompile"); err != nil {
		t.Skip("no osacompile")
	}
	out := filepath.Join(t.TempDir(), "s.scpt")
	if b, err := exec.Command("osacompile", "-o", out, "-e", s).CombinedOutput(); err != nil {
		t.Errorf("osacompile: %v: %s", err, b)
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
