//go:build windows

package ping

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

// createNoWindow is CREATE_NO_WINDOW: the app is built as a GUI program, so
// a console child would otherwise flash a window of its own, one per node
// in a speed test.
const createNoWindow = 0x08000000

var (
	iphlpapi       = syscall.NewLazyDLL("iphlpapi.dll")
	procIcmpCreate = iphlpapi.NewProc("IcmpCreateFile")
	procIcmpClose  = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSend   = iphlpapi.NewProc("IcmpSendEcho")
)

// errNoHelper is IP Helper being missing or unusable. Only then does the
// measurement fall back to ping.exe; a host that answers nothing is a real
// result, not a reason to start a process.
var errNoHelper = errors.New("ping: IP Helper is unavailable")

// icmpEcho sends one echo through IP Helper. Unlike ping.exe it starts no
// process at all: a speed test pings every node, and even with
// CREATE_NO_WINDOW each ping.exe brings a conhost of its own, which on some
// Windows builds still flashes on screen.
func icmpEcho(ctx context.Context, a net.IP, timeout time.Duration) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if d, ok := ctx.Deadline(); ok && time.Until(d) < timeout {
		timeout = time.Until(d)
	}
	wait := max(1, int(timeout/time.Millisecond))

	h, _, _ := procIcmpCreate.Call()
	if h == ^uintptr(0) { // INVALID_HANDLE_VALUE
		return 0, fmt.Errorf("%w: IcmpCreateFile", errNoHelper)
	}
	defer procIcmpClose.Call(h)

	// IPAddr is a ULONG holding the address in network byte order; on a
	// little-endian machine that is the four bytes as they come from To4.
	addr := binary.LittleEndian.Uint32(a.To4())
	payload := []byte("bowel")
	// ICMP_ECHO_REPLY plus the payload and the room the API asks for; only
	// its first twelve bytes — address, status, round trip — are read.
	reply := make([]byte, 256)
	n, _, _ := procIcmpSend.Call(
		h,
		uintptr(addr),
		uintptr(unsafe.Pointer(&payload[0])),
		uintptr(len(payload)),
		0, // no IP options
		uintptr(unsafe.Pointer(&reply[0])),
		uintptr(len(reply)),
		uintptr(wait),
	)
	runtime.KeepAlive(payload)
	runtime.KeepAlive(reply)
	if n == 0 {
		return 0, errors.New("ping: no reply")
	}
	if status := binary.LittleEndian.Uint32(reply[4:8]); status != 0 { // IP_SUCCESS
		return 0, fmt.Errorf("ping: ICMP status %d", status)
	}
	ms := binary.LittleEndian.Uint32(reply[8:12])
	return millis(time.Duration(ms) * time.Millisecond), nil
}

// pingTime matches the round-trip time of the first reply of ping.exe, which
// prints it as "time=12ms" / "time<1ms" in English, and as "时间=12ms" /
// "時間<1ms" in other languages, under a console code page that is often
// not UTF-8. Only the ASCII part is matched.
var pingTime = regexp.MustCompile(`(?i)[=<>]\s*(\d+(?:\.\d+)?)\s*ms`)

// ICMP times one echo to an IPv4 address. iface is a macOS interface name
// (ping -b); on Windows the routing table picks the interface, so it is
// ignored.
func ICMP(ctx context.Context, ip string, iface string, timeout time.Duration) (int, error) {
	a := net.ParseIP(ip)
	if a == nil || a.To4() == nil {
		return 0, errors.New("ping: not an IPv4 address")
	}
	ms, err := icmpEcho(ctx, a, timeout)
	if err == nil || !errors.Is(err, errNoHelper) {
		return ms, err
	}

	// The very old fallback: the system's ping.exe. -w is per-reply, in
	// milliseconds; leave the process room to print its summary and exit.
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
