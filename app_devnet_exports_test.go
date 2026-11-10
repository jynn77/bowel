package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportLineCoversBothCases(t *testing.T) {
	line := exportLine("posix", 7899, []string{"localhost", ".ts.net"})
	for _, want := range []string{"http_proxy=http://127.0.0.1:7899", "HTTPS_PROXY=http://127.0.0.1:7899", `NO_PROXY="localhost,.ts.net"`} {
		if !strings.Contains(line, want) {
			t.Errorf("no %q in %s", want, line)
		}
	}
	if strings.Contains(strings.ToLower(line), "all_proxy") || strings.Contains(line, "socks5") {
		t.Errorf("all_proxy makes tools without SOCKS support fail: %s", line)
	}
}

func TestFishSyntax(t *testing.T) {
	if l := exportLine("fish", 7899, []string{"localhost"}); strings.Contains(l, "export") || !strings.Contains(l, "set -gx HTTPS_PROXY http://127.0.0.1:7899") {
		t.Error(l)
	}
	if l := unsetLine("fish"); !strings.Contains(l, "set -e https_proxy") {
		t.Error(l)
	}
}

// The real shells: the line sets every variable, and the other takes them off.
func TestLinesInRealShells(t *testing.T) {
	const probe = `printf '%s|%s|%s|%s|%s' "$http_proxy" "$https_proxy" "$HTTP_PROXY" "$HTTPS_PROXY" "$NO_PROXY"`
	for _, shell := range []string{"bash", "zsh"} {
		bin, err := exec.LookPath(shell)
		if err != nil {
			continue
		}
		run := func(lines ...string) string {
			out, err := exec.Command(bin, "-c", strings.Join(append(lines, probe), "; ")).Output()
			if err != nil {
				t.Fatal(shell, err)
			}
			return string(out)
		}
		line, a := exportLine("posix", 7899, []string{"localhost", ".ts.net"}), "http://127.0.0.1:7899"
		if got, want := run(line), fmt.Sprintf("%[1]s|%[1]s|%[1]s|%[1]s|localhost,.ts.net", a); got != want {
			t.Errorf("%s: %q, want %q", shell, got, want)
		}
		if got := run(line, unsetLine("posix")); got != "||||" {
			t.Errorf("%s after unset: %q", shell, got)
		}
	}
}

// TestSaveLines writes the two lines where a script can try them against a
// running proxy.
func TestSaveLines(t *testing.T) {
	dir := os.Getenv("BOWEL_SHELL_LINES")
	if dir == "" {
		t.Skip("BOWEL_SHELL_LINES not set")
	}
	text := exportLine("posix", 7899, []string{"localhost", "127.0.0.1", "::1"}) + "\n" + unsetLine("posix") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "line"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
