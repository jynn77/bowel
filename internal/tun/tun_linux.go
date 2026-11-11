package tun

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/oboo/moho/internal/i18n"
)

// On Linux the copy is root's and carries file capabilities, so the core
// runs as the user with only what TUN takes:
//
//   - CAP_NET_ADMIN: the TUN device, its routes and rules;
//   - CAP_NET_BIND_SERVICE: port 53, for gateway mode's resolver;
//   - CAP_NET_RAW: binding sockets to the physical interface, past TUN's
//     routes, on kernels older than 5.7.
//
// Where setcap is not installed, the copy is made setuid root instead, as
// on macOS. Either way it is one pkexec, which also leaves the polkit
// rule of dnsrule_linux.go where systemd-resolved is in use.
const fileCaps = "cap_net_admin,cap_net_bind_service,cap_net_raw=ep"

// Path is where the copy of the core that TUN runs on is kept: in the
// user's data directory, which needs no rights to replace or remove.
func Path(data, name string) string { return filepath.Join(data, "bin", name) }

// Installed reports whether path is a regular file owned by root that
// either has CAP_NET_ADMIN among its effective file capabilities or the
// setuid bit.
func Installed(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || st.Uid != 0 {
		return false
	}
	if fi.Mode()&fs.ModeSetuid != 0 {
		return true
	}
	buf := make([]byte, 64)
	n, err := unix.Lgetxattr(path, "security.capability", buf)
	if err != nil {
		return false
	}
	return capsAllow(buf[:n], unix.CAP_NET_ADMIN)
}

// capsAllow reads a security.capability attribute (struct vfs_cap_data,
// little-endian: a word of version and flags, then pairs of permitted and
// inheritable words) and says whether it makes capability c effective.
func capsAllow(b []byte, c int) bool {
	const (
		effective = 0x000001   // VFS_CAP_FLAGS_EFFECTIVE
		revMask   = 0xff000000 // VFS_CAP_REVISION_MASK
		rev1      = 0x01000000
		rev2      = 0x02000000
		rev3      = 0x03000000
	)
	if len(b) < 12 {
		return false
	}
	magic := binary.LittleEndian.Uint32(b)
	words := 0
	switch magic & revMask {
	case rev1:
		words = 1
	case rev2, rev3:
		words = 2
	}
	if magic&effective == 0 || c/32 >= words || len(b) < 4+8*words {
		return false
	}
	permitted := binary.LittleEndian.Uint32(b[4+8*(c/32):])
	return permitted&(1<<(c%32)) != 0
}

// Uninstall removes dst, which the user can do in a directory of theirs,
// and what the installation left besides: the polkit rule of
// dnsrule_linux.go and its mark. The rule is root's, so where there is
// one this asks for an authorization (one pkexec); the mark stays until
// the rule is gone. A missing dst is not an error.
func Uninstall(dst string) error {
	mark := ruleMarkPath(dst)
	var steps []Step
	err := os.Remove(dst)
	switch {
	case err == nil || errors.Is(err, fs.ErrNotExist):
	case errors.Is(err, fs.ErrPermission):
		steps = append(steps, Step{tool("rm") + " -f ", dst, " ", mark})
	default:
		return err
	}
	if ruleLeft(mark) {
		steps = append(steps, Step{tool("rm") + " -f ", rulePath()})
	}
	if len(steps) > 0 {
		if err := adminRun(Prompt(), steps); err != nil {
			return err
		}
	}
	if err := os.Remove(mark); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// adminRun is runAdmin, which tests replace: they have no polkit.
var adminRun = runAdmin

// stageDir is where the copy waits for the authorisation: beside dst, so
// that taking its name is a rename, which keeps the capabilities.
func stageDir(dst string) string { return filepath.Dir(dst) }

// checkDir refuses a directory whose file system is mounted nosuid: the
// kernel ignores both file capabilities and the setuid bit there, and the
// core would start without its rights.
func checkDir(dir string) error {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return nil // not known: the installation will tell
	}
	if st.Flags&unix.ST_NOSUID != 0 {
		return fmt.Errorf(i18n.T("%s 所在的文件系统以 nosuid 挂载，内核在这里拿不到 TUN 需要的权限",
			"The file system of %s is mounted nosuid, so the core can't get the rights TUN needs there"), dir)
	}
	return nil
}

// toolDirs are where the commands run as root are looked for, in the
// order pkexec's own PATH has them.
var toolDirs = []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin"}

// tool is a command's full path, or its bare name when it is in none of
// toolDirs (pkexec's PATH is those).
func tool(name string) string {
	for _, d := range toolDirs {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return name
}

func installStep(tmp, dst string) (Step, error) {
	st := installStepWith(tool("setcap"), tmp, dst)
	rule, err := ruleStep(tmp, dst)
	if err != nil {
		return nil, err
	}
	return append(st, rule...), nil
}

// installStepWith gives the copy to root first: chown drops capabilities
// and the setuid bit, so the rights come after it.
func installStepWith(setcap, tmp, dst string) Step {
	st := Step{tool("chown") + " root:root ", tmp}
	if filepath.IsAbs(setcap) {
		st = append(st, " && "+tool("chmod")+" 0755 ", tmp, " && "+setcap+" "+fileCaps+" ", tmp)
	} else {
		st = append(st, " && "+tool("chmod")+" 4755 ", tmp)
	}
	return append(st, " && "+tool("mv")+" -f ", tmp, " ", dst)
}

// shellQuote makes s one word of a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// script is the steps as one sh command line, stopping at the first that
// fails.
func script(steps []Step) string {
	cmds := make([]string, len(steps))
	for i, st := range steps {
		var b strings.Builder
		for j, p := range st {
			if j%2 == 0 {
				b.WriteString(p)
			} else {
				b.WriteString(shellQuote(p))
			}
		}
		cmds[i] = b.String()
	}
	return strings.Join(cmds, " && ")
}

// runAdmin runs the steps through pkexec, whose dialog is the desktop's
// polkit agent's: it names the command, and cannot show prompt.
func runAdmin(_ string, steps []Step) error {
	sh := script(steps)
	var cmd *exec.Cmd
	if os.Geteuid() == 0 {
		cmd = exec.Command("/bin/sh", "-c", sh)
	} else {
		pkexec, err := exec.LookPath("pkexec")
		if err != nil {
			return errors.New(i18n.T("找不到 pkexec：请先安装 polkit，授权要通过它来完成",
				"pkexec was not found: install polkit, which the authorization goes through"))
		}
		cmd = exec.Command(pkexec, "/bin/sh", "-c", sh)
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	return adminError(exitCode(err), strings.TrimSpace(string(out)), err)
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// adminError reads pkexec's exit status: 126 when the dialog was
// dismissed, 127 when no authorisation could be had at all (no agent
// running, or not allowed).
func adminError(code int, msg string, err error) error {
	if code == 126 {
		return ErrCanceled
	}
	if msg == "" {
		msg = err.Error()
	}
	if code == 127 {
		return fmt.Errorf(i18n.T("无法取得授权（桌面可能没有运行 polkit 认证代理）：%s",
			"Couldn't get an authorization (the desktop may have no polkit agent running): %s"), msg)
	}
	return fmt.Errorf(i18n.T("授权失败：%s", "Authorization failed: %s"), msg)
}
