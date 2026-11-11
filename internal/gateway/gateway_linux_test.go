package gateway

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The rule allows the two commands SetForwarding runs and nothing else,
// and sudo's own checker accepts it. Nothing is installed: visudo only
// reads a file of the test's.
func TestRule(t *testing.T) {
	r := rule(1000, "/usr/sbin/sysctl")
	want := "#1000 ALL = (root) NOPASSWD: /usr/sbin/sysctl -w net.ipv4.ip_forward=0, /usr/sbin/sysctl -w net.ipv4.ip_forward=1\n"
	if !strings.HasSuffix(r, want) {
		t.Fatalf("rule:\n%s", r)
	}
	if strings.Count(r, "NOPASSWD") != 1 || strings.Contains(r, "ALL = (ALL)") {
		t.Fatalf("rule allows more than it should:\n%s", r)
	}
	if got := strings.Join(forwardCmd("/usr/sbin/sysctl", true), " "); got != "/usr/sbin/sysctl -w net.ipv4.ip_forward=1" {
		t.Fatalf("forwardCmd(true) = %s", got)
	}
	f := filepath.Join(t.TempDir(), "rule")
	if err := os.WriteFile(f, []byte(r), 0o440); err != nil {
		t.Fatal(err)
	}
	visudo := rootBin("visudo")
	if _, err := os.Stat(visudo); err != nil {
		t.Skip("no visudo")
	}
	if out, err := exec.Command(visudo, "-cf", f).CombinedOutput(); err != nil {
		t.Fatalf("visudo: %v: %s", err, out)
	}
}

// The rule is checked before it takes its name, and under a name sudo
// skips until then (one with a dot).
func TestPrepareSteps(t *testing.T) {
	steps := prepareSteps("/tmp/x y/rule", "/etc/sudoers.d/bowel-gateway-1000")
	if len(steps) != 3 {
		t.Fatalf("steps: %q", steps)
	}
	stage := steps[0][3]
	if !strings.HasPrefix(filepath.Base(stage), ".") || filepath.Dir(stage) != "/etc/sudoers.d" {
		t.Errorf("staged at %q", stage)
	}
	if !strings.Contains(steps[0][0], "install -o root -g root -m 0440") || steps[0][1] != "/tmp/x y/rule" {
		t.Errorf("first step: %q", steps[0])
	}
	if !strings.Contains(steps[1][0], "visudo -c") || steps[1][1] != stage {
		t.Errorf("not checked where it is staged: %q", steps[1])
	}
	if !strings.Contains(steps[2][0], "mv -f") || steps[2][3] != "/etc/sudoers.d/bowel-gateway-1000" {
		t.Errorf("last step: %q", steps[2])
	}
	for _, st := range steps {
		if len(st)%2 != 0 || !filepath.IsAbs(strings.Fields(st[0])[0]) {
			t.Errorf("step %q doesn't end on a path, or names its command without one", st)
		}
	}
}

func TestReadyNeedsRoot(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root is always ready")
	}
	f := filepath.Join(t.TempDir(), "rule")
	if err := os.WriteFile(f, []byte(rule(1000, "/usr/sbin/sysctl")), 0o440); err != nil {
		t.Fatal(err)
	}
	if ruleInstalled(f) || ruleInstalled(filepath.Join(t.TempDir(), "missing")) {
		t.Error("a file of the user's counts as the installed rule")
	}
}

func TestInhibitArgs(t *testing.T) {
	a := inhibitArgs(4321)
	if !slices.Contains(a, "--what=sleep:idle") || !slices.Contains(a, "--mode=block") || !slices.Contains(a, "--pid=4321") {
		t.Errorf("args %q", a)
	}
	// The lock ends with bowel: its holder waits on bowel's process.
	if i := slices.Index(a, "tail"); i < 0 || a[len(a)-1] != "/dev/null" {
		t.Errorf("no command to hold the lock: %q", a)
	}
}

func TestUfwEnabled(t *testing.T) {
	for conf, want := range map[string]bool{
		"# comment\nENABLED=yes\nLOGLEVEL=low\n": true, "ENABLED=no\n": false, `ENABLED="YES"` + "\n": true, "": false, "#ENABLED=yes\n": false,
	} {
		if ufwEnabled(conf) != want {
			t.Errorf("%q: %v", conf, !want)
		}
	}
}

// On this machine: the kernel's switch is read, changing nothing; the
// LAN address is the default route's interface's; and the lock against
// sleep is taken and given back where systemd is.
func TestLive(t *testing.T) {
	sys := OS()
	fwd, err := sys.Forwarding()
	if err != nil {
		t.Skip("no /proc here:", err)
	}
	n := sys.Network()
	t.Logf("forwarding %v, network %+v, primary %q, ready %v", fwd, n, PrimaryInterface(), sys.Ready())
	if n.Iface != "" && n.IP == "" {
		t.Errorf("an interface with no address: %+v", n)
	}
	if p := PrimaryInterface(); p != "" && n.Iface != "" && n.IP != "" && p != n.Iface {
		t.Logf("the LAN address is %s's, the default route %s's", n.Iface, p)
	}
	stop, err := sys.KeepAwake()
	if err != nil {
		t.Fatalf("KeepAwake: %v", err)
	}
	stop()
}
