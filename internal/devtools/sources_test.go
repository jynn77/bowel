package devtools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func sourceOf(t *testing.T, o Options, id string) Source {
	t.Helper()
	for _, s := range Sources(o) {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no source %q", id)
	return Source{}
}

// With nothing configured every tool is on its official source.
func TestSourcesDefault(t *testing.T) {
	got := Sources(opts(t))
	if len(got) != 4 {
		t.Fatalf("%d sources", len(got))
	}
	for _, s := range got {
		if s.Mirror != "" || s.File != "" || len(s.Mirrors) == 0 || s.Tool == "" || s.Down() {
			t.Fatalf("%+v", s)
		}
		for _, m := range s.Mirrors {
			if h := MirrorHost(m); h == m || strings.Contains(h, "/") {
				t.Errorf("host of %q: %q", m, h)
			}
		}
	}
}

func TestSourcesRead(t *testing.T) {
	cases := []struct{ id, rel, text, want string }{
		{SourceNPM, ".npmrc", "//registry.npmjs.org/:_authToken=abc\n  registry = https://npm.acme.dev/ ; ours\n", "https://npm.acme.dev/"},
		{SourcePyPI, ".pip/pip.conf", "[global]\nindex-url: https://pypi.acme.dev/simple\n", "https://pypi.acme.dev/simple"},
		{SourcePyPI, "Library/Application Support/pip/pip.conf", "[global]\nindex-url = https://mirrors.aliyun.com/pypi/simple/\n", "https://mirrors.aliyun.com/pypi/simple/"},
		{SourceGo, goEnvFile(), "GOFLAGS=-mod=mod\nGOPROXY=https://goproxy.cn,direct\n", "https://goproxy.cn"},
		{SourceCargo, ".cargo/config", "[source.crates-io]\nreplace-with = 'vendored'\n[source.vendored]\ndirectory = \"vendor\"\n", "vendored"},
		// The official source written out, a scoped registry, a comment
		// and a source nothing is replaced with are not a mirror.
		{SourceNPM, ".npmrc", "registry=https://registry.npmjs.org/\n", ""},
		{SourceNPM, ".npmrc", "@acme:registry=https://npm.acme.dev/\n# registry=https://old.example/\n; registry=https://older.example/\n", ""},
		{SourcePyPI, ".config/pip/pip.conf", "[global]\nindex-url = https://pypi.org/simple\n", ""},
		{SourceGo, goEnvFile(), "GOPROXY=direct\n", ""},
		{SourceCargo, ".cargo/config.toml", "[source.acme]\nregistry = \"sparse+https://crates.acme.dev/\"\n", ""},
	}
	for _, c := range cases {
		o := opts(t)
		put(t, o.path(c.rel), c.text)
		got := sourceOf(t, o, c.id)
		if got.Mirror != c.want || (c.want != "" && got.File != o.path(c.rel)) {
			t.Errorf("%s in %s: %q\n%+v", c.id, c.rel, c.text, got)
		}
	}
}

// Setting a mirror changes one setting and leaves the rest; going back to
// the official source gives the file it started from.
func TestSetMirrorRoundTrip(t *testing.T) {
	cases := []struct{ id, rel, before string }{
		{SourceNPM, ".npmrc", ""},
		{SourceNPM, ".npmrc", "//registry.npmjs.org/:_authToken=abc\n@acme:registry=https://npm.acme.dev/\n"},
		{SourcePyPI, ".config/pip/pip.conf", ""},
		{SourcePyPI, ".config/pip/pip.conf", "[global]\ntimeout = 60\n\n[install]\nuser = true\n"},
		{SourceGo, goEnvFile(), "GOFLAGS=-mod=mod\n"},
		{SourceCargo, ".cargo/config.toml", ""},
		{SourceCargo, ".cargo/config.toml", "[net]\ngit-fetch-with-cli = true\n"},
		{SourceCargo, ".cargo/config.toml", "[net]\ngit-fetch-with-cli = true\n\n[source.crates-io]\n# as shipped\n\n[build]\njobs = 4\n"},
	}
	for _, c := range cases {
		o := opts(t)
		path := o.path(c.rel)
		if c.before != "" {
			put(t, path, c.before)
		}
		src := sourceOf(t, o, c.id)
		for _, m := range src.Mirrors {
			if err := SetMirror(o, c.id, m); err != nil {
				t.Fatalf("%s: %v", c.id, err)
			}
			if got := sourceOf(t, o, c.id); got.Mirror != m || got.File != path {
				t.Fatalf("%s set %s, read %+v\n%s", c.id, m, got, get(t, path))
			}
		}
		text := get(t, path)
		for _, l := range strings.Split(strings.TrimSpace(c.before), "\n") {
			if !strings.Contains(text, l) {
				t.Errorf("%s: lost %q:\n%s", c.id, l, text)
			}
		}
		if err := SetMirror(o, c.id, ""); err != nil {
			t.Fatal(err)
		}
		if c.before == "" {
			// A file that was only there for this goes with it.
			if _, err := os.Stat(path); err == nil {
				t.Errorf("%s left an empty %s behind", c.id, c.rel)
			}
		} else if got := get(t, path); got != c.before {
			t.Errorf("%s back to official:\n%q\nwant\n%q", c.id, got, c.before)
		}
		if got := sourceOf(t, o, c.id); got.Mirror != "" {
			t.Errorf("%s still on %q", c.id, got.Mirror)
		}
	}
}

// pip refuses a file with two [global] sections.
func TestPipKeepsOneGlobal(t *testing.T) {
	o := opts(t)
	path := o.path(".config/pip/pip.conf")
	put(t, path, "[global]\ntimeout = 60\n")
	src := sourceOf(t, o, SourcePyPI)
	for _, m := range src.Mirrors {
		if err := SetMirror(o, SourcePyPI, m); err != nil {
			t.Fatal(err)
		}
	}
	text := get(t, path)
	if strings.Count(text, "[global]") != 1 || strings.Count(text, "index-url") != 1 {
		t.Fatalf("%s", text)
	}
}

// A setting of the user's is changed where it is, under the name it has.
func TestSetMirrorInPlace(t *testing.T) {
	o := opts(t)
	old := o.path(".pip/pip.conf")
	put(t, old, "[global]\nindex-url = https://pypi.acme.dev/simple\n")
	m := sourceOf(t, o, SourcePyPI).Mirrors[0]
	if err := SetMirror(o, SourcePyPI, m); err != nil {
		t.Fatal(err)
	}
	if got := get(t, old); got != "[global]\nindex-url = "+m+"\n" {
		t.Fatalf("%q", got)
	}
	if _, err := os.Stat(o.path(".config/pip/pip.conf")); err == nil {
		t.Fatal("a second file was written")
	}

	o = opts(t)
	cargo := o.path(".cargo/config.toml")
	put(t, cargo, "[source.crates-io]\nreplace-with = \"acme\"\n\n[source.acme]\nregistry = \"sparse+https://crates.acme.dev/\"\n")
	m = sourceOf(t, o, SourceCargo).Mirrors[0]
	if err := SetMirror(o, SourceCargo, m); err != nil {
		t.Fatal(err)
	}
	if got := get(t, cargo); got != "[source.crates-io]\nreplace-with = \"acme\"\n\n[source.acme]\nregistry = \""+m+"\"\n" {
		t.Fatalf("%q", got)
	}
}

// Only bowel's own addresses are written into a config file.
func TestSetMirrorRefuses(t *testing.T) {
	o := opts(t)
	for _, bad := range []string{"https://x.example/", "https://registry.npmmirror.com/\nalways-auth=true"} {
		if err := SetMirror(o, SourceNPM, bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if err := SetMirror(o, "nope", ""); err == nil {
		t.Error("unknown source accepted")
	}
	if _, err := os.Stat(o.path(".npmrc")); err == nil {
		t.Fatal("a refused address still wrote the file")
	}
}

func TestProbe(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer up.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer bad.Close()
	gone := httptest.NewServer(nil)
	gone.Close()
	// One lost request is not a mirror that is down.
	n := 0
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n++; n == 1 {
			panic(http.ErrAbortHandler)
		}
	}))
	defer flaky.Close()
	if _, ok := ask(context.Background(), http.DefaultClient, flaky.URL); !ok || n != 2 {
		t.Errorf("a way that answers the second time was called down (asked %d times)", n)
	}

	s := Source{ID: SourceNPM, Mirror: gone.URL + "/", Mirrors: []string{up.URL + "/", bad.URL + "/"}}
	s.Probes = Prober{Direct: http.DefaultClient}.Probe(context.Background(), s)
	if len(s.Probes) != 3 {
		t.Fatalf("%+v", s.Probes)
	}
	if p, ok := s.ProbeOf(up.URL + "/"); !ok || !p.OK || p.Ms < 1 {
		t.Errorf("a server that answers 404 is there: %+v", p)
	}
	if p, _ := s.ProbeOf(bad.URL + "/"); p.OK {
		t.Errorf("a 502 is not an answer: %+v", p)
	}
	if !s.Down() {
		t.Error("what is in use does not answer")
	}
	// With no proxy the official source is not measured.
	if _, ok := s.ProbeOf(""); ok {
		t.Error("the official source was measured without bowel")
	}
	if kind, m := s.Fix(false); kind != FixMirror || m != up.URL+"/" {
		t.Errorf("fix: %q %q", kind, m)
	}
}

