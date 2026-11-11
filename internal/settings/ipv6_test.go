package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oboo/moho/internal/core"
)

func openWith(t *testing.T, file string) *Store {
	t.Helper()
	dir := t.TempDir()
	if file != "" {
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestIPv6Migration(t *testing.T) {
	for file, want := range map[string]core.IPv6Mode{
		``:                core.IPv6Auto, // a new install
		`{"mixedPort":1}`: core.IPv6Auto,
		`{"ipv6":true}`:   core.IPv6Auto, // every file from before the modes says this
		`{"ipv6":false}`:  core.IPv6Off,
		`{"ipv6":"on"}`:   core.IPv6On,
		`{"ipv6":"off"}`:  core.IPv6Off,
		`{"ipv6":"auto"}`: core.IPv6Auto,
		`{"ipv6":"nope"}`: core.IPv6Auto,
	} {
		if got := openWith(t, file).Get().IPv6; got != want {
			t.Errorf("%s: %q, want %q", file, got, want)
		}
	}
}

// Choosing On is not undone by opening the file again, and Off stays
// readable by a version from before the modes.
func TestIPv6Saved(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for mode, want := range map[core.IPv6Mode]string{core.IPv6On: `"ipv6": "on"`, core.IPv6Off: `"ipv6": false`, core.IPv6Auto: `"ipv6": "auto"`} {
		if _, err := st.Update(func(s *Settings) { s.IPv6 = mode }); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
		if !strings.Contains(string(b), want) {
			t.Errorf("%s isn't saved as %s", mode, want)
		}
		again, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := again.Get().IPv6; got != mode {
			t.Errorf("%s came back as %s", mode, got)
		}
	}
}
