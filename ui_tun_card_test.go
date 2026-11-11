package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/settings"
)

// tunCard is the takeover detail's texts with these settings.
func tunCard(t *testing.T, set func(*settings.Settings)) *ui.Tester {
	t.Helper()
	u := fixtureUI()
	u.snap.Settings = settings.Defaults.Clone()
	u.snap.Settings.Tun = true
	set(&u.snap.Settings)
	u.snap.Takeover.Tun = Entry{Wanted: true, Status: "on"}
	u.home.x = "takeover"
	tt := ui.NewTesterWith(u.view, 1060, 760, ui.TesterOptions{})
	tt.Advance(2 * time.Second)
	return tt
}

// TestTunCardStack: the TUN card's stack row is the stack in the settings,
// not always mixed (#57).
func TestTunCardStack(t *testing.T) {
	want := map[string]string{
		"mixed":  "mixed（TCP 用 system，UDP 用 gVisor）",
		"system": "system（系统协议栈）",
		"gvisor": "gVisor（用户态协议栈）",
	}
	for _, stack := range core.TunStacks {
		tt := tunCard(t, func(s *settings.Settings) { s.TunStack = stack })
		if want[stack] == "" || !tt.HasText(want[stack]) {
			t.Errorf("%s: no %q: %q", stack, want[stack], tt.Texts())
		}
		for other, text := range want {
			if other != stack && tt.HasText(text) {
				t.Errorf("%s: shows %q", stack, text)
			}
		}
		shot(t, tt, "tun-card-"+stack)
	}
}

// TestTunCardDNS: the DNS row follows the hijack list and the DNS mode;
// gateway mode is fake-ip whatever the mode says, as the core's config is.
func TestTunCardDNS(t *testing.T) {
	for _, c := range []struct {
		name    string
		mode    string
		hijack  []string
		gateway bool
		want    string
	}{
		{"default", "fake-ip", []string{"any:53", "tcp://any:53"}, false, "劫持 53 端口 · fake-ip"},
		{"redir-host", "redir-host", []string{"any:53", "tcp://any:53"}, false, "劫持 53 端口 · redir-host"},
		{"gateway", "redir-host", []string{"any:53"}, true, "劫持 53 端口 · fake-ip"},
		{"custom", "fake-ip", []string{"198.18.0.2:53", "any:5353"}, false, "劫持 198.18.0.2:53, any:5353 · fake-ip"},
		{"none", "redir-host", nil, false, "不劫持 · redir-host"},
	} {
		tt := tunCard(t, func(s *settings.Settings) { s.DNSMode, s.DNSHijack, s.Gateway = c.mode, c.hijack, c.gateway })
		if !tt.HasText(c.want) {
			t.Errorf("%s: no %q: %q", c.name, c.want, tt.Texts())
		}
	}
	for _, m := range core.DNSModes {
		if got := tunDNSText(settings.Settings{Options: core.Options{DNSMode: m, DNSHijack: []string{"any:53"}}}); got != "劫持 53 端口 · "+m {
			t.Errorf("%s: %q", m, got)
		}
	}
	// The other languages have the rows too.
	defer i18n.Set(i18n.Lang())
	i18n.Set("en")
	if got := tunStackText("system"); got != "system (the system's own stack)" {
		t.Errorf("en: %q", got)
	}
}
