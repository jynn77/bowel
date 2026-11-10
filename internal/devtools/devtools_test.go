package devtools

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func opts(t *testing.T) Options {
	dir := t.TempDir()
	return Options{Home: filepath.Join(dir, "home"), DataDir: filepath.Join(dir, "data"), Port: 7899}
}

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStripDeletesWholeBlocksOnly(t *testing.T) {
	block := strings.Join(wrap("", "a", "b"), "\n")
	for _, c := range []struct{ in, want string }{
		{"x\n" + block + "\ny\n", "x\ny\n"},
		{block + "\n", ""},
		{"# >>> bowel never closed\nmine\n", "# >>> bowel never closed\nmine\n"},
		{"plain\n", "plain\n"},
	} {
		if got := strip(c.in); got != c.want {
			t.Errorf("strip(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

const colimaSample = `# Number of CPUs
cpu: 2

network:
  # the gateway
  gatewayAddress: 192.168.5.2

# EXAMPLE - add insecure registries
# docker:
#   insecure-registries: []
docker: {}

# Environment variables
env: {}
`

func TestColimaRoundTrip(t *testing.T) {
	o := opts(t)
	path := o.path(colimaConfig)
	put(t, path, colimaSample)
	if err := Apply(o, Dockerd); err != nil {
		t.Fatal(err)
	}
	var f colimaFile
	if err := yaml.Unmarshal([]byte(get(t, path)), &f); err != nil {
		t.Fatal(err)
	}
	if f.Docker.Proxies.HTTP != "http://192.168.5.2:7899" {
		t.Errorf("http-proxy = %q", f.Docker.Proxies.HTTP)
	}
	if !strings.Contains(get(t, path), "# Environment variables") {
		t.Error("comments lost")
	}
	if ok, err := Undo(o, Dockerd); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if got := get(t, path); got != colimaSample {
		t.Errorf("after undo:\n%s", got)
	}
}

// Colima may drop the comments when it rewrites the file; the key still goes.
func TestColimaUndoWithoutComments(t *testing.T) {
	o := opts(t)
	path := o.path(colimaConfig)
	put(t, path, colimaSample)
	if err := Apply(o, Dockerd); err != nil {
		t.Fatal(err)
	}
	var keep []string
	for _, l := range strings.Split(get(t, path), "\n") {
		if !isOpen(l) && !isClose(l) {
			keep = append(keep, l)
		}
	}
	put(t, path, strings.Join(keep, "\n"))
	if _, err := Undo(o, Dockerd); err != nil {
		t.Fatal(err)
	}
	if got := get(t, path); got != colimaSample {
		t.Errorf("after undo:\n%s", got)
	}
}

func TestColimaKeepsOtherDockerKeys(t *testing.T) {
	text := "docker:\n  insecure-registries:\n    - a:5000\nenv: {}\n"
	got, err := setColimaProxies(text, wrap("  ", "proxies:", "  http-proxy: http://x:1"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "insecure-registries") {
		t.Fatal(got)
	}
	if back := dropColimaProxies(got); back != text {
		t.Errorf("got %q, want %q", back, text)
	}
}

func TestContainersOnlyTouchesWhatBowelWrote(t *testing.T) {
	o := opts(t)
	path := o.path(dockerConfig)
	put(t, path, `{"auths":{"x":{}},"proxies":{"default":{"httpProxy":"http://mine:1"}}}`)
	if err := Apply(o, Containers); err == nil {
		t.Error("overwrote the user's proxies")
	}
	if ok, _ := Undo(o, Containers); ok {
		t.Error("removed the user's proxies")
	}

	put(t, path, `{"auths":{"x":{}}}`)
	if err := Apply(o, Containers); err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(get(t, path)), &m); err != nil {
		t.Fatal(err)
	}
	if containerValue(m) != "http://192.168.5.2:7899" || m["auths"] == nil {
		t.Errorf("%s", get(t, path))
	}
	if ok, err := Undo(o, Containers); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if strings.Contains(get(t, path), "proxies") || !strings.Contains(get(t, path), "auths") {
		t.Errorf("%s", get(t, path))
	}
}

func TestStalePort(t *testing.T) {
	o := opts(t)
	put(t, o.path(sshFile), "Host a\n")
	if err := Apply(o, GitSSH); err != nil {
		t.Fatal(err)
	}
	moved := o
	moved.Port = 7000
	got := moved.readGitSSH(context.Background())
	if got.State != Stale || got.Port != 7899 || got.ProxyAction() != KindUpdate {
		t.Errorf("%+v", got)
	}
	if err := Apply(moved, GitSSH); err != nil {
		t.Fatal(err)
	}
	if s := get(t, o.path(sshFile)); strings.Contains(s, "7899") || strings.Count(s, blockClose) != 1 {
		t.Error(s)
	}
}

func TestActions(t *testing.T) {
	for _, c := range []struct {
		t             Tool
		proxy, direct string
	}{
		{Tool{ID: Dockerd, State: Off, Source: SourceNone}, KindApply, ""},
		{Tool{ID: Dockerd, State: On, Source: SourceBowel, Applied: AppliedProxy}, "", KindUndo},
		{Tool{ID: Containers, State: Stale, Source: SourceBowel, Applied: AppliedProxy}, KindUpdate, KindUndo},
		{Tool{ID: Shell, State: On, Source: SourceOwn}, "", ""},
		{Tool{ID: Shell, State: Other, Source: SourceOwn}, "", ""},
		{Tool{ID: Langs, State: Off, Source: SourceFollow}, "", ""},
		{Tool{ID: GitHTTPS, State: On, Source: SourceFollow}, "", KindDirect},
		{Tool{ID: GitHTTPS, State: Off, Source: SourceFollow}, "", ""},
		{Tool{ID: GitHTTPS, State: Off, Source: SourceBowel, Applied: AppliedDirect}, KindUndo, ""},
	} {
		if p, d := c.t.ProxyAction(), c.t.DirectAction(); p != c.proxy || d != c.direct {
			t.Errorf("%+v: proxy %q direct %q, want %q %q", c.t, p, d, c.proxy, c.direct)
		}
	}
}

// The real ssh: the block sends github.com to the proxy only while the port
// is open, and leaves what follows it where it was.
func TestSSHBlockFollowsThePort(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no ssh")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	o := opts(t)
	o.Port = l.Addr().(*net.TCPAddr).Port
	path := o.path(sshFile)
	put(t, path, "User everyone\n")
	if err := Apply(o, GitSSH); err != nil {
		t.Fatal(err)
	}
	g := func(host string) string {
		out, err := exec.Command("ssh", "-G", "-F", path, host).Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	if s := g("github.com"); !strings.Contains(s, "proxycommand nc -X 5 -x 127.0.0.1:"+strconv.Itoa(o.Port)) {
		t.Errorf("port open, no proxy:\n%s", s)
	}
	if s := g("example.com"); strings.Contains(s, "proxycommand nc") {
		t.Errorf("another host got the proxy:\n%s", s)
	}
	if s := g("example.com"); !strings.Contains(s, "user everyone") {
		t.Errorf("a setting after the block stopped applying:\n%s", s)
	}
	l.Close()
	if s := g("github.com"); strings.Contains(s, "proxycommand nc") {
		t.Errorf("port closed, still the proxy:\n%s", s)
	}
}

// The real git: the block overrides the proxy from the environment.
func TestGitDirectOverridesEnvironment(t *testing.T) {
	bin := git()
	if bin == "" {
		t.Skip("no git")
	}
	o := opts(t)
	if err := Direct(o, GitHTTPS); err != nil {
		t.Fatal(err)
	}
	c := o.cmd(context.Background(), bin, "config", "--global", "--get-urlmatch", "http.proxy", "https://github.com")
	out, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("proxy = %q", out)
	}
	got := o.readGitHTTPS(context.Background(), Tool{State: On})
	if got.State != Off || got.Applied != AppliedDirect || got.ProxyAction() != KindUndo {
		t.Errorf("%+v", got)
	}
}

func TestUndoAllLeavesTheRest(t *testing.T) {
	o := opts(t)
	put(t, o.path(colimaConfig), colimaSample)
	put(t, o.path(shellFile), "export A=1\n")
	put(t, o.path(sshFile), "Host a\n")
	put(t, o.path(dockerConfig), "{}")
	for _, id := range []string{Dockerd, Containers, Shell, GitSSH} {
		if err := Apply(o, id); err != nil {
			t.Fatal(id, err)
		}
	}
	got, err := UndoAll(o)
	if err != nil || len(got) != 4 {
		t.Fatal(got, err)
	}
	if s := get(t, o.path(shellFile)); s != "export A=1\n" {
		t.Error(s)
	}
	if s := get(t, o.path(sshFile)); s != "Host a\n" {
		t.Error(s)
	}
	if got, _ := UndoAll(o); len(got) != 0 {
		t.Error("second undo changed", got)
	}
}

func TestWriteGoesThroughSymlink(t *testing.T) {
	o := opts(t)
	real := filepath.Join(filepath.Dir(o.Home), "dotfiles", "zshenv")
	put(t, real, "export A=1\n")
	if err := os.MkdirAll(o.Home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, o.path(shellFile)); err != nil {
		t.Fatal(err)
	}
	if err := Apply(o, Shell); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Lstat(o.path(shellFile)); st.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced")
	}
	if !strings.Contains(get(t, real), "nc -z") {
		t.Error("not written to the real file")
	}
}
