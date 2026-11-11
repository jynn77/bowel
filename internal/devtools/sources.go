package devtools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// Download sources. A package registry is already a service of a route
// group, so the route its traffic takes is the group it is in. What a
// service cannot say is "download from a mirror instead": that is the
// tool's own setting, one line of its config. bowel reads it, changes that
// one line when asked, and measures whether what is in use answers, so
// that one that does not can be put right in one press.
//
// The IDs are the route services' (internal/routes), which is how a
// source finds the group its official address is routed by.
const (
	SourceNPM   = "npm"
	SourcePyPI  = "pypi"
	SourceGo    = "golang"
	SourceCargo = "rust"
)

// Probe is one measured way of reaching a source.
type Probe struct {
	Mirror string `json:"mirror"` // "" is the official source
	Ms     int    `json:"ms"`
	OK     bool   `json:"ok"`
}

// Source is where one tool downloads from.
type Source struct {
	ID   string `json:"id"`
	Tool string `json:"tool"` // npm, pip, Go, Cargo
	// Mirror is the address found in the tool's config; "" is its official
	// source.
	Mirror string `json:"mirror,omitempty"`
	// File is where Mirror was found.
	File string `json:"file,omitempty"`
	// Mirrors are the ones bowel offers.
	Mirrors []string `json:"mirrors"`
	// Probes are what was measured last, nil before the first time.
	Probes []Probe `json:"probes,omitempty"`
}

// MirrorHost is a mirror as a row shows it: without scheme or path.
func MirrorHost(mirror string) string {
	raw := strings.TrimPrefix(mirror, "sparse+")
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return mirror
}

// ProbeOf is what was measured of one way; ok is false when it was not.
func (s Source) ProbeOf(mirror string) (Probe, bool) {
	for _, p := range s.Probes {
		if p.Mirror == mirror {
			return p, true
		}
	}
	return Probe{}, false
}

// Down: what the tool is set to was measured, and did not answer.
func (s Source) Down() bool {
	p, ok := s.ProbeOf(s.Mirror)
	return ok && !p.OK
}

// What Fix says to do.
const (
	FixMirror   = "mirror"   // use the mirror it names
	FixOfficial = "official" // go back to the official source
	FixRoute    = "route"    // send the official source through a node again
)

// Fix is what puts a source that is down right; kind is "" when it is not
// down, or nothing measured would help. routedDirect says the official
// address is not sent through a node now, which is then the thing to
// change: a mirror is not what the user chose.
func (s Source) Fix(routedDirect bool) (kind, mirror string) {
	if !s.Down() {
		return "", ""
	}
	if s.Mirror == "" && routedDirect {
		return FixRoute, ""
	}
	best := Probe{}
	for _, p := range s.Probes {
		if p.OK && p.Mirror != "" && p.Mirror != s.Mirror && slices.Contains(s.Mirrors, p.Mirror) && (best.Mirror == "" || p.Ms < best.Ms) {
			best = p
		}
	}
	if best.Mirror != "" {
		return FixMirror, best.Mirror
	}
	if p, ok := s.ProbeOf(""); ok && p.OK && s.Mirror != "" {
		return FixOfficial, ""
	}
	return "", ""
}

// sourceSpec is how one tool's config says where to download from.
type sourceSpec struct {
	id, tool string
	official string
	// files are where the tool reads its user config, the one a new
	// setting goes into first.
	files   func(o Options) []string
	mirrors []string
	// value finds the setting in a config file's text; "" is none.
	value func(text string) string
	// set and unset change that one setting in a file's text.
	set   func(text, mirror string) string
	unset func(text string) string
	// probe is a small address under a source's that says it answers.
	probe func(base string) string
	mode  os.FileMode
}

// ---- Lines ----

// setLine replaces the first line re matches with line and deletes the
// others; ok is false when none matched.
func setLine(text string, re *regexp.Regexp, line string) (string, bool) {
	lines, out, done := strings.Split(text, "\n"), []string(nil), false
	for _, l := range lines {
		switch {
		case !re.MatchString(l):
			out = append(out, l)
		case !done:
			out, done = append(out, line), true
		}
	}
	return strings.Join(out, "\n"), done
}

func dropLines(text string, re *regexp.Regexp) string {
	lines := strings.Split(text, "\n")
	return strings.Join(slices.DeleteFunc(lines, re.MatchString), "\n")
}

