//go:build windows

package ping

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/oboo/moho/internal/nowindow"
)

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

// ICMP times one echo to an IPv4 address. iface, if set, is the adapter to
// leave through, given to ping as that adapter's own address; a TUN would
// otherwise answer in the server's place. With no adapter to bind, the echo
// goes through IP Helper, which unlike ping.exe starts no process at all: a
// speed test pings every node, and even with CREATE_NO_WINDOW each ping.exe
// brings a conhost of its own, which on some Windows builds still flashes on
// screen.
func ICMP(ctx context.Context, ip string, iface string, timeout time.Duration) (int, error) {
	a := net.ParseIP(ip)
	if a == nil || a.To4() == nil {
		return 0, errors.New("ping: not an IPv4 address")
	}
	if iface == "" {
		ms, err := icmpEcho(ctx, a, timeout)
		if err == nil || !errors.Is(err, errNoHelper) {
			return ms, err
		}
		return icmpPing(ctx, ip, "", timeout)
	}
	src := ifaceIPv4(iface)
	if src == "" {
		return 0, errors.New("ping: the adapter has no IPv4 address")
	}
	return icmpPing(ctx, ip, src, timeout)
}

// icmpEcho sends one echo through IP Helper.
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

// icmpPing times one echo with the system's ping.exe, which needs no
// privileges. src, if set, is the adapter address ping leaves through (-S);
// -w is per-reply, in milliseconds.
func icmpPing(ctx context.Context, ip, src string, timeout time.Duration) (int, error) {
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		return 0, err
	}
	wait := max(1, int((timeout-500*time.Millisecond)/time.Millisecond))
	args := pingArgs(ip, src, time.Duration(wait)*time.Millisecond)
	cmd := nowindow.CommandContext(ctx, filepath.Join(sys, "PING.EXE"), args...)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return parseTime(out)
}

func pingArgs(ip, src string, timeout time.Duration) []string {
	args := []string{"-n", "1", "-w", strconv.Itoa(max(1, int(timeout/time.Millisecond))), "-4"}
	if src != "" {
		args = append(args, "-S", src)
	}
	return append(args, ip)
}

// ifaceIPv4 is the adapter's first IPv4 address.
func ifaceIPv4(name string) string {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	addrs, _ := ifc.Addrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			return n.IP.String()
		}
	}
	return ""
}
