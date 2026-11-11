package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/oboo/moho/internal/routes"
)

// outside is a core started as Windows' task starts it: not through the
// manager, and printing to no one.
type outside struct {
	cmd   *exec.Cmd
	stops atomic.Int32
}

func (o *outside) Pid() int    { return o.cmd.Process.Pid }
func (o *outside) Wait() error { return o.cmd.Wait() }
func (o *outside) Stop() {
	o.stops.Add(1)
	if runtime.GOOS == "windows" {
		// SIGTERM cannot be sent on Windows; a stopped core there is a
		// killed one.
		_ = o.cmd.Process.Kill()
		return
	}
	_ = o.cmd.Process.Signal(syscall.SIGTERM)
}

// A core that Spawn starts is waited for, stopped and listened to as a
// child is: its status follows it, and its log comes from the controller.
func TestSpawnedCore(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	bin := coreBin(t)
	dir, err := os.MkdirTemp("", "moho")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	geo(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte(yamlNodes), 0o600)
	m, err := New(bin, dir)
	if err != nil {
		t.Fatal(err)
	}
	var proc *outside
	asked := ""
	m.Spawn = func(b string) (Process, error) {
		asked = b
		cmd := exec.Command(b, "-d", m.Dir, "-f", m.ConfigPath())
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		proc = &outside{cmd: cmd}
		return proc, nil
	}
	probe, _ := FreePort()
	cfg, err := BuildConfig(Input{
		Providers: []ProviderRef{{ID: "aaa", Path: filepath.Join(dir, "a.txt")}},
		Routes:    routes.Template(), Mode: "rule", MixedPort: mustFree(t), LogLevel: "info",
		TestURL: "https://www.gstatic.com/generate_204", ProbePort: probe, Controller: m.ControllerFields(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(cfg); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.Stop()
	if asked != bin || proc == nil {
		t.Fatalf("Spawn asked for %q", asked)
	}
	if s := m.Status(); s.State != Running || s.PID != proc.Pid() {
		t.Fatalf("status %+v, pid %d", s, proc.Pid())
	}
	// A reload makes the core say something, which only the controller
	// can pass on.
	if err := m.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(m.Logs()) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if l := m.Logs(); len(l) == 0 || !strings.HasPrefix(l[0], "level=") {
		t.Errorf("log from the controller: %q", l)
	}
	m.Stop()
	if s := m.Status(); s.State != Stopped || proc.stops.Load() == 0 {
		t.Fatalf("after stop: %+v, %d stops", s, proc.stops.Load())
	}
}

// Spawn's failure is the start's, and a nil process leaves the core to
// start as a child.
func TestSpawnRefuses(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := New(bin, filepath.Join(dir, "core"))
	if err != nil {
		t.Fatal(err)
	}
	m.Spawn = func(string) (Process, error) { return nil, errors.New("no task") }
	if err := m.start(); err == nil || err.Error() != "no task" {
		t.Fatalf("start: %v", err)
	}
	if s := m.Status(); s.State != Failed || s.Error != "no task" {
		t.Fatalf("status %+v", s)
	}
	calls := 0
	m.Spawn = func(string) (Process, error) { calls++; return nil, nil }
	err = m.start()
	if err == nil || calls != 1 {
		t.Fatalf("start with a child that exits: %v, %d calls", err, calls)
	}
	// Both are a core that did not come up, which the app answers with
	// another executable; a config the core refuses is not, and neither
	// is starting after Close.
	var se *StartError
	if !errors.As(err, &se) {
		t.Errorf("a core that exits is not a StartError: %T", err)
	}
	sentinel := errors.New("gone")
	m.Spawn = func(string) (Process, error) { return nil, fmt.Errorf("spawn: %w", sentinel) }
	if err := m.start(); !errors.As(err, &se) || !errors.Is(err, sentinel) {
		t.Errorf("Spawn's error is not kept: %v", err)
	}
	if err := m.Apply([]byte("mixed-port: 1\n")); err == nil || errors.As(err, &se) {
		t.Errorf("a refused config is a StartError: %v", err)
	}
	m.Close()
	if err := m.start(); err != ErrClosed {
		t.Errorf("after Close: %v", err)
	}
}
