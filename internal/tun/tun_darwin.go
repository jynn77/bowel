package tun

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/oboo/moho/internal/i18n"
)

// Path is where the copy of the core that TUN runs on is kept: in the
// user's data directory, which needs no rights to replace or remove.
func Path(data, name string) string { return filepath.Join(data, "bin", name) }

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
	return runScript(uninstallScript(dst))
}

// stageDir is where the copy waits for the authorisation: beside dst.
func stageDir(dst string) string { return filepath.Dir(dst) }

func checkDir(string) error { return nil }

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

func installStep(tmp, dst string) (Step, error) {
	return Step{"/usr/sbin/chown root:admin ", tmp, " && /bin/chmod 4755 ", tmp, " && /bin/mv -f ", tmp, " ", dst}, nil
}

func installScript(tmp, dst string) string {
	st, _ := installStep(tmp, dst)
	return adminScript(Prompt(), st)
}

func uninstallScript(dst string) string {
	return adminScript(Prompt(), Step{"/bin/rm -f ", dst})
}

func runAdmin(prompt string, steps []Step) error { return runScript(adminScript(prompt, steps...)) }

func runScript(script string) error {
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
