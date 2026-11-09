//go:build windows

package ping

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"time"
)

// createNoWindow is CREATE_NO_WINDOW: the app is built as a GUI program, so
// a console child would otherwise flash a window of its own, one per node
// in a speed test.
const createNoWindow = 0x08000000

// pingTime matches the round-trip time of the first reply. Windows prints
// it as "time=12ms" / "time<1ms" in English, and as "时间=12ms" / "時間<1ms"
// in other languages, under a console code page that is often not UTF-8.
// Only the ASCII part is matched, so the encoding of the words around it
// doesn't matter. The summary lines (Minimum / 最短 …) also carry "ms", but
// they come after the reply, and FindSubmatch takes the first match.
var pingTime = regexp.MustCompile(`(?i)[=<>]\s*(\d+(?:\.\d+)?)\s*ms`)

// ICMP times one echo to an IPv4 address with the system's ping.exe, which
// needs no privileges. iface is a macOS interface name (ping -b); on Windows
// the routing table picks the interface, so it is ignored.
func ICMP(ctx context.Context, ip string, iface string, timeout time.Duration) (int, error) {
	if a := net.ParseIP(ip); a == nil || a.To4() == nil {
		return 0, errors.New("ping: not an IPv4 address")
	}
	// -w is per-reply, in milliseconds. Leave the process a little room to
	// print its summary and exit before the caller's context expires.
	wait := max(1, int((timeout-500*time.Millisecond)/time.Millisecond))
	args := []string{"-n", "1", "-w", strconv.Itoa(wait), "-4", ip}
	cmd := exec.CommandContext(ctx, pingExe(), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return parseTime(out)
}

// pingExe finds ping.exe even when System32 is missing from PATH.
func pingExe() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		p := filepath.Join(root, "System32", "ping.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "ping"
}

func parseTime(out []byte) (int, error) {
	// A real reply carries a TTL; "Request timed out" and the summary of a
	// lost ping don't. Checking this keeps a failed run from being read as
	// a 0 ms answer.
	if !bytes.Contains(bytes.ToLower(out), []byte("ttl")) {
		return 0, errors.New("ping: no reply")
	}
	m := pingTime.FindSubmatch(out)
	if m == nil {
		return 0, errors.New("ping: no reply")
	}
	v, err := strconv.ParseFloat(string(m[1]), 64)
	if err != nil {
		return 0, err
	}
	return millis(time.Duration(v * float64(time.Millisecond))), nil
}
