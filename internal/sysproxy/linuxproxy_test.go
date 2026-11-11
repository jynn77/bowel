package sysproxy

import (
	"reflect"
	"slices"
	"testing"
)

const gsettingsOut = `org.gnome.system.proxy autoconfig-url 'http://wpad.corp/proxy.pac'
org.gnome.system.proxy ignore-hosts ['localhost', '127.0.0.0/8', '::1', 'it\'s.example']
org.gnome.system.proxy mode 'manual'
org.gnome.system.proxy use-same-proxy true
org.gnome.system.proxy.ftp host ''
org.gnome.system.proxy.ftp port 0
org.gnome.system.proxy.http authentication-password ''
org.gnome.system.proxy.http enabled false
org.gnome.system.proxy.http host '127.0.0.1'
org.gnome.system.proxy.http port 7890
org.gnome.system.proxy.https host "proxy.corp"
org.gnome.system.proxy.https port 8443
org.gnome.system.proxy.socks host ''
org.gnome.system.proxy.socks port 1080
`

func TestParseGSettings(t *testing.T) {
	sv := parseGSettings(gsettingsOut).service()
	want := Service{
		Name: gnomeName, PAC: "http://wpad.corp/proxy.pac",
		Web:    Endpoint{On: true, Host: "127.0.0.1", Port: 7890},
		Secure: Endpoint{On: true, Host: "proxy.corp", Port: 8443},
		Bypass: []string{"localhost", "127.0.0.0/8", "::1", "it's.example"},
	}
	if !reflect.DeepEqual(sv, want) {
		t.Errorf("service:\n got %+v\nwant %+v", sv, want)
	}
	// Off keeps the addresses; an empty list has its own spelling.
	off := parseGSettings("org.gnome.system.proxy mode 'none'\norg.gnome.system.proxy ignore-hosts @as []\norg.gnome.system.proxy.http host 'a'\norg.gnome.system.proxy.http port 1\n").service()
	if off.Web != (Endpoint{Host: "a", Port: 1}) || off.Bypass != nil || off.PACOn {
		t.Errorf("off: %+v", off)
	}
	if auto := parseGSettings("org.gnome.system.proxy mode 'auto'\norg.gnome.system.proxy autoconfig-url 'u'\n").service(); !auto.PACOn || auto.PAC != "u" {
		t.Errorf("auto: %+v", auto)
	}
}

// What is written reads back as the same service, the switch last.
func TestGnomeSets(t *testing.T) {
	sv := pointed(Service{Name: gnomeName, PAC: "http://p/x.pac", PACOn: true}, "127.0.0.1", 7890, []string{"*.local", "169.254/16", "it's"})
	sets := gnomeSets(sv)
	if last := sets[len(sets)-1]; !slices.Equal(last, []string{gnomeSchema, "mode", "'manual'"}) {
		t.Errorf("last set: %q", last)
	}
	text := ""
	for _, s := range sets {
		text += s[0] + " " + s[1] + " " + s[2] + "\n"
	}
	if got := parseGSettings(text).service(); !reflect.DeepEqual(got, sv) {
		t.Errorf("read back:\n got %+v\nwant %+v\n%s", got, sv, text)
	}
	if !slices.Equal(sv.Bypass, []string{"*.local", "169.254.0.0/16", "it's"}) || sv.PACOn {
		t.Errorf("pointed: %+v", sv)
	}
	mode := func(sv Service) string { s := gnomeSets(sv); return s[len(s)-1][2] }
	if m := mode(off(sv)); m != "'none'" {
		t.Errorf("off: mode %s", m)
	}
	if m := mode(Service{PAC: "u", PACOn: true}); m != "'auto'" {
		t.Errorf("a script: mode %s", m)
	}
	if m := mode(Service{PACOn: true}); m != "'none'" {
		t.Errorf("a script with no address: mode %s", m)
	}
	if l := gvList(nil); l != "@as []" {
		t.Errorf("empty list: %s", l)
	}
}

const kioslaverc = `[$Version]
update_info=kioslave.upd:kde2.2/r1

[Proxy Settings]
NoProxyFor=localhost,127.0.0.1, *.corp
Proxy Config Script=
ProxyType=1
ReversedException=false
httpProxy[$e]=http://127.0.0.1 7890
httpsProxy=http://proxy.corp:8443
socksProxy=socks://[::1] 1080

[Other]
httpProxy=http://wrong 1
`

