package sysproxy

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestParseWinServer(t *testing.T) {
	for _, c := range []struct {
		in                 string
		web, secure, socks Endpoint
	}{
		{"", Endpoint{}, Endpoint{}, Endpoint{}},
		{"127.0.0.1:7899", Endpoint{Host: "127.0.0.1", Port: 7899}, Endpoint{Host: "127.0.0.1", Port: 7899}, Endpoint{}},
		{"http=proxy.corp:8080;https=proxy.corp:8443;socks=127.0.0.1:1080",
			Endpoint{Host: "proxy.corp", Port: 8080}, Endpoint{Host: "proxy.corp", Port: 8443}, Endpoint{Host: "127.0.0.1", Port: 1080}},
		{"http://127.0.0.1:7890", Endpoint{Host: "127.0.0.1", Port: 7890}, Endpoint{Host: "127.0.0.1", Port: 7890}, Endpoint{}},
		{"HTTP=a:1; socks=b", Endpoint{Host: "a", Port: 1}, Endpoint{}, Endpoint{Host: "b", Port: 1080}},
		{"[::1]:7899", Endpoint{Host: "::1", Port: 7899}, Endpoint{Host: "::1", Port: 7899}, Endpoint{}},
		{"proxy.corp", Endpoint{Host: "proxy.corp", Port: 80}, Endpoint{Host: "proxy.corp", Port: 80}, Endpoint{}},
		{"https=s:443;d:3128", Endpoint{Host: "d", Port: 3128}, Endpoint{Host: "s", Port: 443}, Endpoint{}},
	} {
		web, secure, socks := parseWinServer(c.in)
		if web != c.web || secure != c.secure || socks != c.socks {
			t.Errorf("parseWinServer(%q) = %+v %+v %+v, want %+v %+v %+v", c.in, web, secure, socks, c.web, c.secure, c.socks)
		}
	}
}

func TestWinServer(t *testing.T) {
	e := func(h string, p int) Endpoint { return Endpoint{Host: h, Port: p} }
	for _, c := range []struct {
		web, secure, socks Endpoint
		want               string
	}{
		{e("127.0.0.1", 7899), e("127.0.0.1", 7899), Endpoint{}, "127.0.0.1:7899"},
		{e("::1", 7899), e("::1", 7899), Endpoint{}, "[::1]:7899"},
		{e("a", 1), e("b", 2), e("c", 3), "http=a:1;https=b:2;socks=c:3"},
		{e("a", 1), e("a", 1), e("c", 3), "http=a:1;https=a:1;socks=c:3"},
		{Endpoint{}, Endpoint{}, Endpoint{}, ""},
	} {
		if got := winServer(c.web, c.secure, c.socks); got != c.want {
			t.Errorf("winServer = %q, want %q", got, c.want)
		}
	}
}

