// Package tun installs the core so that it can open a TUN device, which
// takes rights a user's process does not have. Each system gives them its
// own way, after an administrator's authorisation asked once:
//
//   - macOS: a copy of the core in a user directory, made setuid root (as
//     Clash Verge does).
//   - Linux: the same copy, owned by root and given the file capabilities
//     TUN needs (CAP_NET_ADMIN and two more), through pkexec; setuid root
//     where setcap is missing.
//   - Windows: a copy only administrators can write, and a scheduled task
//     that runs it elevated, which the user starts and ends without being
//     asked again.
package tun

import (
	"io"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/oboo/moho/internal/i18n"
)

// ErrCanceled is returned by Install and Uninstall when the user dismissed
// the administrator prompt.
var ErrCanceled error = canceled{}

// canceled says it in the interface's language when it is said.
type canceled struct{}

func (canceled) Error() string {
	return i18n.T("用户取消了授权", "The authorization was canceled")
}

// ErrGone is in what Spawn returns when the mark of an installation is
// there and what it stands for is not: on Windows, the scheduled task was
// deleted, or the task started nothing and the installation is an older
// version's, with no second task to try. Installed cannot tell (it asks the Task Scheduler nothing), so
// whoever sees this takes the installation as not made.
var ErrGone error = gone{}

type gone struct{}

func (gone) Error() string {
	return i18n.T("运行内核的计划任务不在了，重新开启 TUN 会再授权一次", "The scheduled task that runs the core is gone; turning TUN on again asks for the authorization once more")
}

// CoreDir is the directory the core runs in (its -d; the config is
// config.yaml inside). Windows writes both into the task that runs the
// core, and a task with other arguments does not count as installed; the
// other systems do not read it. The task's core gets no environment from
// the app, so every file its config names has to be inside this directory:
// mihomo reads nothing outside it without a SAFE_PATHS variable. Set once, before anything else is called.
var CoreDir string

// device is the TUN device's name in the settings, "" for the core's own
// choice.
var device atomic.Value

// SetDevice tells the package what the settings call the TUN device. Linux
// names it in the polkit rule an installation leaves; the other systems
// do not read it.
func SetDevice(name string) { device.Store(name) }

// Step is one shell command run as root (on Windows, elevated): its parts
// alternate between literal text and paths, which the shell sees as one
// word each.
type Step []string

// Prompt is what the administrator dialog says for TUN alone, where the
// system lets the dialog say anything (macOS).
func Prompt() string {
	return i18n.T("bowel 需要授权以启用 TUN 模式", "bowel needs authorization to turn on TUN mode")
}

// Install copies src to where dst is installed from, then asks for an
// administrator's authorisation to make it the core TUN runs on. It
// returns ErrCanceled when the user canceled, and dst is as it was.
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

// InstallStep copies src beside dst as Install does and returns what is
// left to do as root, for Admin to run together with other steps under
// one authorisation. The copy is the step's first path.
func InstallStep(src, dst string) (Step, error) {
	dir := stageDir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := checkDir(dir); err != nil {
		return nil, err
	}
	// What an authorization that was refused left behind.
	if old, err := filepath.Glob(filepath.Join(dir, ".mihomo-*")); err == nil {
		for _, p := range old {
			_ = os.Remove(p)
		}
	}
	// A copy that becomes dst only once it is root's: dst may be the copy
	// TUN runs on now, and must outlive a refused authorization.
	tmp, err := copyTemp(src, dir)
	if err != nil {
		return nil, err
	}
	step, err := installStep(tmp, dst)
	if err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return step, nil
}

// Admin runs the steps, one after the other and stopping at the first
// that fails, as root under a single administrator authorisation; the
// dialog shows prompt where it can. It returns ErrCanceled when the user
// canceled.
func Admin(prompt string, steps ...Step) error {
	if len(steps) == 0 {
		return nil
	}
	return runAdmin(prompt, steps)
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
