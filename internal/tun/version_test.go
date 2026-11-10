//go:build !windows

// The fake core below is a #!/bin/sh script: only a unix can run it.

package tun

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeCore(t *testing.T, line string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mihomo")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho '"+line+"'\necho 'Use tags: x'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStale(t *testing.T) {
	v1 := fakeCore(t, "Mihomo Meta v1.19.33 darwin arm64 with go1.26.10 Wed Oct 21 2026")
	same := fakeCore(t, "Mihomo Meta v1.19.33 darwin arm64 with go1.26.11 Thu Oct 22 2026")
	v2 := fakeCore(t, "Mihomo Meta v1.20.0 darwin arm64 with go1.26.10 Wed Oct 21 2026")
	junk := fakeCore(t, "something else")
	for _, c := range []struct {
		name, a, b string
		want       bool
	}{
		{"other version", v2, v1, true},
		{"same version, another build", same, v1, false},
		{"unreadable output", junk, v1, false},
		{"missing file", v1, filepath.Join(t.TempDir(), "none"), false},
	} {
		if got := Stale(c.a, c.b); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}
