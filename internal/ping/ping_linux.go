package ping

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strconv"
	"time"
)

// pingBins are where ping is: an app started from a desktop may have a
// short PATH.
var pingBins = []string{"ping", "/usr/bin/ping", "/bin/ping", "/usr/sbin/ping", "/sbin/ping"}

// ICMP times one echo to an IPv4 address with the system's ping (iputils'
// or BusyBox's), which needs no privileges of bowel's. iface, if set, is
// the interface to leave through; a TUN would otherwise answer in the
// server's place.
func ICMP(ctx context.Context, ip string, iface string, timeout time.Duration) (int, error) {
	if a := net.ParseIP(ip); a == nil || a.To4() == nil {
		return 0, errors.New("ping: not an IPv4 address")
	}
	var bin string
	for _, b := range pingBins {
		if p, err := exec.LookPath(b); err == nil {
			bin = p
			break
		}
	}
	if bin == "" {
		return 0, errors.New("ping: no ping command")
	}
	out, err := exec.CommandContext(ctx, bin, pingArgs(ip, iface, timeout)...).Output()
	if err != nil {
		return 0, err
	}
	return parseTime(out)
}

func pingArgs(ip, iface string, timeout time.Duration) []string {
	args := []string{"-n", "-c", "1", "-W", strconv.Itoa(max(1, int(timeout/time.Second)))}
	if iface != "" {
		args = append(args, "-I", iface)
	}
	return append(args, ip)
}
