package core

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oboo/moho/internal/devnet"
	"github.com/oboo/moho/internal/routes"
	"gopkg.in/yaml.v3"
)

func directDefault(users []routes.UserRule) []routes.Route {
	var p routes.Plan
	p.ChangePolicy(routes.Default, func(q *routes.Policy) { q.Direct = true })
	return routes.Build(p, users)
}

// TestDefaultGoesDirect: what no rule matched goes out DIRECT, the rules
// keep their groups, and the default route's group stays for global mode.
func TestDefaultGoesDirect(t *testing.T) {
	users := []routes.UserRule{{Kind: routes.KindSuffix, Value: "corp.example", Route: routes.Default}}
	out, err := BuildConfig(Input{Routes: directDefault(users), UserRules: users, Mode: "rule", MixedPort: 7899, ProbePort: 7900, TestURL: "x"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"- MATCH,DIRECT", "- DOMAIN-SUFFIX,corp.example,DIRECT", "- DOMAIN-SUFFIX,openai.com,AI 服务", "name: 默认", "name: 默认·自动"} {
		if !strings.Contains(s, want) {
			t.Errorf("config lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "MATCH,默认") {
		t.Errorf("what no rule matched still goes by the group:\n%s", s)
	}
}

// goesBy runs the real core on routes all and asks it for each host of
// want, which is to go the way the core's log then says.
func goesBy(t *testing.T, all []routes.Route, users []routes.UserRule, want map[string]string) {
	t.Helper()
	bin, geo := bundledMihomo(t)
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok "+r.Host) }))
	defer web.Close()
	_, webPort, _ := net.SplitHostPort(web.Listener.Addr().String())
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	probe, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	in := devInput("rule", devnet.Overlay{})
	in.Routes, in.UserRules = all, users
	in.MixedPort, in.ProbePort, in.FindProcess = port, probe, "off"
	in.Options.NameserverPolicy = []string{"+.bowel-test.dev=" + answerDNS(t)}
	b, err := BuildConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	dir := coreDir(t, geo)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log lockedBuf
	cmd := exec.CommandContext(ctx, bin, "-d", dir, "-f", path)
	cmd.Stdout, cmd.Stderr = &log, &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	defer cancel()
	up := false
	for i := 0; i < 100 && !up; i++ {
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond); err == nil {
			c.Close()
			up = true
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !up {
		t.Fatalf("the core did not start:\n%s", log.String())
	}
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}}
	for host, via := range want {
		// The core's port opens a moment before its resolver is ready.
		var res *http.Response
		var body []byte
		for i := 0; i < 20; i++ {
			if res, err = client.Get("http://" + host + ":" + webPort + "/"); err == nil {
				body, _ = io.ReadAll(res.Body)
				res.Body.Close()
				if res.StatusCode == 200 {
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		if err != nil {
			t.Errorf("%s: %v\n%s", host, err, log.String())
			continue
		}
		if res.StatusCode != 200 || !strings.HasPrefix(string(body), "ok ") {
			t.Errorf("%s: %d %s\n%s", host, res.StatusCode, body, log.String())
		}
		went := false // the core writes its log a moment after it answers
		for i := 0; i < 40 && !went; i++ {
			went = slices.ContainsFunc(strings.Split(log.String(), "\n"), func(l string) bool {
				return strings.Contains(l, host+":"+webPort) && strings.Contains(l, via)
			})
			time.Sleep(50 * time.Millisecond)
		}
		if !went {
			t.Errorf("%s did not go %q:\n%s", host, via, log.String())
		}
	}
}

// Through the real core: a name no rule matches leaves DIRECT, while one a
// rule matches still leaves by its group.
func TestDefaultGoesDirectRuns(t *testing.T) {
	users := []routes.UserRule{{Kind: routes.KindSuffix, Value: "hit.bowel-test.dev", Route: "ai"}}
	goesBy(t, directDefault(users), users, map[string]string{
		"miss.bowel-test.dev": "match Match using DIRECT", "api.hit.bowel-test.dev": "using AI 服务"})
}

func directGroup(users []routes.UserRule) []routes.Route {
	var p routes.Plan
	p.SetStrategy("ai", routes.ModeOrder, "")
	p.ChangePolicy("ai", func(q *routes.Policy) { q.Direct = true })
	return routes.Build(p, users)
}

// TestGroupGoesDirect: a route group that goes direct keeps its rules and
// its name in the core, as a group holding DIRECT alone, and none of its
// node groups. The other groups, and what no rule matched, go as before.
func TestGroupGoesDirect(t *testing.T) {
	users := []routes.UserRule{{Kind: routes.KindSuffix, Value: "corp.example", Route: "ai"}}
	out, err := BuildConfig(Input{Routes: directGroup(users), UserRules: users, Mode: "rule", MixedPort: 7899, ProbePort: 7900, TestURL: "x",
		Orders: map[string][]string{}})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Groups []struct {
			Name, Type string
			Proxies    []string
		} `yaml:"proxy-groups"`
		Rules []string
	}
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range cfg.Groups {
		if strings.HasPrefix(g.Name, "AI 服务·") {
			t.Errorf("a node group of a group that goes direct: %+v", g)
		}
		if g.Name == "AI 服务" {
			found = true
			if g.Type != "select" || !slices.Equal(g.Proxies, []string{"DIRECT"}) {
				t.Errorf("its group: %+v", g)
			}
		}
	}
	if !found {
		t.Errorf("no group of its own: %+v", cfg.Groups)
	}
	for _, want := range []string{"DOMAIN-SUFFIX,corp.example,AI 服务", "DOMAIN-SUFFIX,openai.com,AI 服务", "DOMAIN-SUFFIX,github.com,开发", "MATCH,默认"} {
		if !slices.Contains(cfg.Rules, want) {
			t.Errorf("rules lack %q:\n%s", want, strings.Join(cfg.Rules, "\n"))
		}
	}
}

// Through the real core: what the group's rules match leaves DIRECT, by
// the group's name, and the rest still goes by the default route's group.
func TestGroupGoesDirectRuns(t *testing.T) {
	users := []routes.UserRule{{Kind: routes.KindSuffix, Value: "hit.bowel-test.dev", Route: "ai"}}
	goesBy(t, directGroup(users), users, map[string]string{
		"api.hit.bowel-test.dev": "using AI 服务[DIRECT]", "miss.bowel-test.dev": "match Match using 默认"})
}
