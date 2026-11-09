package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/gateway"
	"github.com/oboo/moho/internal/monitor"
	"github.com/oboo/moho/internal/settings"
	"github.com/oboo/moho/internal/tun"
)

// gwFake is a system whose forwarding is a variable. With t set, any
// change to the system fails the test.
type gwFake struct {
	t        *testing.T
	fwd      bool
	ready    bool
	awake    int
	prepared [][]tun.Step
	net      gateway.Network
}

func (f *gwFake) Forwarding() (bool, error) { return f.fwd, nil }
func (f *gwFake) SetForwarding(on bool) error {
	if f.t != nil {
		f.t.Error("the test tried to change IP forwarding")
		return errors.New("no")
	}
	f.fwd = on
	return nil
}
func (f *gwFake) Ready() bool { return f.ready }
func (f *gwFake) Prepare(prompt string, before ...tun.Step) error {
	if f.t != nil {
		f.t.Error("the test tried to ask for authorization")
		return errors.New("no")
	}
	if prompt == "" {
		return errors.New("no prompt")
	}
	f.prepared = append(f.prepared, before)
	f.ready = true
	return nil
}
func (f *gwFake) KeepAwake() (func(), error) {
	if f.t != nil {
		f.t.Error("the test tried to keep the Mac awake")
		return nil, errors.New("no")
	}
	f.awake++
	return func() { f.awake-- }, nil
}
func (f *gwFake) Network() gateway.Network    { return f.net }
func (f *gwFake) AnswersDNS(addr string) bool { return f.fwd && addr == f.net.IP+":53" }

// fakeTun replaces the TUN installation for the test: installed says
// whether the root copy is there, and admin counts the dialogs.
func fakeTun(t *testing.T, installed *bool, dialogs *int) {
	saved := osTun
	osTun.installed = func(string) bool { return *installed }
	osTun.step = func(src, dst string) (tun.Step, error) { return tun.Step{"/install ", dst}, nil }
	osTun.admin = func(prompt string, steps ...tun.Step) error {
		*dialogs++
		*installed = true
		return nil
	}
	t.Cleanup(func() { osTun = saved })
}

func gatewayApp(t *testing.T, sys gateway.System) *App {
	dir := t.TempDir()
	st, err := settings.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := core.New("/nonexistent/mihomo", filepath.Join(dir, "core"))
	if err != nil {
		t.Fatal(err)
	}
	return &App{
		dataDir: dir, settings: st, core: cm, coreBin: "/bundle/mihomo", tunBin: filepath.Join(dir, "bin", "mihomo"),
		gw: gateway.New(sys, filepath.Join(dir, "gateway-before.json")), mon: monitor.New(filepath.Join(dir, "stats.json")),
		pending: map[string]bool{},
	}
}

// One administrator dialog at most: TUN's installation rides along with
// the gateway's when both are missing, and nothing is asked when both
// are there.
func TestPrepareGatewayAsksOnce(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		tunThere, ruleThere  bool
		wantPrepare, wantTun int // Prepare calls (with TUN's step or not), TUN-only dialogs
		withStep             bool
	}{
		{"neither", false, false, 1, 0, true},
		{"tun only", true, false, 1, 0, false},
		{"rule only", false, true, 0, 1, false},
		{"both", true, true, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installed, dialogs := tc.tunThere, 0
			fakeTun(t, &installed, &dialogs)
			f := &gwFake{ready: tc.ruleThere}
			a := gatewayApp(t, f)
			if err := a.prepareGateway(); err != nil {
				t.Fatal(err)
			}
			if len(f.prepared) != tc.wantPrepare || dialogs != tc.wantTun {
				t.Fatalf("Prepare %d times, TUN dialogs %d", len(f.prepared), dialogs)
			}
			if tc.wantPrepare > 0 && (len(f.prepared[0]) == 1) != tc.withStep {
				t.Fatalf("steps given to Prepare: %q", f.prepared[0])
			}
			if !f.ready || (!tc.tunThere && !installed && tc.wantTun > 0) {
				t.Fatal("not ready after preparing")
			}
		})
	}
}

