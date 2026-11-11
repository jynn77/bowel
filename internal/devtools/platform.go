package devtools

// What differs between the systems: which shell a terminal runs, and
// which netcat ssh and the shell can call.
//
// macOS is zsh and the OpenBSD nc it ships, always. Linux is whatever
// $SHELL names, of the two bowel writes for, and whichever netcat is
// installed. Windows has neither: its terminals are PowerShell and cmd,
// whose environment is the user's registry and a profile script, and its
// ssh has no nc to call; nothing is connected there yet.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/nowindow"
)

// Supported says whether bowel can connect developer tools on this
// system.
func Supported() bool { return runtime.GOOS != "windows" }

var errUnsupported error = unsupported{}

type unsupported struct{}

func (unsupported) Error() string {
	return i18n.T("Windows 上还不能接入开发工具", "Developer tools can't be connected under Windows yet")
}

const bashFile = ".bashrc"

// shell is the terminal's shell: "zsh", "bash", or "" for one bowel
// writes nothing for.
func (o Options) shell() string {
	if o.Shell != "" {
		return o.Shell
	}
	return shellOf(runtime.GOOS, os.Getenv("SHELL"))
}

// shellOf is the shell of a system whose $SHELL is env. macOS's
// terminals run zsh whatever a process started from Finder has there.
func shellOf(goos, env string) string {
	if goos != "linux" {
		return "zsh"
	}
	switch name := filepath.Base(env); name {
	case "zsh", "bash":
		return name
	case "", ".", "sh", "dash":
		return "bash" // no shell of the user's choosing: the system's
	}
	return ""
}

// shellRel is the file the shell reads whenever a terminal opens: zsh's
// .zshenv, which every zsh reads, and bash's .bashrc, which interactive
// ones do (and login ones through the system's .profile).
func (o Options) shellRel() string {
	switch o.shell() {
	case "zsh":
		return shellFile
	case "bash":
		return bashFile
	}
	return ""
}

const (
	openbsdKind = "openbsd" // nc -z, nc -X 5 -x proxy host port
	ncatKind    = "ncat"    // ncat -z, ncat --proxy-type socks5 --proxy proxy host port
)

var netcatOnce struct {
	sync.Once
	kind string
}

// netcat is the kind of netcat this machine has, "" for none that can
// speak SOCKS (the "traditional" nc cannot).
func (o Options) netcat() string {
	if o.Netcat != "" {
		if o.Netcat == "none" {
			return ""
		}
		return o.Netcat
	}
	if runtime.GOOS == "darwin" {
		return openbsdKind
	}
	netcatOnce.Do(func() { netcatOnce.kind = detectNetcat() })
	return netcatOnce.kind
}

func detectNetcat() string {
	if nc := find("nc"); nc != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, _ := nowindow.CommandContext(ctx, nc, "-h").CombinedOutput()
		if kind := netcatKind(string(out)); kind != "" {
			return kind
		}
	}
	if find("ncat") != "" {
		return ncatKind
	}
	return ""
}

// netcatKind tells a netcat by what `nc -h` prints: OpenBSD's lists its
// proxy options, Nmap's names itself (Fedora installs it as nc).
func netcatKind(help string) string {
	switch {
	case strings.Contains(help, "Ncat"):
		return ncatKind
	case strings.Contains(help, "-X proto") || strings.Contains(help, "proxy_protocol") || strings.Contains(help, "OpenBSD"):
		return openbsdKind
	}
	return ""
}

// listening is a shell command that succeeds while bowel's port takes
// connections. bash asks for the connection itself; zsh calls netcat.
func (o Options) listening() string {
	if o.shell() == "bash" {
		return fmt.Sprintf("(exec 3<>/dev/tcp/127.0.0.1/%d)", o.Port)
	}
	return o.listeningWith(o.netcat())
}

func (o Options) listeningWith(nc string) string {
	if nc == ncatKind {
		return fmt.Sprintf("ncat -z 127.0.0.1 %d", o.Port)
	}
	return fmt.Sprintf("nc -z 127.0.0.1 %d", o.Port)
}

func errNoNetcat() error {
	return errors.New(i18n.T("需要能走 SOCKS 的 netcat：请安装 netcat-openbsd 或 ncat", "This needs a netcat that speaks SOCKS: install netcat-openbsd or ncat"))
}
