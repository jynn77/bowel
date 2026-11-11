package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/devnet"
	"github.com/oboo/moho/internal/settings"
	"github.com/oboo/moho/internal/sysproxy"
)

// fakeIPv6 stands in for the network, and is every test's: none of them
// should reach the internet to find out about it.
type fakeIPv6 struct {
	mu     sync.Mutex
	state  devnet.IPv6State
	checks int
	resets int
	asked  [2]string
}

func (f *fakeIPv6) Check(primary, forced string) devnet.IPv6State {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	f.asked = [2]string{primary, forced}
	return f.state
}

func (f *fakeIPv6) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets++
}

func (f *fakeIPv6) set(s devnet.IPv6State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = s
}

func init() { ipv6Watch = &fakeIPv6{state: devnet.IPv6State{Reason: devnet.IPv6NoAddress}} }

var (
	ipv6Up   = devnet.IPv6State{Usable: true, Iface: "en0"}
	ipv6Down = devnet.IPv6State{Reason: devnet.IPv6NoAddress}
)

func TestReadIPv6AsksOnlyForAuto(t *testing.T) {
	saved := ipv6Watch
	defer func() { ipv6Watch = saved }()
	f := &fakeIPv6{state: ipv6Up}
	ipv6Watch = f
	s := settings.Defaults.Clone()
	s.IPv6, s.Interface = core.IPv6On, ""
	if got := readIPv6(s, "en1"); got.Known() || f.checks != 0 || f.resets != 1 {
		t.Errorf("on: %+v checks %d resets %d", got, f.checks, f.resets)
	}
	s.IPv6, s.Interface = core.IPv6Auto, "en5"
	if got := readIPv6(s, "en1"); got != ipv6Up || f.asked != [2]string{"en1", "en5"} {
		t.Errorf("auto: %+v asked %v", got, f.asked)
	}
}

func TestIPv6Note(t *testing.T) {
	for _, c := range []struct {
		m    core.IPv6Mode
		s    devnet.IPv6State
		want string
	}{
		{core.IPv6Off, ipv6Up, "只用 IPv4"},
		{core.IPv6On, ipv6Down, "总是"},
		{core.IPv6Auto, ipv6Up, "en0"},
		{core.IPv6Auto, ipv6Down, "没有公网"},
		{core.IPv6Auto, devnet.IPv6State{Reason: devnet.IPv6Unreachable}, "连不通"},
		{core.IPv6Auto, devnet.IPv6State{}, "真正有"},
	} {
		if got := ipv6Note(c.m, c.s); !strings.Contains(got, c.want) {
			t.Errorf("%s %+v: %q", c.m, c.s, got)
		}
	}
}

// The running core follows what Auto finds, is left alone while nothing
// changes, and doesn't care under the other modes.
func TestAutoIPv6FollowsTheNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("starts the core")
	}
	savedProxy := osProxy
	osProxy.enable = func(string, int, []string) error { return nil }
	osProxy.disable = func() error { return nil }
	osProxy.snapshot = func() (sysproxy.Saved, error) { return sysproxy.Saved{}, nil }
	osProxy.restore = func(context.Context, sysproxy.Saved) error { return nil }
	defer func() { osProxy = savedProxy }()
	savedDetect, savedWatch := devDetect, ipv6Watch
	devDetect = func(context.Context) devnet.Env { return devnet.Env{PrimaryIface: "en0"} }
	f := &fakeIPv6{state: ipv6Down}
	ipv6Watch = f
	defer func() { devDetect, ipv6Watch = savedDetect, savedWatch }()

	dir, err := os.MkdirTemp("", "mh")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	mygo.App.SetPath(mygo.PathUserData, dir)
	defer mygo.App.SetPath(mygo.PathUserData, "")
	if !exists(bundledCore()) {
		t.Skip("no core; run scripts/fetch-core.sh")
	}
	a, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	port, err := core.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.settings.Update(func(s *settings.Settings) { s.MixedPort = port }); err != nil {
		t.Fatal(err)
	}
	a.copyGeo()
	a.detectDev(time.Second)
	if err := a.applyConfig(); err != nil {
		t.Fatal(err)
	}
	defer a.shutdown()

	config := func() string {
		t.Helper()
		b, err := os.ReadFile(a.core.ConfigPath())
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if !strings.Contains(config(), "\nipv6: false\n") {
		t.Fatal("started with IPv6 on a network without it")
	}
	if got := a.ipv6Report(); got["mode"] != "auto" || got["enabled"] != false {
		t.Errorf("report: %v", got)
	}
	info := func() int64 {
		fi, err := os.Stat(a.core.ConfigPath())
		if err != nil {
			t.Fatal(err)
		}
		return fi.ModTime().UnixNano()
	}
	before := info()
	a.devSync()
	if info() != before {
		t.Error("reloaded with nothing changed")
	}

	f.set(ipv6Up)
	a.devSync()
	if !strings.Contains(config(), "\nipv6: true\n") {
		t.Error("the network got IPv6 and the core didn't")
	}
	if got := a.ipv6Report(); got["enabled"] != true {
		t.Errorf("report: %v", got)
	}
	c, err := a.core.Client()
	if err != nil {
		t.Fatal(err)
	}
	if v, err := c.Config(context.Background()); err != nil || !v.IPv6 {
		t.Errorf("the running core: %+v %v", v, err)
	}
	f.set(ipv6Down)
	a.devSync()
	if !strings.Contains(config(), "\nipv6: false\n") {
		t.Error("the network lost IPv6 and the core kept it")
	}
	if v, err := c.Config(context.Background()); err != nil || v.IPv6 {
		t.Errorf("the running core: %+v %v", v, err)
	}

	// On ignores the network.
	if err := a.updateSettings(func(s *settings.Settings) { s.IPv6 = core.IPv6On }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(config(), "\nipv6: true\n") {
		t.Error("On is not on")
	}
	before = info()
	a.devSync()
	if info() != before {
		t.Error("a network change reloaded On")
	}

	// Going back to Auto reads the network first, not 20 seconds later.
	f.set(ipv6Up)
	if err := a.updateSettings(func(s *settings.Settings) { s.IPv6 = core.IPv6Auto }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(config(), "\nipv6: true\n") {
		t.Error("Auto didn't read the network when it was chosen")
	}
}
