// Package tun installs the core so that it can open a TUN device: mihomo
// needs root for that, so Moho keeps a copy of the core in a user directory
// made setuid root after an administrator's authorisation (as Clash Verge
// does).
package tun

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/oboo/moho/internal/i18n"
)

// Reset has nothing to do here: this Mac's utun device, its address and the
// routes sing-tun added all go when the core closes TUN, and the core is
// always asked to close it (SIGTERM, which it answers).
func Reset() error { return nil }

// ErrCanceled is returned by Install and Uninstall when the user dismissed
// the administrator prompt.
var ErrCanceled error = canceled{}

// canceled says it in the interface's language when it is said.
type canceled struct{}

func (canceled) Error() string {
	return i18n.T("用户取消了授权", "The authorization was canceled")
}

// prompt is shown in the administrator authorisation dialog.
const prompt = "bowel 需要授权以启用 TUN 模式"

// Installed reports whether path is a regular file owned by root with the
// setuid bit.
func Installed(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode()&fs.ModeSetuid == 0 {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == 0
}

// Install copies src to dst (creating dst's directory), then asks for an
// administrator's authorisation through osascript to chown root:admin and
// chmod 4755 it. It returns ErrCanceled when the user canceled.
func Install(src, dst string) error {
	step, err := InstallStep(src, dst)
	if err != nil {
		return err
	}
	if err := Admin(Prompt(), step); err != nil {
		_ = os.Remove(step[1]) // the copy; dst is as it was
		return err
	}
	return nil
}

// InstallStep copies src to dst as Install does and returns what is left
// to do as root, for Admin to run together with other steps under one
// authorisation.
func InstallStep(src, dst string) (Step, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, err
	}
	// What an authorization that was refused left behind.
	if old, err := filepath.Glob(filepath.Join(filepath.Dir(dst), ".mihomo-*")); err == nil {
		for _, p := range old {
			_ = os.Remove(p)
		}
	}
	// A copy beside dst, which becomes dst only once it is root's: dst may
	// be the copy TUN runs on now, and must outlive a refused authorization.
	tmp, err := copyTemp(src, filepath.Dir(dst))
	if err != nil {
		return nil, err
	}
	return installStep(tmp, dst), nil
}

// Step is one shell command run as root: its parts alternate between
// literal text and paths, which the shell sees as one word each.
type Step []string

// Prompt is what the administrator dialog says for TUN alone.
func Prompt() string { return i18n.T(prompt, "bowel needs authorization to turn on TUN mode") }

// Admin runs the steps, one after the other and stopping at the first
// that fails, as root under a single administrator authorisation; the
// dialog shows prompt. It returns ErrCanceled when the user canceled.
func Admin(prompt string, steps ...Step) error {
	if len(steps) == 0 {
		return nil
	}
	return runAdmin(adminScript(prompt, steps...))
}

// Uninstall removes dst. A root-owned file in a directory of the user's can
// be removed without privileges; otherwise it asks for an administrator's
// authorisation. A missing dst is not an error.
func Uninstall(dst string) error {
	err := os.Remove(dst)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	return runAdmin(uninstallScript(dst))
}

func copyTemp(src, dir string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(dir, ".mihomo-*")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(out.Name(), 0o755)
	}
	if err != nil {
		_ = os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}

// appleString quotes s as an AppleScript string literal.
func appleString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// shellExpr builds an AppleScript expression for a shell command: literal
// parts are AppleScript strings, paths go through `quoted form of` so the
// shell sees each as one word.
func shellExpr(parts ...string) string {
	var b []string
	for i, p := range parts {
		if i%2 == 0 {
			b = append(b, appleString(p))
		} else {
			b = append(b, "quoted form of "+appleString(p))
		}
	}
	return strings.Join(b, " & ")
}

func adminScript(prompt string, steps ...Step) string {
	exprs := make([]string, len(steps))
	for i, st := range steps {
		exprs[i] = shellExpr(st...)
	}
	return "do shell script " + strings.Join(exprs, ` & " && " & `) + " with prompt " + appleString(prompt) + " with administrator privileges"
}

func installStep(tmp, dst string) Step {
	return Step{"/usr/sbin/chown root:admin ", tmp, " && /bin/chmod 4755 ", tmp, " && /bin/mv -f ", tmp, " ", dst}
}

func installScript(tmp, dst string) string { return adminScript(Prompt(), installStep(tmp, dst)) }

func uninstallScript(dst string) string {
	return adminScript(Prompt(), Step{"/bin/rm -f ", dst})
}

func runAdmin(script string) error {
	out, err := exec.Command("/usr/bin/osascript", "-e", script).CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if strings.Contains(msg, "-128") || strings.Contains(msg, "User canceled") {
		return ErrCanceled
	}
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf(i18n.T("授权失败：%s", "Authorization failed: %s"), msg)
}
