package tun

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/oboo/moho/internal/i18n"
)

// On Windows a TUN adapter (Wintun's, which the core carries) can only be
// made by an elevated process, and nothing like a setuid bit lets a
// program be elevated without asking. So the one authorisation (a UAC
// prompt) does two things, in one elevated cmd:
//
//   - it copies the core to a directory under %ProgramData% that only
//     administrators and SYSTEM can write, where no program of the user's
//     can replace it afterwards;
//   - it registers a scheduled task of the user's that runs that copy,
//     with the arguments the app runs its core with, at the highest run
//     level. The user may start and end their own task without being
//     asked again, which is what Spawn does. A second task runs the same
//     in the session the user is logged on in, for the accounts the
//     first cannot be started for, through a copy of the app's own
//     program kept beside the core (see fallbackTaskXML).
//
// A mark beside the copy, written by the same elevated command, says what
// the task was registered for: Installed reads it, with no call to the
// Task Scheduler.

// Path is where the copy of the core that TUN runs on is kept: a
// directory of this user's under %ProgramData%, closed to all but
// administrators by the installation.
func Path(data, name string) string {
	root := os.Getenv("ProgramData")
	if root == "" {
		root = `C:\ProgramData`
	}
	return filepath.Join(root, "bowel", "tun", userSID(), name)
}

// userSID is the user's security identifier, as S-1-5-21-…: what the
// task runs as, and a name no other user's installation has.
func userSID() string {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "user"
	}
	return u.User.Sid.String()
}

// taskName is the scheduled task's, one per user.
func taskName() string { return taskNameOf(userSID()) }

// Installed reports whether path is the copy an installation made for
// the directory the core runs in now. Only an elevated process can have
// written the mark that says so.
func Installed(path string) bool {
	if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	b, err := os.ReadFile(markPath(path))
	return err == nil && CoreDir != "" && strings.TrimSpace(string(b)) == mark(path, CoreDir, userSID())
}

// stageDir is where the copy and the task's definition wait for the
// authorisation: the user cannot write beside dst.
func stageDir(string) string { return filepath.Join(os.TempDir(), "bowel-tun") }

func checkDir(string) error { return nil }

func installStep(tmp, dst string) (Step, error) {
	if CoreDir == "" {
		return nil, errors.New("tun: CoreDir is not set")
	}
	sid := userSID()
	// The app's own program, to be the second task's launcher.
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	ltmp, err := copyTemp(exe, filepath.Dir(tmp))
	if err != nil {
		return nil, err
	}
	xml := filepath.Join(filepath.Dir(tmp), ".mihomo-task.xml")
	xml2 := filepath.Join(filepath.Dir(tmp), ".mihomo-task-interactive.xml")
	for path, def := range map[string]string{
		xml:  taskXML(sid, dst, CoreDir),
		xml2: fallbackTaskXML(sid, launcherPath(dst), dst, CoreDir),
	} {
		if err := os.WriteFile(path, utf16File(def), 0o600); err != nil {
			_ = os.Remove(ltmp)
			return nil, err
		}
	}
	return winInstallStep(tmp, ltmp, dst, xml, taskName(), xml2, fallbackName(taskName()), mark(dst, CoreDir, sid)), nil
}

// Uninstall deletes the tasks and the copy's directory, which takes an
// administrator. A missing dst is not an error.
func Uninstall(dst string) error {
	if _, err := os.Lstat(dst); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return runAdmin(Prompt(), []Step{{"(schtasks /Delete /F /TN ", taskName(), " >nul 2>nul & schtasks /Delete /F /TN ", fallbackName(taskName()),
		" >nul 2>nul & ver >nul) && rmdir /s /q ", filepath.Dir(dst)}})
}

// shellExecuteInfo is SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	size       uint32
	mask       uint32
	hwnd       windows.Handle
	verb       *uint16
	file       *uint16
	parameters *uint16
	directory  *uint16
	show       int32
	instApp    windows.Handle
	idList     uintptr
	class      *uint16
	keyClass   windows.Handle
	hotKey     uint32
	icon       windows.Handle
	process    windows.Handle
}

const (
	seeNoCloseProcess = 0x00000040 // SEE_MASK_NOCLOSEPROCESS
	seeNoUI           = 0x00000400 // SEE_MASK_FLAG_NO_UI: no error boxes of its own
)

var procShellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// runAdmin runs the steps in an elevated cmd.exe, hidden, and waits for
// it. Windows' own prompt names the program, and cannot show prompt.
func runAdmin(_ string, steps []Step) error {
	sh, err := winScript(steps)
	if err != nil {
		return err
	}
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(filepath.Join(sys, "cmd.exe"))
	if err != nil {
		return err
	}
	// /s: what is between the outer quotes is the command, as written.
	params, err := windows.UTF16PtrFromString(`/d /s /c "` + sh + `"`)
	if err != nil {
		return err
	}
	info := shellExecuteInfo{mask: seeNoCloseProcess | seeNoUI, verb: verb, file: file, parameters: params, show: windows.SW_HIDE}
	info.size = uint32(unsafe.Sizeof(info))
	if r, _, err := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return ErrCanceled
		}
		return fmt.Errorf(i18n.T("授权失败：%s", "Authorization failed: %s"), err)
	}
	if info.process == 0 {
		return errors.New(i18n.T("授权失败：没有启动提权的进程", "Authorization failed: no elevated process was started"))
	}
	defer windows.CloseHandle(info.process)
	if _, err := windows.WaitForSingleObject(info.process, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.process, &code); err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf(i18n.T("授权后的安装没有完成（退出码 %d）", "The installation didn't finish after the authorization (exit code %d)"), code)
	}
	return nil
}