// Forwarding follows the settings, and goes back as it was found when the
// gateway is turned off, when takeover pauses, or after a crash.
func TestSyncGateway(t *testing.T) {
	installed, dialogs := true, 0
	fakeTun(t, &installed, &dialogs)
	f := &gwFake{ready: true}
	a := gatewayApp(t, f)
	set := func(fn func(*settings.Settings)) {
		t.Helper()
		if _, err := a.settings.Update(fn); err != nil {
			t.Fatal(err)
		}
		a.syncGateway()
	}
	set(func(s *settings.Settings) { s.Gateway, s.Tun, s.Paused = true, true, false })
	if !f.fwd || f.awake != 1 {
		t.Fatalf("on: forwarding %v, awake %d", f.fwd, f.awake)
	}
	set(func(s *settings.Settings) { s.Paused = true })
	if f.fwd || f.awake != 0 {
		t.Fatalf("paused: forwarding %v, awake %d", f.fwd, f.awake)
	}
	set(func(s *settings.Settings) { s.Paused = false })
	if !f.fwd {
		t.Fatal("resumed: forwarding off")
	}
	// A crash: the next start, with the gateway off, puts it back.
	b := gatewayApp(t, f)
	b.settings, b.gw = a.settings, gateway.New(f, filepath.Join(a.dataDir, "gateway-before.json"))
	if _, err := b.settings.Update(func(s *settings.Settings) { s.Gateway = false }); err != nil {
		t.Fatal(err)
	}
	b.syncGateway()
	if f.fwd {
		t.Fatal("after a crash: forwarding left on")
	}
}

func TestGatewayView(t *testing.T) {
	on := settings.Defaults.Clone()
	on.Gateway, on.Tun, on.Paused = true, true, false
	good := gwProbe{at: time.Now(), net: gateway.Network{IP: "192.168.1.20"}, fwd: true, dns: true, devices: 2}
	for _, tc := range []struct {
		name   string
		s      settings.Settings
		on     bool
		tun    string
		ready  bool
		pr     gwProbe
		status string
	}{
		{"off", settings.Defaults, false, "off", false, gwProbe{}, "off"},
		{"paused", on, false, "off", true, good, "paused"},
		{"needs authorization", on, true, "install", false, gwProbe{}, "install"},
		{"tun down", on, true, "error", true, good, "error"},
		{"not looked", on, true, "on", true, gwProbe{}, "checking"},
		{"no lan", on, true, "on", true, gwProbe{at: time.Now(), fwd: true}, "error"},
		{"no forwarding", on, true, "on", true, gwProbe{at: time.Now(), net: good.net, dns: true}, "error"},
		{"no dns", on, true, "on", true, gwProbe{at: time.Now(), net: good.net, fwd: true}, "error"},
		{"on", on, true, "on", true, good, "on"},
	} {
		g := gatewayView(tc.s, tc.on, tc.tun, tc.ready, true, tc.pr, nil)
		if g.Status != tc.status {
			t.Errorf("%s: status %q, want %q (%s)", tc.name, g.Status, tc.status, g.Detail)
		}
		if g.Status == "error" && g.Detail == "" {
			t.Errorf("%s: an error without a word", tc.name)
		}
	}
	if g := gatewayView(on, true, "on", true, true, good, nil); g.Devices != 2 || g.IP != "192.168.1.20" || !g.Tun || !g.Awake {
		t.Errorf("on: %+v", g)
	}
}

// Turning TUN off puts back what turning it on changed outside the config:
// the core it ran is the bundled one again, and what TUN left on the system
// is taken back.
func TestTunOffPutsTheSystemBack(t *testing.T) {
	installed, dialogs := true, 0
	fakeTun(t, &installed, &dialogs)
	resets := 0
	saved := osTunReset
	osTunReset = func() error { resets++; return nil }
	t.Cleanup(func() { osTunReset = saved })

	a := gatewayApp(t, &gwFake{})
	a.core.SetBin(a.tunBin)
	a.tunOff()
	if resets != 1 {
		t.Fatalf("the TUN leftovers were taken back %d times, want 1", resets)
	}
	if got := a.core.Bin; got != a.coreBin {
		t.Fatalf("the core after TUN is %q, want the bundled %q", got, a.coreBin)
	}
	if gate := a.settings.Get().Tun; gate {
		t.Fatal("TUN was turned on by turning it off")
	}
}
