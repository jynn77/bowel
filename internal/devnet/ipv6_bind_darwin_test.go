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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !dialIPv6(ctx, "lo0", l.Addr().String()) {
		t.Error("couldn't connect bound to the loopback")
	}
	if dialIPv6(ctx, "no-such-interface", l.Addr().String()) {
		t.Error("connected bound to no interface")
	}
}
