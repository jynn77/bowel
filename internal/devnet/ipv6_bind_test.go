//go:build linux || windows

package devnet

import (
	"context"
	"net"
	"testing"
	"time"
)

// The socket option is taken, and a bound connection really connects: the
// loopback interface is the one that has something to connect to here.
func TestDialIPv6Bound(t *testing.T) {
	l, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("no IPv6 loopback:", err)
	}
	defer l.Close()
	lo := ""
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagLoopback != 0 {
			lo = ifc.Name
			break
		}
	}
	if lo == "" {
		t.Skip("no loopback interface")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !dialIPv6(ctx, lo, l.Addr().String()) {
		t.Errorf("couldn't connect bound to %s", lo)
	}
	if dialIPv6(ctx, "no-such-interface", l.Addr().String()) {
		t.Error("connected bound to no interface")
	}
}
