package main

import (
	"strings"
	"testing"

	"github.com/oboo/moho/internal/devtools"
	"github.com/oboo/moho/internal/i18n"
)

// Each system's lines name that system's own things, and no other's.
func TestPlatformLines(t *testing.T) {
	saved := i18n.Lang()
	defer i18n.Set(saved)
	i18n.Set("en")
	if s := tunInstallHintOn("darwin"); !strings.Contains(s, "macOS") {
		t.Errorf("macOS: %q", s)
	}
	if s := tunInstallHintOn("windows"); !strings.Contains(s, "User Account Control") || strings.Contains(s, "macOS") {
		t.Errorf("Windows: %q", s)
	}
	if s := tunInstallHintOn("linux"); strings.Contains(s, "macOS") || strings.Contains(s, "Windows") {
		t.Errorf("Linux: %q", s)
	}
	if s := firewallTipOn("darwin"); !strings.Contains(s, "System Settings") {
		t.Errorf("macOS firewall: %q", s)
	}
	if s := firewallTipOn("linux"); !strings.Contains(s, "ufw") || strings.Contains(s, "macOS") {
		t.Errorf("Linux firewall: %q", s)
	}
}

func TestToolSubOf(t *testing.T) {
	for file, want := range map[string]string{"~/.zshenv": "zsh", "~/.bashrc": "bash", "": ""} {
		if got := toolSubOf(devtools.Tool{ID: devtools.Shell, File: file}); got != want {
			t.Errorf("shell at %q: %q", file, got)
		}
	}
	if got := toolSubOf(devtools.Tool{ID: devtools.GitSSH, File: "~/.ssh/config"}); got != "git@github.com" {
		t.Errorf("Git SSH: %q", got)
	}
}
