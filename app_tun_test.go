package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/settings"
)

func TestTunNeedsInstall(t *testing.T) {
	saved := osTun
	defer func() { osTun = saved }()
	installed := true
	osTun.installed = func(string) bool { return installed }
	a := &App{}
	if a.tunNeedsInstall() {
		t.Error("current copy: no authorization needed")
	}
	a.tunStale.Store(true)
	if !a.tunNeedsInstall() {
		t.Error("stale copy: needs one")
	}
	a.tunStale.Store(false)
	installed = false
	if !a.tunNeedsInstall() {
		t.Error("no copy: needs one")
	}
}

// TestRefusedTunUpdateKeepsCore: refusing leaves the warning and the core.
func TestRefusedTunUpdateKeepsCore(t *testing.T) {
	saved := osTun
	defer func() { osTun = saved }()
	osTun.install = func(string, string) error { return errors.New("refused") }
	m, err := core.New("/old", filepath.Join(t.TempDir(), "core"))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{core: m, tunBin: "/tun"}
	a.tunStale.Store(true)
	if err := a.installTun(); err == nil {
		t.Fatal("no error")
	}
	if !a.tunStale.Load() || m.Bin != "/old" {
		t.Errorf("stale %v, bin %s", a.tunStale.Load(), m.Bin)
	}
	osTun.install = func(string, string) error { return nil }
	if err := a.installTun(); err != nil {
		t.Fatal(err)
	}
	if a.tunStale.Load() || m.Bin != "/tun" {
		t.Errorf("stale %v, bin %s", a.tunStale.Load(), m.Bin)
	}
}

// TestStaleTunIsANoticeNotAFault: the light stays green, and it only shows
// to those who use TUN.
func TestStaleTunIsANoticeNotAFault(t *testing.T) {
	a := &App{}
	a.tunStale.Store(true)
	snap := Snapshot{Nodes: []NodeView{{Name: "n"}}}
	st, is := a.judge(true, settings.Settings{Tun: true}, core.Status{State: core.Running}, Takeover{}, snap, nil)
	if st != StateOK || is == nil || is.Action != "tun-update" {
		t.Fatalf("state %v, issue %+v", st, is)
	}
	if _, is := a.judge(true, settings.Settings{}, core.Status{State: core.Running}, Takeover{}, snap, nil); is != nil {
		t.Errorf("TUN off: %+v", is)
	}
}