func TestFix(t *testing.T) {
	ms := []string{"a", "b"}
	cases := []struct {
		name   string
		s      Source
		direct bool
		kind   string
		mirror string
	}{
		{"not measured", Source{Mirror: "a", Mirrors: ms}, false, "", ""},
		{"fine", Source{Mirror: "a", Mirrors: ms, Probes: []Probe{{"a", 30, true}}}, false, "", ""},
		{"mirror down, the fastest other", Source{Mirror: "a", Mirrors: ms, Probes: []Probe{{"", 140, true}, {"a", 0, false}, {"b", 50, true}}}, false, FixMirror, "b"},
		{"mirror down, no other mirror", Source{Mirror: "a", Mirrors: ms, Probes: []Probe{{"", 140, true}, {"a", 0, false}, {"b", 0, false}}}, false, FixOfficial, ""},
		{"official down because it is routed directly", Source{Mirrors: ms, Probes: []Probe{{"", 0, false}, {"a", 30, true}}}, true, FixRoute, ""},
		{"official down through its node", Source{Mirrors: ms, Probes: []Probe{{"", 0, false}, {"a", 30, true}, {"b", 20, true}}}, false, FixMirror, "b"},
		{"nothing answers", Source{Mirror: "a", Mirrors: ms, Probes: []Probe{{"", 0, false}, {"a", 0, false}, {"b", 0, false}}}, false, "", ""},
		{"a private registry down is not swapped for an unlisted one", Source{Mirror: "x", Mirrors: ms, Probes: []Probe{{"x", 0, false}, {"y", 10, true}, {"a", 30, true}}}, false, FixMirror, "a"},
	}
	for _, c := range cases {
		if kind, m := c.s.Fix(c.direct); kind != c.kind || m != c.mirror {
			t.Errorf("%s: %q %q", c.name, kind, m)
		}
	}
}
