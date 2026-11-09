package provider

import (
	"encoding/base64"
	"os"
	"testing"
)

func TestParseServersYAML(t *testing.T) {
	got := parseServers([]byte("mixed-port: 7890\nproxies:\n  - {name: a, type: trojan, server: a.example, port: 443}\n  - {name: b, type: hysteria2, server: 1.2.3.4, port: '8443'}\n  - {name: c, type: ss}\n"))
	if got["a"] != "a.example" || got["b"] != "1.2.3.4" {
		t.Fatalf("got %v", got)
	}
	if _, ok := got["c"]; ok {
		t.Fatal("a node with no server was kept")
	}
}

func TestParseServersLinks(t *testing.T) {
	vmess := base64.StdEncoding.EncodeToString([]byte(`{"ps":"v","add":"v.example","port":443}`))
	vmessStr := base64.StdEncoding.EncodeToString([]byte(`{"ps":"v2","add":"v2.example","port":"8080"}`))
	ss := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pw@s.example:8388"))
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pw"))
	links := "vmess://" + vmess + "\nvmess://" + vmessStr + "\nss://" + ss + "#old\nss://" + userinfo + "@n.example:1234#new\n" +
		"trojan://pw@t.example:443?sni=x#T%20one\nhysteria2://pw@[2001:db8::1]:443/?insecure=1#six\nhy2://pw@h.example:443,20000-30000#hop\nnonsense\n"
	want := map[string]string{
		"v": "v.example", "v2": "v2.example", "old": "s.example", "new": "n.example",
		"T one": "t.example", "six": "2001:db8::1", "hop": "h.example",
	}
	for _, in := range []string{links, base64.StdEncoding.EncodeToString([]byte(links))} {
		got := parseServers([]byte(in))
		if len(got) != len(want) {
			t.Fatalf("got %v", got)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s = %q, want %q", k, got[k], v)
			}
		}
	}
}

// TestParseNames: nodes with no server of their own still have names.
// TestParseAddrs: the addresses keep the port the content gave.
func TestParseAddrs(t *testing.T) {
	got := parseAddrs([]byte("proxies:\n  - {name: a, type: trojan, server: a.example, port: 443}\n  - {name: b, type: ss, server: '2001:db8::1', port: '8443'}\n  - {name: c, type: ss}\n"))
	if got["a"] != "a.example:443" || got["b"] != "[2001:db8::1]:8443" {
		t.Errorf("yaml: %v", got)
	}
	if _, ok := got["c"]; ok {
		t.Error("a node with no server was kept")
	}

	vmess := base64.StdEncoding.EncodeToString([]byte(`{"ps":"v","add":"v.example","port":443}`))
	links := "trojan://pw@t.example:443?sni=x#T%20one\nhysteria2://pw@[2001:db8::1]:443/?insecure=1#six\n" +
		"hy2://pw@h.example:443,20000-30000#hop\nvmess://" + vmess + "\n"
	want := map[string]string{
		"T one": "t.example:443", "six": "[2001:db8::1]:443",
		"hop": "h.example:443,20000-30000", "v": "v.example:443",
	}
	got = parseAddrs([]byte(links))
	if len(got) != len(want) {
		t.Fatalf("links: %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

// TestStoreAddrs: the parse is cached by the file, and read again when the
// provider's file changes.
func TestStoreAddrs(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Path("p1")
	if err := os.WriteFile(p, []byte("proxies:\n  - {name: a, type: trojan, server: a.example, port: 443}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.Addrs("p1"); got["a"] != "a.example:443" {
		t.Fatalf("first: %v", got)
	}
	if err := os.WriteFile(p, []byte("proxies:\n  - {name: a, type: trojan, server: a.example, port: 8443}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.Addrs("p1"); got["a"] != "a.example:8443" {
		t.Errorf("after an update: %v", got)
	}
	if got := s.Addrs("nope"); got != nil {
		t.Errorf("a provider with no file: %v", got)
	}
}

func TestParseNames(t *testing.T) {
	got := parseNames([]byte("proxies:\n  - {name: a, type: direct}\n  - {name: b, type: trojan, server: b.example}\n"))
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("yaml: %q", got)
	}
	got = parseNames([]byte("trojan://pw@203.0.113.11:443#hk\n"))
	if len(got) != 1 || got[0] != "hk" {
		t.Errorf("links: %q", got)
	}
}