func TestParseKioslaverc(t *testing.T) {
	sv := parseKioslaverc(kioslaverc).service()
	want := Service{
		Name:   kdeName,
		Web:    Endpoint{On: true, Host: "127.0.0.1", Port: 7890},
		Secure: Endpoint{On: true, Host: "proxy.corp", Port: 8443},
		Socks:  Endpoint{On: true, Host: "::1", Port: 1080},
		Bypass: []string{"localhost", "127.0.0.1", "*.corp"},
	}
	if !reflect.DeepEqual(sv, want) {
		t.Errorf("service:\n got %+v\nwant %+v", sv, want)
	}
	if none := parseKioslaverc("").service(); none.Web.On || none.PACOn || none.Bypass != nil {
		t.Errorf("no file: %+v", none)
	}
	if pac := parseKioslaverc("[Proxy Settings]\nProxyType=2\nProxy Config Script=http://p/x.pac\nhttpProxy=http://a 1\n").service(); !pac.PACOn || pac.Web.On || pac.Web.Host != "a" {
		t.Errorf("a script: %+v", pac)
	}
	if wpad := parseKioslaverc("[Proxy Settings]\nProxyType=3\n").service(); !wpad.AutoDetect {
		t.Errorf("WPAD: %+v", wpad)
	}
}

func TestKdeSets(t *testing.T) {
	sv := pointed(Service{Name: kdeName, PAC: "http://p/x.pac", PACOn: true}, "127.0.0.1", 7890, []string{"*.local", "10/8"})
	sets := kdeSets(sv)
	if last := sets[len(sets)-1]; last != [2]string{"ProxyType", "1"} {
		t.Errorf("last set: %q", last)
	}
	text := "[" + kdeGroup + "]\n"
	for _, kv := range sets {
		text += kv[0] + "=" + kv[1] + "\n"
	}
	if got := parseKioslaverc(text).service(); !reflect.DeepEqual(got, sv) {
		t.Errorf("read back:\n got %+v\nwant %+v\n%s", got, sv, text)
	}
	if !slices.Contains(sets, [2]string{"httpProxy", "http://127.0.0.1 7890"}) || !slices.Contains(sets, [2]string{"socksProxy", "socks://127.0.0.1 7890"}) || !slices.Contains(sets, [2]string{"NoProxyFor", "*.local,10.0.0.0/8"}) {
		t.Errorf("sets: %q", sets)
	}
	kind := func(sv Service) string { s := kdeSets(sv); return s[len(s)-1][1] }
	if k := kind(off(sv)); k != "0" {
		t.Errorf("off: ProxyType %s", k)
	}
	if k := kind(Service{PAC: "u", PACOn: true}); k != "2" {
		t.Errorf("a script: ProxyType %s", k)
	}
	if k := kind(Service{AutoDetect: true}); k != "3" {
		t.Errorf("WPAD: ProxyType %s", k)
	}
}

// Ours takes HTTP and HTTPS at bowel's port, and no script before them.
func TestServiceStatus(t *testing.T) {
	sv := pointed(Service{}, "127.0.0.1", 7890, nil)
	if s := serviceStatus(sv, 7890); !s.On || !s.Ours || s.Port != 7890 {
		t.Errorf("pointed: %+v", s)
	}
	if s := serviceStatus(sv, 7891); !s.On || s.Ours {
		t.Errorf("another port: %+v", s)
	}
	if s := serviceStatus(off(sv), 7890); s.On || s.Ours {
		t.Errorf("off: %+v", s)
	}
	sv.PACOn = true
	if s := serviceStatus(sv, 7890); s.Ours {
		t.Errorf("under a script: %+v", s)
	}
	other := Service{Web: Endpoint{On: true, Host: "proxy.corp", Port: 8080}}
	if s := serviceStatus(other, 7890); !s.On || s.Ours || s.Host != "proxy.corp" {
		t.Errorf("another proxy: %+v", s)
	}
}

func TestLinuxBypass(t *testing.T) {
	got := linuxBypass([]string{" *.local ", "", "169.254/16", "10.0.0.0/8", "192.168.1.7/24", "fd00::/8", "localhost", "a/b"})
	want := []string{"*.local", "169.254.0.0/16", "10.0.0.0/8", "192.168.1.0/24", "fd00::/8", "localhost", "a/b"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