var (
	npmLine   = regexp.MustCompile(`^[ \t]*registry[ \t]*=[ \t]*([^\s#;]*)`)
	pipLine   = regexp.MustCompile(`^[ \t]*index-url[ \t]*[=:][ \t]*([^\s#;]*)`)
	pipGlobal = regexp.MustCompile(`(?m)^\[global\][ \t]*$`)
	goLine    = regexp.MustCompile(`^GOPROXY=(.*)$`)
	tomlHead  = regexp.MustCompile(`^[ \t]*\[`)
	tomlStr   = `[ \t]*=[ \t]*["']([^"']*)["']`
	cargoWith = regexp.MustCompile(`^[ \t]*replace-with` + tomlStr)
	cargoReg  = regexp.MustCompile(`^[ \t]*registry` + tomlStr)
)

// lineValue is the first value re captures on a line of text.
func lineValue(text string, re *regexp.Regexp) string {
	for _, l := range strings.Split(text, "\n") {
		if m := re.FindStringSubmatch(l); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// unless is v, or "" when v is the official source written out.
func unless(v, official string) string {
	if strings.TrimSuffix(v, "/") == strings.TrimSuffix(official, "/") {
		return ""
	}
	return v
}

// ---- Cargo's tables ----

const cargoName = "bowel-mirror"

// table is the lines of [source.<name>]: the header at from, its body up
// to to, without the blank lines before the next table.
func table(lines []string, name string) (from, to int, ok bool) {
	head := regexp.MustCompile(`^[ \t]*\[source\.` + regexp.QuoteMeta(name) + `\][ \t]*(#.*)?$`)
	from = slices.IndexFunc(lines, head.MatchString)
	if from < 0 {
		return 0, 0, false
	}
	to = from + 1
	for to < len(lines) && !tomlHead.MatchString(lines[to]) {
		to++
	}
	for to > from+1 && strings.TrimSpace(lines[to-1]) == "" {
		to--
	}
	return from, to, true
}

func tableValue(lines []string, name string, re *regexp.Regexp) string {
	from, to, ok := table(lines, name)
	if !ok {
		return ""
	}
	return lineValue(strings.Join(lines[from+1:to], "\n"), re)
}

// cargoValue is the registry crates-io is replaced with, or the name of
// what replaces it when that is not a registry (a vendored directory).
func cargoValue(text string) string {
	lines := strings.Split(text, "\n")
	name := tableValue(lines, "crates-io", cargoWith)
	if name == "" {
		return ""
	}
	if reg := tableValue(lines, name, cargoReg); reg != "" {
		return reg
	}
	return name
}

// setInTable sets one key of a table, adding the table when it is missing.
func setInTable(lines []string, name string, re *regexp.Regexp, line string) []string {
	from, to, ok := table(lines, name)
	if !ok {
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = lines[:n-1]
		}
		return append(lines, "[source."+name+"]", line, "")
	}
	for i := from + 1; i < to; i++ {
		if re.MatchString(lines[i]) {
			lines[i] = line
			return lines
		}
	}
	return slices.Insert(lines, from+1, line)
}

func cargoSet(text, mirror string) string {
	lines := strings.Split(text, "\n")
	if text == "" {
		lines = nil
	}
	reg := `registry = "` + mirror + `"`
	// A registry of the user's own naming keeps its name.
	if name := tableValue(lines, "crates-io", cargoWith); name != "" && tableValue(lines, name, cargoReg) != "" {
		return strings.Join(setInTable(lines, name, cargoReg, reg), "\n")
	}
	lines = setInTable(lines, "crates-io", cargoWith, `replace-with = "`+cargoName+`"`)
	return strings.Join(setInTable(lines, cargoName, cargoReg, reg), "\n")
}

// cargoUnset stops replacing crates-io, and takes away the tables that
// were only there for that.
func cargoUnset(text string) string {
	lines := strings.Split(text, "\n")
	if from, to, ok := table(lines, cargoName); ok {
		lines = slices.Delete(lines, from, to)
	}
	from, to, ok := table(lines, "crates-io")
	if !ok {
		return strings.Join(lines, "\n")
	}
	for i := from + 1; i < to; i++ {
		if cargoWith.MatchString(lines[i]) {
			lines = slices.Delete(lines, i, i+1)
			to--
			break
		}
	}
	if to == from+1 {
		lines = slices.Delete(lines, from, to)
	}
	return strings.Join(lines, "\n")
}

// ---- The tools ----

var sourceSpecs = []sourceSpec{
	{
		id: SourceNPM, tool: "npm", official: "https://registry.npmjs.org/", mode: 0o600,
		files: func(o Options) []string { return []string{o.path(".npmrc")} },
		mirrors: []string{
			"https://registry.npmmirror.com/",
			"https://mirrors.cloud.tencent.com/npm/",
			"https://repo.huaweicloud.com/repository/npm/",
		},
		value: func(text string) string { return lineValue(text, npmLine) },
		set: func(text, m string) string {
			if out, ok := setLine(text, npmLine, "registry="+m); ok {
				return out
			}
			return appended(text, []string{"registry=" + m})
		},
		unset: func(text string) string { return dropLines(text, npmLine) },
		// A small package's page, which is what npm itself asks for. Not
		// /-/ping: npmmirror does not answer that, though it serves packages.
		probe: func(base string) string { return base + "ms" },
	},
	{
		id: SourcePyPI, tool: "pip", official: "https://pypi.org/simple", mode: 0o644,
		// pip on macOS reads Application Support when that folder is
		// there, and ~/.config otherwise; ~/.pip is the old place. On
		// Windows it is pip.ini under AppData.
		files: func(o Options) []string {
			if runtime.GOOS == "windows" {
				return []string{o.path("AppData/Roaming/pip/pip.ini"), o.path("pip/pip.ini")}
			}
			mac, xdg, old := o.path("Library/Application Support/pip/pip.conf"), o.path(".config/pip/pip.conf"), o.path(".pip/pip.conf")
			if st, err := os.Stat(filepath.Dir(mac)); err == nil && st.IsDir() {
				return []string{mac, xdg, old}
			}
			return []string{xdg, mac, old}
		},
		mirrors: []string{
			"https://pypi.tuna.tsinghua.edu.cn/simple",
			"https://mirrors.aliyun.com/pypi/simple/",
			"https://mirrors.ustc.edu.cn/pypi/simple",
		},
		value: func(text string) string { return lineValue(text, pipLine) },
		// pip refuses a file with two [global] sections, so the line goes
		// into the one that is there.
		set: func(text, m string) string {
			line := "index-url = " + m
			if out, ok := setLine(text, pipLine, line); ok {
				return out
			}
			if loc := pipGlobal.FindStringIndex(text); loc != nil {
				return text[:loc[1]] + "\n" + line + text[loc[1]:]
			}
			return appended(text, []string{"[global]", line})
		},
		unset: func(text string) string {
			out := dropLines(text, pipLine)
			// A [global] that was only there for this goes with it.
			if strings.TrimSpace(out) == "[global]" {
				return ""
			}
			return out
		},
		probe: func(base string) string { return strings.TrimSuffix(base, "/") + "/pip/" },
	},
	{
		id: SourceGo, tool: "Go", official: "https://proxy.golang.org", mode: 0o644,
		// What `go env -w` writes: a line to a setting. GOENV can move the
		// file; that is not followed.
		files: func(o Options) []string { return []string{o.path(goEnvFile())} },
		mirrors: []string{
			"https://goproxy.cn",
			"https://goproxy.io",
			"https://mirrors.aliyun.com/goproxy/",
		},
		// GOPROXY is a list: the first is the one asked.
		value: func(text string) string {
			v, _, _ := strings.Cut(lineValue(text, goLine), ",")
			if v == "direct" || v == "off" {
				return ""
			}
			return v
		},
		set: func(text, m string) string {
			line := "GOPROXY=" + m + ",direct"
			if out, ok := setLine(text, goLine, line); ok {
				return out
			}
			return appended(text, []string{line})
		},
		unset: func(text string) string { return dropLines(text, goLine) },
		probe: func(base string) string { return strings.TrimSuffix(base, "/") + "/golang.org/x/text/@v/list" },
	},
	{
		id: SourceCargo, tool: "Cargo", official: "sparse+https://index.crates.io/", mode: 0o644,
		files: func(o Options) []string { return []string{o.path(".cargo/config.toml"), o.path(".cargo/config")} },
		mirrors: []string{
			"sparse+https://rsproxy.cn/index/",
			"sparse+https://mirrors.ustc.edu.cn/crates.io-index/",
			"sparse+https://mirrors.tuna.tsinghua.edu.cn/crates.io-index/",
		},
		value: cargoValue,
		set:   cargoSet,
		unset: cargoUnset,
		probe: func(base string) string { return strings.TrimPrefix(base, "sparse+") + "config.json" },
	},
}

func sourceSpecOf(id string) (sourceSpec, bool) {
	for _, s := range sourceSpecs {
		if s.id == id {
			return s, true
		}
	}
	return sourceSpec{}, false
}

func (o Options) readSource(s sourceSpec) Source {
	out := Source{ID: s.id, Tool: s.tool, Mirrors: s.mirrors}
	for _, f := range s.files(o) {
		text, err := read(f)
		if err != nil {
			continue
		}
		if v := unless(s.value(text), s.official); v != "" {
			out.Mirror, out.File = v, f
			break
		}
	}
	return out
}

// Sources reads where each tool downloads from.
func Sources(o Options) []Source {
	out := make([]Source, 0, len(sourceSpecs))
	for _, s := range sourceSpecs {
		out = append(out, o.readSource(s))
	}
	return out
}

// SetMirror points a tool at one of its mirrors, or with "" back at its
// official source, by changing that one setting where it is and leaving
// the rest of the file as it was.
func SetMirror(o Options, id, mirror string) error {
	s, ok := sourceSpecOf(id)
	if !ok {
		return fmt.Errorf("unknown source %q", id)
	}
	// Only addresses bowel wrote itself go into a config file.
	if mirror != "" && !slices.Contains(s.mirrors, mirror) {
		return fmt.Errorf("%s: %q is not one of its mirrors", s.tool, mirror)
	}
	files := s.files(o)
	if mirror != "" {
		file := files[0]
		if now := o.readSource(s); now.File != "" {
			file = now.File
		}
		text, err := read(file)
		if err != nil {
			return err
		}
		return write(file, s.set(text, mirror), s.mode)
	}
	// Back to official: wherever it is set.
	for _, f := range files {
		text, err := read(f)
		if err != nil {
			return err
		}
		if s.value(text) == "" {
			continue
		}
		out := s.unset(text)
		// A file with nothing left in it says what no file says.
		if strings.TrimSpace(out) == "" {
			if err := os.Remove(f); err != nil {
				return err
			}
			continue
		}
		if err := write(f, out, s.mode); err != nil {
			return err
		}
	}
	return nil
}

// ---- Measuring ----

// Prober measures sources. Mirrors are asked directly; the official
// address through bowel, so that it takes the route its service has.
// Proxied is nil when bowel is not carrying traffic: the official source
// is then not measured, and so not called down.
type Prober struct {
	Direct, Proxied *http.Client
}

// NewProber is a Prober for bowel's mixed port; 0 is no proxy.
func NewProber(port int) Prober {
	client := func(proxy func(*http.Request) (*url.URL, error)) *http.Client {
		return &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{Proxy: proxy, DisableKeepAlives: true}}
	}
	p := Prober{Direct: client(nil)}
	if port > 0 {
		p.Proxied = client(http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}))
	}
	return p
}

