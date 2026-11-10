package ruleset

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// A cut of the list from the issue that asked for this, with what real
// lists carry.
const sampleYAML = `# AI Proxy Rules
payload:
  # Anthropic
  - DOMAIN-SUFFIX,anthropic.com
  - DOMAIN,codewhisperer.us-east-1.amazonaws.com
  - DOMAIN-KEYWORD,openai
  - DOMAIN-REGEX,^a,b$
  - IP-CIDR,160.79.104.0/21,no-resolve
  - IP-CIDR6,2607:6bc0::/32,no-resolve
  - IP-ASN,399358,no-resolve
  - PROCESS-NAME,Claude
`

func TestParseClassicalYAML(t *testing.T) {
	s, err := Parse([]byte(sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	if s.Behavior != Classical || len(s.Rules) != 8 || s.Skipped != 0 {
		t.Fatalf("%+v", s)
	}
	if s.Rules[3] != "DOMAIN-REGEX,^a,b$" {
		t.Errorf("a payload with a comma was cut: %q", s.Rules[3])
	}
	if s.Rules[4] != "IP-CIDR,160.79.104.0/21,no-resolve" {
		t.Errorf("no-resolve lost: %q", s.Rules[4])
	}
}

func TestParseTextDropsTargetsAndWhatTheCoreRefuses(t *testing.T) {
	s, err := Parse([]byte("# comment\n\n// another\ndomain-suffix, Example.COM, Proxy\nUSER-AGENT,Foo*\nURL-REGEX,^https://x\nMATCH,DIRECT\nRULE-SET,x,y\nIP-CIDR,not-an-ip\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Rules, []string{"DOMAIN-SUFFIX,example.com"}) || s.Skipped != 5 {
		t.Fatalf("%+v", s)
	}
}

func TestParseNameAndAddressLists(t *testing.T) {
	s, err := Parse([]byte(".surge.example\nplain.example\n"))
	if err != nil || s.Behavior != Domain || !slices.Equal(s.Rules, []string{"+.surge.example", "plain.example"}) {
		t.Errorf("text domains: %+v %v", s, err)
	}
	s, err = Parse([]byte("payload:\n  - '.sub.example'\n  - '+.all.example'\n"))
	if err != nil || s.Behavior != Domain || !slices.Equal(s.Rules, []string{".sub.example", "+.all.example"}) {
		t.Errorf("yaml keeps mihomo's meaning: %+v %v", s, err)
	}
	s, err = Parse([]byte("10.1.2.3/8\n1.1.1.1\n2606:4700::/32\n"))
	if err != nil || s.Behavior != IPCIDR || !slices.Equal(s.Rules, []string{"10.0.0.0/8", "1.1.1.1/32", "2606:4700::/32"}) {
		t.Errorf("addresses: %+v %v", s, err)
	}
}

func TestParseRefusesWhatIsNoList(t *testing.T) {
	for name, in := range map[string]string{
		"binary": "MRS\x01\x00\xff\xfe", "html": "<html><body>Not found</body></html>\n",
		"empty": "\n# nothing\n", "only unsupported": "USER-AGENT,x\nMATCH,DIRECT\n", "config": "mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n",
	} {
		if s, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s: read as %+v", name, s)
		}
	}
}

func TestMatch(t *testing.T) {
	s, _ := Parse([]byte(sampleYAML))
	m := newMatcher(s)
	for _, c := range []struct {
		host, ip, want string
		ok             bool
	}{
		{"api.anthropic.com", "", "DOMAIN-SUFFIX,anthropic.com", true},
		{"anthropic.com", "", "DOMAIN-SUFFIX,anthropic.com", true},
		{"notanthropic.com", "", "", false},
		{"chat.openai.example", "", "DOMAIN-KEYWORD,openai", true},
		{"", "160.79.104.9", "IP-CIDR,160.79.104.0/21,no-resolve", true},
		{"", "8.8.8.8", "", false},
	} {
		if got, ok := m.match(c.host, c.ip); ok != c.ok || got != c.want {
			t.Errorf("%q %q = %q %v", c.host, c.ip, got, ok)
		}
	}
	if len(m.preview) != previewSize || m.preview[0].Value != "anthropic.com" {
		t.Errorf("preview: %+v", m.preview)
	}
}

func TestSaveWritesWhatTheCoreReads(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	set, _ := Parse([]byte(".x.example\n"))
	if err := st.Save("s1", &Checked{Source: "/a", Set: set}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(st.Path("s1"))
	var y struct {
		Payload []string `yaml:"payload"`
	}
	if err := yaml.Unmarshal(b, &y); err != nil || !slices.Equal(y.Payload, []string{"+.x.example"}) {
		t.Fatalf("%s: %v", b, err)
	}
	// What is on disk serves after a restart.
	again, _ := Open(st.Dir())
	if m, ok := again.Get("s1"); !ok || m.Count != 1 || m.Behavior != Domain {
		t.Fatalf("%+v %v", m, ok)
	}
	if _, ok := again.Match("s1", "a.x.example", ""); !ok {
		t.Error("no match after reopening")
	}
}

func TestUpdateKeepsTheLastGoodList(t *testing.T) {
	body := "DOMAIN-SUFFIX,a.example\n"
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	st, _ := Open(t.TempDir())
	c, err := Fetch(context.Background(), srv.Client(), srv.URL+"/rules/list.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name() != "list" {
		t.Errorf("name %q", c.Name())
	}
	if err := st.Save("s1", c); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(st.Path("s1"))
	for name, bad := range map[string]func(){
		"a page instead": func() { body = "<html></html>" },
		"server error":   func() { body, status = "DOMAIN,x.example\n", 500 },
	} {
		bad()
		if err := st.Update(context.Background(), srv.Client(), "s1", c.Source); err == nil {
			t.Errorf("%s: no error", name)
		}
		after, _ := os.ReadFile(st.Path("s1"))
		if string(after) != string(before) {
			t.Errorf("%s: file replaced", name)
		}
		if m, _ := st.Get("s1"); m.Error == "" || m.Count != 1 {
			t.Errorf("%s: %+v", name, m)
		}
	}
	body, status = "DOMAIN-SUFFIX,a.example\nDOMAIN-SUFFIX,b.example\n", http.StatusOK
	if err := st.Update(context.Background(), srv.Client(), "s1", c.Source); err != nil {
		t.Fatal(err)
	}
	if m, _ := st.Get("s1"); m.Error != "" || m.Count != 2 {
		t.Errorf("after recovery: %+v", m)
	}
	if _, ok := st.Match("s1", "b.example", ""); !ok {
		t.Error("the new rules don't count")
	}
}

func TestSources(t *testing.T) {
	for _, s := range []string{"https://x.example/a.yaml", "http://x.example/a", "/tmp/a.list", "~/a.list"} {
		if err := Check(s); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	for _, s := range []string{"hello", "x.example/a", "https://", "ftp://x/a"} {
		if err := Check(s); err == nil {
			t.Errorf("%s accepted", s)
		}
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "mine.list")
	_ = os.WriteFile(p, []byte("x.example\n"), 0o600)
	c, err := Fetch(context.Background(), nil, p)
	if err != nil || c.Name() != "mine" {
		t.Errorf("%+v %v", c, err)
	}
}

func TestStaleAndKeep(t *testing.T) {
	st, _ := Open(t.TempDir())
	set, _ := Parse([]byte("x.example\n"))
	_ = st.Save("s1", &Checked{Set: set})
	_ = st.Save("s2", &Checked{Set: set})
	if st.Stale("s1", time.Now()) || !st.Stale("s1", time.Now().Add(UpdateEvery+time.Minute)) {
		t.Error("stale")
	}
	if !st.Stale("never", time.Now()) {
		t.Error("a set never fetched is due")
	}
	st.Keep(map[string]bool{"s1": true})
	if _, ok := st.Get("s2"); ok {
		t.Error("s2 kept")
	}
	if _, err := os.Stat(st.Path("s2")); err == nil || !strings.Contains(err.Error(), "no such") {
		t.Errorf("s2's file: %v", err)
	}
	if _, ok := st.Get("s1"); !ok {
		t.Error("s1 lost")
	}
}
