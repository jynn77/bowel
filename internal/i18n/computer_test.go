package i18n

import "testing"

func TestComputer(t *testing.T) {
	for _, c := range []struct {
		l        int32
		in, want string
	}{
		{zh, "这台 Mac 没有连上局域网", "这台电脑没有连上局域网"},
		{zh, "Mac 睡眠、关机或 bowel 退出后", "电脑睡眠、关机或 bowel 退出后"},
		{zh, "IPv6 流量不经过这台 Mac。", "IPv6 流量不经过这台电脑。"},
		{hant, "這台 Mac 上的網路", "這台電腦上的網路"},
		{en, "This Mac isn't on a LAN", "This computer isn't on a LAN"},
		{en, "bowel keeps the Mac from sleeping", "bowel keeps the computer from sleeping"},
		{en, "reserve it for this Mac's address", "reserve it for this computer's address"},
		{ja, "この Mac のネットワーク", "このパソコンのネットワーク"},
		{ja, "このMac", "このパソコン"},
		// Not the word: the system's name, an address, another word.
		{en, "macOS asks for an authorization", "macOS asks for an authorization"},
		{zh, "macOS 防火墙开着", "macOS 防火墙开着"},
		{en, "The MAC address of the Machine", "The MAC address of the Machine"},
		{en, "MagicDNS", "MagicDNS"},
	} {
		if got := computer(c.in, c.l); got != c.want {
			t.Errorf("%q → %q, want %q", c.in, got, c.want)
		}
		// From the cache, the same.
		if got := computer(c.in, c.l); got != c.want {
			t.Errorf("%q again → %q", c.in, got)
		}
	}
}

// On a Mac nothing is rewritten; elsewhere T's strings are.
func TestTNamesTheComputer(t *testing.T) {
	saved, savedLang := notMac, Lang()
	defer func() { notMac = saved; Set(savedLang) }()
	Set("en")
	notMac = false
	if s := T("这台 Mac 上的网络", "Networks on this Mac"); s != "Networks on this Mac" {
		t.Errorf("on a Mac: %q", s)
	}
	notMac = true
	if s := T("这台 Mac 上的网络", "Networks on this Mac"); s != "Networks on this computer" {
		t.Errorf("elsewhere: %q", s)
	}
	Set("zh")
	if s := T("这台 Mac 上的网络", "Networks on this Mac"); s != "这台电脑上的网络" {
		t.Errorf("elsewhere, in Chinese: %q", s)
	}
}