// tries is how often a way is asked before it is called down: one lost
// request is common on a mirror's CDN, and is not the mirror being down.
const tries, tryFor = 3, 2500 * time.Millisecond

func askOnce(ctx context.Context, c *http.Client, addr string) (int, bool) {
	ctx, cancel := context.WithTimeout(ctx, tryFor)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return 0, false
	}
	start := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		return 0, false
	}
	resp.Body.Close()
	// Any answer of the server's own counts: a mirror that has no such
	// page is still there. A gateway's 5xx is not.
	return max(1, int(time.Since(start).Milliseconds())), resp.StatusCode < 500
}

// ask is how long addr took to answer, on the first try that it did.
func ask(ctx context.Context, c *http.Client, addr string) (int, bool) {
	for range tries {
		if ms, ok := askOnce(ctx, c, addr); ok {
			return ms, true
		}
		if ctx.Err() != nil {
			break
		}
	}
	return 0, false
}

// Probe measures every way of reaching s: its official source, its
// mirrors, and what it is set to when that is neither.
func (p Prober) Probe(ctx context.Context, s Source) []Probe {
	spec, ok := sourceSpecOf(s.ID)
	if !ok {
		return nil
	}
	ways := append([]string{""}, s.Mirrors...)
	if s.Mirror != "" && !slices.Contains(ways, s.Mirror) {
		ways = append(ways, s.Mirror)
	}
	out := make([]Probe, len(ways))
	var wg sync.WaitGroup
	for i, m := range ways {
		client, base := p.Direct, m
		if m == "" {
			client, base = p.Proxied, spec.official
		}
		out[i].Mirror = m
		if client == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i].Ms, out[i].OK = ask(ctx, client, spec.probe(base))
		}()
	}
	wg.Wait()
	if p.Proxied == nil {
		out = out[1:]
	}
	return out
}

// goEnvFile is where Go keeps what `go env -w` wrote, under the home folder:
// the user's config folder, which each system has in its own place.
func goEnvFile() string {
	switch runtime.GOOS {
	case "darwin":
		return "Library/Application Support/go/env"
	case "windows":
		return "AppData/Roaming/go/env"
	}
	return ".config/go/env"
}
