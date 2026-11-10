//go:build windows

// The login item, on Windows. Moho runs elevated: the installer marks the
// executable RUNASADMIN, for the tunnel needs the administrator's token. The
// framework's login item goes through the Run key, whose entries start
// unelevated — the tunnel would be dead every morning. A scheduled task of
// the highest run level starts the process with that token, and without a
// prompt: this is what the framework's own comments tell such apps to do.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/oboo/moho/internal/i18n"
)

// loginTask is the scheduled task's name. One name: the task is this app's,
// and it is remade on every switch.
const loginTask = "bowel"

// createNoWindow is CREATE_NO_WINDOW. schtasks is a console program, and
// this app is a GUI one: without it, every switch of the login item would
// flash a console window.
const createNoWindow = 0x08000000

// schtasks is the command that creates or deletes the task, run with no
// console of its own.
func schtasks(args ...string) *exec.Cmd {
	c := exec.Command("schtasks", args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return c
}

// buildTaskAction is the task's action: the executable, quoted — its path
// may have spaces in it — with the argument the framework reads to keep the
// window hidden at login.
func buildTaskAction(exe string) string {
	return fmt.Sprintf(`"%s" --mygo-opened-at-login`, exe)
}

// schtasksArgs is what schtasks runs for a switch: creating the task of the
// highest run level at logon, or deleting it.
func schtasksArgs(on bool, exe string) []string {
	if on {
		return []string{"/Create", "/TN", loginTask, "/SC", "ONLOGON", "/RL", "HIGHEST", "/F", "/TR", buildTaskAction(exe)}
	}
	return []string{"/Delete", "/TN", loginTask, "/F"}
}

// setOpenAtLoginOS makes the task, or unmakes it. Made by a process of the
// administrator's token, the highest run level asks nothing of the user;
// deleting a task that is not there is the state the user asked for.
func setOpenAtLoginOS(on bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	out, err := schtasks(schtasksArgs(on, exe)...).CombinedOutput()
	if err != nil {
		if !on && strings.Contains(strings.ToLower(string(out)), "does not exist") {
			return nil
		}
		return fmt.Errorf(i18n.T("开机启动设置失败: %s", "Setting the login item failed: %s"), strings.TrimSpace(string(out)))
	}
	return nil
}

// openAtLoginOS says whether the task is there.
func openAtLoginOS() bool {
	return schtasks("/Query", "/TN", loginTask).Run() == nil
}
