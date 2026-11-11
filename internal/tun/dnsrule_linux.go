package tun

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// With TUN on, the core points systemd-resolved at its own resolver for
// the TUN device: `resolvectl domain`, `default-route` and `dns` when the
// device comes up, `revert` when it goes. Each is a polkit action of
// resolved's that asks for an administrator, and the core, which runs as
// the user with file capabilities, is an ordinary user to polkit: three
// dialogs every time TUN starts (issue 71). So the one authorization also
// leaves a polkit rule that allows this user those four actions.
//
// The rule is a file of its own, as the gateway's sudoers rule is:
// deleting it undoes it. It names the user, and the device where polkit
// is told which link is meant (systemd 256 and later); with an older
// systemd it allows the user the four actions on any link. That is less
// than the copy itself gives: whoever can run it can already send every
// packet of the machine, port 53 included, where they like.

// ruleActions are what resolvectl asks polkit for on the core's behalf.
var ruleActions = []string{
	"org.freedesktop.resolve1.set-dns-servers",
	"org.freedesktop.resolve1.set-domains",
	"org.freedesktop.resolve1.set-default-route",
	"org.freedesktop.resolve1.revert",
}

// defaultDevice is what the core calls the TUN device on Linux when the
// settings name none.
const defaultDevice = "Meta"

// What the rule depends on of the machine. Tests replace them.
var (
	// rulesDir is where polkit 0.106 and later reads an administrator's
	// rules. Older ones (.pkla files) have no such directory, and get no
	// rule: there the dialogs stay.
	rulesDir = "/etc/polkit-1/rules.d"
	ruleUID  = os.Getuid
	ruleUser = func() string {
		if u, err := user.Current(); err == nil {
			return u.Username
		}
		return ""
	}
	ruleLook = exec.LookPath
)

func ruleDevice() string {
	if d, _ := device.Load().(string); d != "" {
		return d
	}
	return defaultDevice
}

// dnsRule is the rule for user and the TUN device dev.
func dnsRule(user, dev string) string {
	ids, _ := json.Marshal(ruleActions)
	u, _ := json.Marshal(user)
	d, _ := json.Marshal(dev)
	return fmt.Sprintf(`// Written by bowel for TUN mode: lets this user point systemd-resolved at
// the TUN device's resolver without a password each time TUN starts.
// Delete the file to undo it.
polkit.addRule(function(action, subject) {
    if (%s.indexOf(action.id) < 0 || subject.user !== %s) {
        return polkit.Result.NOT_HANDLED;
    }
    // systemd 256 and later say which link is to be changed.
    var link = action.lookup("interface");
    if (link && link !== %s) {
        return polkit.Result.NOT_HANDLED;
    }
    return polkit.Result.YES;
});
`, ids, u, d)
}

func rulePath() string {
	return filepath.Join(rulesDir, fmt.Sprintf("49-bowel-tun-%d.rules", ruleUID()))
}

// ruleMarkPath is the rule as it was installed, kept beside the copy:
// polkit's directory is closed to users on some systems, and there the
// mark is all that says a rule was written.
func ruleMarkPath(dst string) string { return filepath.Join(filepath.Dir(dst), "tun-dns-rule") }

// refusedNote ends the mark where polkit's directory did not take the
// rule: such a machine is asked once, not every time TUN is turned on.
const refusedNote = "// Not installed: polkit's directory refused it.\n"

// ruleApplies says whether this machine is one the rule is for: the core
// runs as a user (not root, and not setuid for want of setcap), resolved's
// command is there for the core to call, and polkit reads rules.
func ruleApplies() bool {
	if ruleUID() == 0 || ruleUser() == "" || !filepath.IsAbs(tool("setcap")) {
		return false
	}
	if _, err := ruleLook("resolvectl"); err != nil {
		return false
	}
	st, err := os.Stat(rulesDir)
	return err == nil && st.IsDir()
}

// NeedsRule reports whether the installation at dst lacks the rule, or
// has one for another device or user: turning TUN on then takes the
// authorization once more. The mark says what was written; the rule
// itself is read too where the user may read it, so that one deleted or
// changed by hand is written again. Where polkit's directory is closed
// to users the mark is taken at its word.
func NeedsRule(dst string) bool {
	if !ruleApplies() {
		return false
	}
	want := dnsRule(ruleUser(), ruleDevice())
	b, err := os.ReadFile(ruleMarkPath(dst))
	if err != nil {
		return true
	}
	switch string(b) {
	case want + refusedNote:
		return false
	case want:
	default:
		return true
	}
	got, err := os.ReadFile(rulePath())
	if err != nil {
		return errors.Is(err, fs.ErrNotExist)
	}
	return string(got) != want
}

// ruleLeft reports whether an installation with this mark has a rule for
// Uninstall to remove. Where polkit's directory cannot be looked into,
// the mark answers.
func ruleLeft(mark string) bool {
	_, err := os.Lstat(rulePath())
	if err == nil {
		return true
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	b, err := os.ReadFile(mark)
	return err == nil && !strings.HasSuffix(string(b), refusedNote)
}

// ruleStep writes the rule beside tmp and returns the commands that make
// it polkit's, to follow the installation's own; nil where no rule applies.
func ruleStep(tmp, dst string) (Step, error) {
	if !ruleApplies() {
		return nil, nil
	}
	rule := dnsRule(ruleUser(), ruleDevice())
	dir := filepath.Dir(tmp)
	staged, err := stage(dir, ".mihomo-rule-*", rule)
	if err != nil {
		return nil, err
	}
	refused, err := stage(dir, ".mihomo-norule-*", rule+refusedNote)
	if err != nil {
		_ = os.Remove(staged)
		return nil, err
	}
	return ruleStepWith(staged, refused, rulePath(), ruleMarkPath(dst)), nil
}

func stage(dir, pattern, text string) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(text)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// ruleStepWith installs the rule and keeps what was written as the mark;
// where polkit's directory refuses the rule the mark is refused, which
// says so. Neither fails the installation.
func ruleStepWith(tmp, refused, rule, mark string) Step {
	mv := tool("mv") + " -f "
	return Step{" && { " + tool("install") + " -m 0644 -o root -g root ", tmp, " ", rule,
		" && " + mv, tmp, " ", mark, " || " + mv, refused, " ", mark,
		"; " + tool("rm") + " -f ", tmp, " ", refused, "; }"}
}
