package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	for in, want := range map[string]Kind{
		"https://sub.example.com/api/v1/client/subscribe?token=x": Subscription,
		"  http://a.example/s  ":                                  Subscription,
		"vmess://eyJhZGQiOiIxLjIuMy40In0=":                        Link,
		"trojan://pw@h:443#a\nss://YWVz@h:1#b":                    Link,
		"hysteria2://pw@h:443?sni=x#LA":                           Link,
		"proxies:\n  - {name: a, type: direct}":                   File,
	} {
		got, err := Detect(in)
		if err != nil || got != want {
			t.Errorf("Detect(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "hello", "ftp://x"} {
		if _, err := Detect(in); err == nil {
			t.Errorf("Detect(%q) should fail", in)
		}
	}
}

func TestLooksLikeNodes(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("trojan://pw@h:443#a\nss://x@h:1#b"))
	for _, ok := range []string{"proxies:\n - {name: a, type: direct}", "vmess://abc", b64} {
		if err := looksLikeNodes([]byte(ok)); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "<!DOCTYPE html><html>", "just text"} {
		if err := looksLikeNodes([]byte(bad)); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestSubscriptionLifecycle(t *testing.T) {
	body := "proxies:\n  - {name: a, type: direct}\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("no User-Agent")
		}
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=10; expire=1798732800")
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''%E6%9C%BA%E5%9C%BA%20A")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Add(context.Background(), srv.Client(), srv.URL+"/sub?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "机场 A" || p.Kind != Subscription || p.Usage == nil || p.Usage.Total != 10 || !p.Enabled || p.AutoUpdate != 12 {
		t.Fatalf("added %+v", p)
	}
	if b, _ := os.ReadFile(s.Path(p.ID)); string(b) != body {
		t.Fatalf("content %q", b)
	}
	body = "<html>error</html>"
	if _, err := s.Update(context.Background(), srv.Client(), p.ID); err == nil {
		t.Fatal("update with an error page should fail")
	}
	if got, _ := s.Get(p.ID); got.Error == "" {
		t.Fatal("error not recorded")
	}
	// The old content survives a failed update.
	if b, _ := os.ReadFile(s.Path(p.ID)); string(b) == body {
		t.Fatal("content replaced by the error page")
	}
	s2, _ := Open(dir)
	if len(s2.List()) != 1 {
		t.Fatal("index not persisted")
	}
	if err := s.Delete(p.ID); err != nil || len(s.List()) != 0 {
		t.Fatal("delete")
	}
}

func TestLinkName(t *testing.T) {
	if got := linkName("trojan://pw@h:443#%E6%B4%9B%E6%9D%89%E7%9F%B6"); got != "洛杉矶" {
		t.Errorf("got %q", got)
	}
	if got := linkName("vless://x@h:1\nss://y@h:2"); got != "vless 节点 等 2 个节点" {
		t.Errorf("got %q", got)
	}
}

func TestFromQR(t *testing.T) {
	sub := "https://sub.example.com/s?token=x"
	for _, c := range []struct {
		in   []string
		want string
	}{
		{[]string{sub}, sub},
		{[]string{"clash://install-config?url=" + url.QueryEscape(sub) + "&name=x"}, sub},
		{[]string{"sub://" + base64.StdEncoding.EncodeToString([]byte(sub)) + "#name"}, sub},
		{[]string{"trojan://a@h:1#A", "ss://b@h:2#B", "trojan://a@h:1#A"}, "trojan://a@h:1#A\nss://b@h:2#B"},
		{[]string{sub, "vmess://abc"}, "vmess://abc"},
	} {
		got, err := FromQR(c.in)
		if err != nil || got != c.want {
			t.Errorf("FromQR(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := FromQR([]string{"WIFI:S:home;T:WPA;P:pw;;"}); err == nil {
		t.Error("a Wi-Fi code should not be taken")
	}
}

func TestShare(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	links := "trojan://a@h:1#A\nss://b@h:2#B"
	p, err := s.Add(context.Background(), http.DefaultClient, links)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Share(p.ID); err != nil || got != links {
		t.Errorf("Share = %q, %v", got, err)
	}
	f, _ := s.Add(context.Background(), http.DefaultClient, "proxies:\n  - {name: a, type: direct}")
	if _, err := s.Share(f.ID); err == nil {
		t.Error("a file has nothing to share")
	}
}

func TestReloadLocalFile(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "nodes.yaml")
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	content := func(id string) string {
		t.Helper()
		b, err := s.Content(id)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	write(src, "proxies:\n  - {name: A, type: direct}\n")
	p, err := s.AddFile(src)
	if err != nil {
		t.Fatal(err)
	}
	const two = "proxies:\n  - {name: A, type: direct}\n  - {name: B, type: direct}\n"
	write(src, two)
	if p, err = s.Reload(p.ID, ""); err != nil || content(p.ID) != two || p.Error != "" {
		t.Fatalf("reload: %v, %+v", err, p)
	}

	write(src, "<html>not nodes</html>")
	if p, err = s.Reload(p.ID, ""); err == nil || p.Error == "" || content(p.ID) != two {
		t.Errorf("bad content: %v, %+v", err, p)
	}
	write(src, "proxies:\n  - {name: C, type: direct}\n")
	s.Vet = func(string, []byte) error { return errors.New("no") }
	if _, err = s.Reload(p.ID, ""); err == nil || content(p.ID) != two {
		t.Errorf("vetoed: %v", err)
	}
	s.Vet = nil

	// Another file, chosen again: it is the one remembered from then on.
	other := filepath.Join(dir, "other.yaml")
	write(other, "proxies:\n  - {name: D, type: direct}\n")
	if p, err = s.Reload(p.ID, other); err != nil || p.Source != other || !strings.Contains(content(p.ID), "name: D") {
		t.Errorf("another file: %v, %+v", err, p)
	}

	// Without a file to go back to there is nothing to read.
	if _, err := s.update(p.ID, func(p *Provider) { p.Source = "" }); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reload(p.ID, ""); err == nil {
		t.Error("read a file nobody named")
	}
	link, err := s.Add(context.Background(), nil, "trojan://x@h:1#n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reload(link.ID, src); err != nil || strings.Contains(content(link.ID), "proxies") {
		t.Errorf("links were replaced: %v", err)
	}
}
