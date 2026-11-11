package sysproxy

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeDesktop puts stand-ins for gsettings and kwriteconfig6 first in
// PATH: each keeps what it is told in files of the test's, so nothing of
// the machine's is read or changed. It returns where GNOME's keys are.
func fakeDesktop(t *testing.T, kdeSession bool) (gnomeDir string) {
	t.Helper()
	dir := t.TempDir()
	bin, store, conf := filepath.Join(dir, "bin"), filepath.Join(dir, "gs"), filepath.Join(dir, "config")
	for _, d := range []string{bin, store, conf} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script("gsettings", `case "$1" in
get) cat "$FAKE_GS/$2 $3" 2>/dev/null || { echo "No such schema" >&2; exit 1; } ;;
set) printf '%s\n' "$4" > "$FAKE_GS/$2 $3" ;;
list-recursively) for f in "$FAKE_GS"/*; do printf '%s %s\n' "$(basename "$f")" "$(cat "$f")"; done ;;
*) exit 2 ;;
esac
`)
	script("kwriteconfig6", `f="$XDG_CONFIG_HOME/$2"
[ "$1" = --file ] && [ "$3" = --group ] && [ "$5" = --key ] || exit 2
[ -f "$f" ] || printf '[%s]\n' "$4" > "$f"
grep -v "^$6=" "$f" > "$f.tmp"
printf '%s=%s\n' "$6" "$7" >> "$f.tmp"
mv "$f.tmp" "$f"
`)
	script("dbus-send", "exit 0\n")
	for k, v := range map[string]string{
		"org.gnome.system.proxy mode": "'none'", "org.gnome.system.proxy autoconfig-url": "''",
		"org.gnome.system.proxy ignore-hosts": "['localhost', '127.0.0.0/8', '::1']",
		"org.gnome.system.proxy.http host":    "''", "org.gnome.system.proxy.http port": "8080",
		"org.gnome.system.proxy.https host": "''", "org.gnome.system.proxy.https port": "0",
		"org.gnome.system.proxy.socks host": "''", "org.gnome.system.proxy.socks port": "0",
	} {
		if err := os.WriteFile(filepath.Join(store, k), []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("FAKE_GS", store)
	t.Setenv("XDG_CONFIG_HOME", conf)
	t.Setenv("KDE_FULL_SESSION", "")
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	if kdeSession {
		t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	}
	return store
}

// The whole round on GNOME: what was there is read, the proxy is pointed
// and measured as bowel's, a change behind bowel's back shows, and
// Restore leaves the desktop exactly as it was.
func TestGnomeRoundTrip(t *testing.T) {
	store := fakeDesktop(t, false)
	key := func(k, v string) {
		if err := os.WriteFile(filepath.Join(store, k), []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	key("org.gnome.system.proxy mode", "'auto'")
	key("org.gnome.system.proxy autoconfig-url", "'http://wpad.corp/proxy.pac'")
	key("org.gnome.system.proxy.http host", "'old.corp'")
	key("org.gnome.system.proxy.http port", "3128")

	before, err := Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Services) != 1 || before.Services[0].Name != gnomeName || !before.Services[0].PACOn || before.Services[0].Web != (Endpoint{Host: "old.corp", Port: 3128}) {
		t.Fatalf("snapshot: %+v", before)
	}
	if s, err := Check(7890); err != nil || s.On || s.Ours {
		t.Fatalf("before: %+v, %v", s, err)
	}
	if err := Enable("127.0.0.1", 7890, []string{"*.local", "169.254/16"}); err != nil {
		t.Fatal(err)
	}
	if s, err := Check(7890); err != nil || !s.Ours || s.Host != "127.0.0.1" || s.Port != 7890 {
		t.Fatalf("after Enable: %+v, %v", s, err)
	}
	if b, _ := os.ReadFile(filepath.Join(store, "org.gnome.system.proxy ignore-hosts")); strings.TrimSpace(string(b)) != "['*.local', '169.254.0.0/16']" {
		t.Errorf("ignore-hosts: %s", b)
	}
	// Another program points HTTPS elsewhere.
	key("org.gnome.system.proxy.https port", "9999")
	if s, _ := Check(7890); !s.On || s.Ours {
		t.Errorf("hijacked: %+v", s)
	}
	key("org.gnome.system.proxy.https port", "7890")
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	if s, _ := Check(7890); s.On || s.Ours {
		t.Errorf("after Disable: %+v", s)
	}
	if err := Restore(context.Background(), before); err != nil {
		t.Fatal(err)
	}
	after, err := Snapshot()
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Errorf("after Restore:\n got %+v\nwant %+v (%v)", after, before, err)
	}
	// With nothing saved, Restore turns the proxies off.
	if err := Enable("127.0.0.1", 7890, nil); err != nil {
		t.Fatal(err)
	}
	if err := Restore(context.Background(), Saved{}); err != nil {
		t.Fatal(err)
	}
	if s, _ := Check(7890); s.On {
		t.Errorf("after Restore of nothing: %+v", s)
	}
}

// A Plasma session keeps both in step, and is measured by KDE's own.
func TestKdeRoundTrip(t *testing.T) {
	fakeDesktop(t, true)
	file := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), kdeFile)
	before, err := Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Services) != 2 || before.Services[0].Name != kdeName || before.Services[1].Name != gnomeName {
		t.Fatalf("snapshot: %+v", before)
	}
	if err := Enable("127.0.0.1", 7890, []string{"*.local"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	for _, want := range []string{"[Proxy Settings]", "ProxyType=1", "httpProxy=http://127.0.0.1 7890", "httpsProxy=http://127.0.0.1 7890", "NoProxyFor=*.local"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("kioslaverc lacks %q:\n%s", want, b)
		}
	}
	if s, err := Check(7890); err != nil || !s.Ours {
		t.Fatalf("after Enable: %+v, %v", s, err)
	}
	// KDE's file is the one measured: GNOME's alone going off doesn't show.
	if err := (gnome{bin: "gsettings"}).write(context.Background(), Service{}); err != nil {
		t.Fatal(err)
	}
	if s, _ := Check(7890); !s.Ours {
		t.Errorf("measured by GNOME's in a Plasma session: %+v", s)
	}
	if err := Restore(context.Background(), before); err != nil {
		t.Fatal(err)
	}
	if after, err := Snapshot(); err != nil || !reflect.DeepEqual(after, before) {
		t.Errorf("after Restore:\n got %+v\nwant %+v (%v)", after, before, err)
	}
}

// A session with neither desktop: Enable says why, and nothing reads on.
func TestNoDesktop(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDG_CURRENT_DESKTOP", "sway")
	t.Setenv("KDE_FULL_SESSION", "")
	if err := Enable("127.0.0.1", 7890, nil); err != errNoDesktop {
		t.Errorf("Enable: %v", err)
	}
	if s, err := Check(7890); err != nil || s.On {
		t.Errorf("Check: %+v, %v", s, err)
	}
	if err := Disable(); err != nil {
		t.Errorf("Disable: %v", err)
	}
	if saved, err := Snapshot(); err != nil || len(saved.Services) != 0 {
		t.Errorf("Snapshot: %+v, %v", saved, err)
	}
}
