package ping

import (
	"context"
	"os/exec"
	"slices"
	"testing"
	"time"
)

func TestPingArgs(t *testing.T) {
	if got := pingArgs("1.1.1.1", "eth0", 2500*time.Millisecond); !slices.Equal(got, []string{"-n", "-c", "1", "-W", "2", "-I", "eth0", "1.1.1.1"}) {
		t.Errorf("args %q", got)
	}
	if got := pingArgs("1.1.1.1", "", 100*time.Millisecond); !slices.Equal(got, []string{"-n", "-c", "1", "-W", "1", "1.1.1.1"}) {
		t.Errorf("args %q", got)
	}
}

// The system's ping really answers for this machine itself, where there
// is one and it may send.
func TestICMPLoopback(t *testing.T) {
	if _, err := exec.LookPath("ping"); err != nil {
		t.Skip("no ping command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ms, err := ICMP(ctx, "127.0.0.1", "", time.Second)
	if err != nil {
		t.Skip("ping can't send here:", err)
	}
	if ms < 1 {
		t.Errorf("%d ms", ms)
	}
	if _, err := ICMP(ctx, "::1", "", time.Second); err == nil {
		t.Error("an IPv6 address was taken")
	}
}
