//go:build windows

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const createNoWindow = 0x08000000

func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

func interrupt(cmd *exec.Cmd) { _ = cmd.Process.Kill() }

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
)

// processImage is the executable path of a process, or "" when it is gone
// or not ours to ask about.
func processImage(pid int) string {
	const queryLimitedInformation = 0x1000
	h, _, _ := procOpenProcess.Call(queryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, syscall.MAX_LONG_PATH)
	n := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// killStale kills the core a previous run left behind, whose pid is in the
// file at path. With TUN the leftover would keep the adapter and the routes
// that point traffic into it, which a dead core turns into no network at
// all; the pid is checked against the image name first, so a number that
// another process inherited kills nothing.
func killStale(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return
	}
	if img := processImage(pid); !strings.EqualFold(filepath.Base(img), "mihomo.exe") {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
	_ = os.Remove(path)
}
