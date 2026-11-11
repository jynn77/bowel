package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Exercise the desktop upload's file selection and the script that assembles
// those artifacts into a release. Signing is stubbed; no release key is needed.
func TestLinuxInstallerReleaseAsset(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the release assembly script runs on Linux")
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string
				With struct{ Path string }
			}
		}
	}
	data, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	var patterns []string
	for _, step := range workflow.Jobs["desktop"].Steps {
		if strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
			patterns = append(patterns, strings.Fields(step.With.Path)...)
		}
	}
	if len(patterns) == 0 {
		t.Fatal("desktop artifact upload has no paths")
	}
	root := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	installer := "#!/bin/sh\n# Generated for both Linux architectures.\n"
	for _, arch := range []string{"amd64", "arm64"} {
		job := filepath.Join(root, arch)
		build := filepath.Join(job, "build", "linux-"+arch)
		write(filepath.Join(build, "install.sh"), installer)
		write(filepath.Join(build, "bowel-0.3.0-linux-"+arch+".tar.gz"), "archive fixture")
		write(filepath.Join(build, "bowel-0.3.0-1."+arch+".rpm"), "rpm fixture")
		for _, pattern := range patterns {
			matches, err := filepath.Glob(filepath.Join(job, pattern))
			if err != nil {
				t.Fatal(err)
			}
			for _, match := range matches {
				content, err := os.ReadFile(match)
				if err != nil {
					t.Fatal(err)
				}
				write(filepath.Join(root, "release", "linux-"+arch, filepath.Base(match)), string(content))
			}
		}
	}
	bin := filepath.Join(root, "bin")
	write(filepath.Join(bin, "gpg"), "#!/bin/sh\ncase \"$*\" in *--list-secret-keys*) printf 'fpr:::::::::TESTKEY:\\n';; *) cat >/dev/null;; esac\n")
	write(filepath.Join(bin, "gpgconf"), "#!/bin/sh\nexit 0\n")
	write(filepath.Join(bin, "rpmsign"), "#!/bin/sh\nexit 0\n")
	out := filepath.Join(root, "dist")
	cmd := exec.Command("bash", "scripts/prepare-release.sh", filepath.Join(root, "release"), out)
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "PACKAGES_GPG_PRIVATE_KEY=test-only")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("prepare-release: %v\n%s", err, output)
	}
	got, err := os.ReadFile(filepath.Join(out, "install.sh"))
	if err != nil {
		t.Fatalf("Linux installer missing from release: %v", err)
	}
	if string(got) != installer {
		t.Fatalf("installer changed while assembling the release: %q", got)
	}
	sums, err := os.ReadFile(filepath.Join(out, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x  install.sh", sha256.Sum256(got))
	if !strings.Contains(string(sums), want+"\n") {
		t.Fatalf("SHA256SUMS missing %q:\n%s", want, sums)
	}
}