func TestWinBypass(t *testing.T) {
	got := winBypass([]string{"localhost", "127.0.0.1", "*.local", "169.254/16", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fd00::/8", "timestamp.apple.com", "10.1.2.3/32"})
	want := "localhost;127.0.0.1;*.local;169.254.*;10.*;" +
		"172.16.*;172.17.*;172.18.*;172.19.*;172.20.*;172.21.*;172.22.*;172.23.*;" +
		"172.24.*;172.25.*;172.26.*;172.27.*;172.28.*;172.29.*;172.30.*;172.31.*;" +
		"192.168.*;timestamp.apple.com;10.1.2.3;<local>"
	if got != want {
		t.Errorf("winBypass =\n%s\nwant\n%s", got, want)
	}
	if got := winBypass([]string{"<local>", "10.0.0.0/8", "10.0.0.0/8"}); got != "<local>;10.*" {
		t.Errorf("winBypass with duplicates = %q", got)
	}
	if got := wildcards("10.0.0.0/30"); !slices.Equal(got, []string{"10.0.0.0", "10.0.0.1", "10.0.0.2", "10.0.0.3"}) {
		t.Errorf("wildcards /30 = %v", got)
	}
	if got := wildcards("0.0.0.0/0"); !slices.Equal(got, []string{"*"}) {
		t.Errorf("wildcards /0 = %v", got)
	}
}

// An IPv6 address in ProxyOverride makes InternetSetOption reject the whole
// call, so none may reach it; everything else is written as before.
func TestWinBypassIPv6(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []string
		want string
	}{
		{"loopback", []string{"::1"}, "<local>"},
		{"bracketed", []string{"[::1]"}, "<local>"},
		{"bracketed with port", []string{"[::1]:8080"}, "<local>"},
		{"full address", []string{"2001:db8::1"}, "<local>"},
		{"wildcard", []string{"fe80::*"}, "<local>"},
		{"surrounded by spaces", []string{" ::1 "}, "<local>"},
		{"network", []string{"fd00::/8"}, "<local>"},
		{"host and port kept", []string{"example.com:8080"}, "example.com:8080;<local>"},
		{"IPv4 and port kept", []string{"10.0.0.1:8080"}, "10.0.0.1:8080;<local>"},
		{"IPv4 kept", []string{"127.0.0.1"}, "127.0.0.1;<local>"},
		{"IPv4 wildcard kept", []string{"192.168.*"}, "192.168.*;<local>"},
		{"name wildcard kept", []string{"*.local"}, "*.local;<local>"},
		{"the devnet loopback literals", []string{"localhost", "127.0.0.1", "::1"}, "localhost;127.0.0.1;<local>"},
	} {
		if got := winBypass(c.in); got != c.want {
			t.Errorf("%s: winBypass(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestWinServiceRoundTrip(t *testing.T) {
	for _, ws := range []winSettings{
		{flags: winDirect, server: "", override: "", pac: ""},
		{flags: winDirect | winProxy, server: "proxy.corp:8080", override: "*.corp;<local>"},
		{flags: winDirect | winPAC | winAutoDetect, server: "proxy.corp:8080", override: "<local>", pac: "http://wpad/proxy.pac"},
		{flags: winDirect | winProxy, server: "http=a:1;https=b:2;socks=c:3"},
		{flags: winDirect, server: "a:1", pac: "http://kept-while-off/p.pac"}, // off, but kept
	} {
		sv := winService(ws)
		if got := sv.winSettings(); got != ws {
			t.Errorf("round trip of %+v gave %+v (via %+v)", ws, got, sv)
		}
	}
}

func TestWinServiceForget(t *testing.T) {
	// A snapshot taken while Moho held the proxy restores to none, and
	// keeps the PAC script the user had.
	saved := Saved{Services: []Service{winService(winSettings{
		flags: winDirect | winProxy, server: "127.0.0.1:7899", override: "localhost;<local>", pac: "http://wpad/p.pac",
	})}}
	saved.Forget(7899)
	got := saved.Services[0].winSettings()
	want := winSettings{flags: winDirect, override: strings.Join(DefaultBypass, ";"), pac: "http://wpad/p.pac"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestWinServiceTurnOffDead(t *testing.T) {
	// The address and bypass list stay; only the switch goes off.
	saved := Saved{Services: []Service{winService(winSettings{flags: winDirect | winProxy, server: "127.0.0.1:7890", override: "localhost;<local>"})}}
	saved.TurnOffDead(func(string, int) bool { return false })
	got := saved.Services[0].winSettings()
	want := winSettings{flags: winDirect, server: "127.0.0.1:7890", override: "localhost;<local>"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestWinStatus(t *testing.T) {
	ours := winSettings{flags: winDirect | winProxy, server: "127.0.0.1:7899"}
	if s := winStatus(ours, 7899); !s.On || !s.Ours || s.Host != "127.0.0.1" || s.Port != 7899 {
		t.Errorf("ours: %+v", s)
	}
	if s := winStatus(ours, 7890); !s.On || s.Ours {
		t.Errorf("another port: %+v", s)
	}
	off := ours
	off.flags = winDirect
	if s := winStatus(off, 7899); s.On || s.Ours {
		t.Errorf("switched off: %+v", s)
	}
	pac := ours
	pac.flags |= winPAC
	pac.pac = "http://wpad/p.pac"
	if s := winStatus(pac, 7899); !s.On || s.Ours {
		t.Errorf("behind a PAC script: %+v", s)
	}
	socksOnly := winSettings{flags: winDirect | winProxy, server: "socks=127.0.0.1:7899"}
	if s := winStatus(socksOnly, 7899); !s.On || s.Ours {
		t.Errorf("SOCKS only: %+v", s)
	}
}
