package devtools

import (
	"context"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestShellOf(t *testing.T) {
	for _, c := range []struct{ goos, env, want string }{
		{"darwin", "/bin/bash", "zsh"}, {"darwin", "", "zsh"},
		{"linux", "/usr/bin/zsh", "zsh"}, {"linux", "/bin/bash", "bash"}, {"linux", "", "bash"}, {"linux", "/bin/sh", "bash"},
		{"linux", "/usr/bin/fish", ""}, {"linux", "/usr/bin/nu", ""},
	} {
		if got := shellOf(c.goos, c.env); got != c.want {
			t.Errorf("%s with SHELL=%q: %q, want %q", c.goos, c.env, got, c.want)
		}
	}
}

func TestNetcatKind(t *testing.T) {
	for help, want := range map[string]string{
		"OpenBSD netcat (Debian patchlevel 1.226-1ubuntu2)\nusage: nc [-46CDdFhklNnrStUuvZz] [-I length] [-X proxy_protocol] [-x proxy_address[:port]]": openbsdKind,
		"usage: nc [-46AacCDdEFhklMnOortUuvz] [-K tc] [-X proxy_protocol] [-x proxy_address[:port]]":                                                    openbsdKind,
		"Ncat 7.94 ( https://nmap.org/ncat )\nUsage: ncat [options] [hostname] [port]":                                                                  ncatKind,
		"[v1.10-47]\nconnect to somewhere:\tnc [-options] hostname port[s] [ports] ...":                                                                 "",
		"": "",
	} {
		if got := netcatKind(help); got != want {
			t.Errorf("%q: %q, want %q", help, got, want)
		}
	}
}

// bash gets its block in .bashrc, with no netcat in it; the shell that
// is the user's is the one read, and undoing clears either file.
func TestBashBlock(t *testing.T) {
	o := opts(t)
	o.Shell, o.Netcat = "bash", "none"
	put(t, o.path(bashFile), "alias ll='ls -l'\n")
	if tool := o.readShell(context.Background()); tool.File != "~/.bashrc" {
		t.Fatalf("read at %q", tool.File)
	}
	if err := Apply(o, Shell); err != nil {
		t.Fatal(err)
	}
	text := get(t, o.path(bashFile))
	if !strings.HasPrefix(text, "alias ll='ls -l'\n") || !strings.Contains(text, "(exec 3<>/dev/tcp/127.0.0.1/7899) 2>/dev/null") || strings.Contains(text, "nc ") {
		t.Fatalf(".bashrc:\n%s", text)
	}
	if tool := o.readShell(context.Background()); tool.State != On || tool.Source != SourceBowel || tool.Port != 7899 {
		t.Errorf("read back: %+v", tool)
	}
	o.Port = 7900
	if tool := o.readShell(context.Background()); tool.State != Stale {
		t.Errorf("after the port moved: %+v", tool)
	}
	// The user moves to zsh: the block in .bashrc is still bowel's to undo.
	o.Shell = "zsh"
	if changed, err := Undo(o, Shell); err != nil || !changed {
		t.Fatalf("undo: %v, %v", changed, err)
	}
	if got := get(t, o.path(bashFile)); got != "alias ll='ls -l'\n" {
		t.Errorf("after undo:\n%s", got)
	}
}

// The block does what it says in a real bash: the proxy's variables are
// there while the port takes connections, and not otherwise.
func TestBashBlockRuns(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	o := opts(t)
	o.Shell, o.Port = "bash", l.Addr().(*net.TCPAddr).Port
	script := strings.Join(o.shellBlock(), "\n") + "\nprintf '%s' \"${https_proxy:-none}\"\n"
	run := func() string {
		cmd := exec.Command(bash, "-c", script)
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("bash: %v\n%s", err, script)
		}
		return string(out)
	}
	if got, want := run(), "http://127.0.0.1:"+strconv.Itoa(o.Port); got != want {
		t.Errorf("with the port open: %q, want %q", got, want)
	}
	l.Close()
	if got := run(); got != "none" {
		t.Errorf("with the port closed: %q", got)
	}
}

// ssh's block calls the netcat the machine has, and there is none to
// write with a netcat that cannot speak SOCKS.
func TestSSHBlockNetcats(t *testing.T) {
	o := opts(t)
	o.Netcat = ncatKind
	b := strings.Join(o.sshBlock(), "\n")
	if !strings.Contains(b, `exec "ncat -z 127.0.0.1 7899"`) || !strings.Contains(b, "ProxyCommand ncat --proxy-type socks5 --proxy 127.0.0.1:7899 %h %p") {
		t.Errorf("with ncat:\n%s", b)
	}
	if blockPortOf(o.sshBlock()) != 7899 {
		t.Errorf("port read back: %d", blockPortOf(o.sshBlock()))
	}
	o.Netcat = "none"
	if err := Apply(o, GitSSH); err == nil {
		t.Error("written with no netcat")
	}
	if err := Apply(o, Shell); err == nil {
		t.Error("zsh's block written with no netcat")
	}
	o.Shell = "fish"
	if o.shellRel() != "" {
		t.Errorf("a file for fish: %q", o.shellRel())
	}
	o.Shell = ""
	if tool := (Options{Home: o.Home, Shell: "none-such"}).readShell(context.Background()); tool.State != Unknown || tool.File != "" {
		t.Errorf("an unknown shell: %+v", tool)
	}
}

// git is found where it is: only a Mac's /usr/bin/git may be a stub.
func TestGitFound(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS has the stub to tell apart")
	}
	if p, _ := exec.LookPath("git"); p != "" && git() == "" {
		t.Errorf("git is at %s and was not found", p)
	}
}
