// Package devtools reads which developer tools reach the proxy, and
// connects them one at a time.
//
// What is written sits between two comments, and undoing deletes all of it;
// nothing of what was there before is kept or restored. Where a format has
// no comments (JSON), or the program may rewrite the file (colima.yaml), the
// key itself is removed, and written.json remembers that bowel put it there.
package devtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/nowindow"
)

// The tools, by ID.
const (
	Dockerd    = "dockerd"    // Colima's Docker daemon: docker pull
	Containers = "containers" // what runs in containers, and docker build
	Shell      = "shell"      // zsh's environment
	Langs      = "langs"      // tools that read that environment
	GitHTTPS   = "git-https"
	GitSSH     = "git-ssh"
)

// Whether a tool's traffic reaches bowel.
const (
	On      = "on"
	Off     = "off"
	Other   = "other"   // another proxy
	Stale   = "stale"   // bowel's, but not at the address bowel is at now
	Unknown = "unknown" // could not be read
)

// Where a tool's setting comes from.
const (
	SourceNone   = "none"
	SourceBowel  = "bowel"
	SourceOwn    = "own"    // the user's: shown, never changed
	SourceFollow = "follow" // no setting of its own, it follows the shell
)

// What bowel has written for a tool.
const (
	AppliedProxy  = "proxy"
	AppliedDirect = "direct"
)

// What a tool does when bowel is not running.
const (
	GuardNeed = "need" // it cannot reach the proxy: it fails
	GuardAuto = "auto" // it connects directly
)

// What can be done to a tool.
const (
	KindApply  = "apply"
	KindUpdate = "update"
	KindDirect = "direct"
	KindUndo   = "undo"
)

// Tool is what was found of one tool.
type Tool struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Source  string `json:"source"`
	Applied string `json:"applied,omitempty"`
	Guard   string `json:"guard,omitempty"`
	// Value is the proxy it was found pointing at.
	Value string `json:"value,omitempty"`
	File  string `json:"file,omitempty"`
	// Port is the one in what bowel wrote.
	Port int `json:"port,omitempty"`
	// Pending: written, but the running program has not taken it yet.
	Pending bool `json:"pending,omitempty"`
	// Restart: changing it restarts Colima, which stops the containers.
	Restart bool `json:"restart,omitempty"`
}

// ProxyAction is what asking for the proxy does now; "" is nothing.
func (t Tool) ProxyAction() string {
	switch {
	case t.State == On || t.ID == Langs:
		return ""
	case t.State == Stale:
		return KindUpdate
	case t.Applied == AppliedDirect:
		return KindUndo
	case t.State == Off && t.Source == SourceNone && t.ID != GitHTTPS:
		return KindApply
	}
	return ""
}

// DirectAction is what asking for a direct connection does now.
func (t Tool) DirectAction() string {
	switch {
	case t.Applied == AppliedProxy:
		return KindUndo
	case t.ID == GitHTTPS && t.Source == SourceFollow && t.State == On:
		return KindDirect
	}
	return ""
}

// Options is the machine and the proxy to read and write for.
type Options struct {
	Home    string
	DataDir string // where written.json is
	Port    int    // bowel's mixed port
	NoProxy []string
	// Shell is the terminal's shell, "zsh" or "bash"; "" is this
	// machine's (platform.go). Netcat likewise is the nc that ssh and
	// the shell call, "openbsd" or "ncat"; "" is this machine's.
	Shell  string
	Netcat string
}

const colimaConfig = ".colima/default/colima.yaml"

var binDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}

// find locates a program. An app started from Finder has a short PATH.
func find(name string) string {
	for _, d := range binDirs {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	p, _ := exec.LookPath(name)
	return p
}

// git is a git that will run: without the command line tools, /usr/bin/git
// is a stub that opens an installer dialog.
func git() string {
	p := find("git")
	if p != "/usr/bin/git" || runtime.GOOS != "darwin" {
		return p
	}
	if nowindow.Command("/usr/bin/xcode-select", "-p").Run() != nil {
		return ""
	}
	return p
}

func (o Options) cmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	c := nowindow.CommandContext(ctx, name, args...)
	c.Env = append(os.Environ(), "HOME="+o.Home, "PATH="+strings.Join(binDirs, ":")+":"+os.Getenv("PATH"))
	return c
}

func (o Options) path(rel string) string { return filepath.Join(o.Home, rel) }

// ---- The block ----

func blockOpen() string {
	return i18n.T("# >>> bowel 管理 · 请勿在此区域内添加内容，撤销时会整段删除", "# >>> managed by bowel · do not add anything here, undo deletes the whole block")
}

const blockClose = "# <<< bowel"

func isOpen(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "# >>>") && strings.Contains(t, "bowel")
}
func isClose(line string) bool { return strings.TrimSpace(line) == blockClose }

func wrap(indent string, lines ...string) []string {
	out := []string{indent + blockOpen()}
	for _, l := range lines {
		out = append(out, indent+l)
	}
	return append(out, indent+blockClose)
}

// block is what is between the comments, nil if there are none.
func block(text string) []string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if !isOpen(l) {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if isClose(lines[j]) {
				return lines[i : j+1]
			}
		}
	}
	return nil
}

// strip deletes every whole block. A block that never closes stays: what
// follows its opening is not known to be bowel's.
func strip(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if isOpen(lines[i]) {
			end := -1
			for j := i + 1; j < len(lines); j++ {
				if isClose(lines[j]) {
					end = j
					break
				}
			}
			if end >= 0 {
				i = end
				continue
			}
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

func appended(text string, lines []string) string {
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + strings.Join(lines, "\n") + "\n"
}

func prepended(text string, lines []string) string {
	return strings.Join(lines, "\n") + "\n" + text
}

func read(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// write replaces path at once, through a symlink if it is one (dotfiles
// often are), keeping its mode.
func write(path, text string, mode os.FileMode) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".bowel-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(text); err == nil {
		err = f.Chmod(mode)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// ---- What bowel wrote where a comment cannot say so ----

type written map[string]int

func (o Options) writtenPath() string { return filepath.Join(o.DataDir, "written.json") }

func (o Options) loadWritten() written {
	w := written{}
	if b, err := os.ReadFile(o.writtenPath()); err == nil {
		_ = json.Unmarshal(b, &w)
	}
	return w
}

func (o Options) saveWritten(w written) error {
	b, err := json.Marshal(w)
	if err != nil {
		return err
	}
	return write(o.writtenPath(), string(b)+"\n", 0o600)
}

func (o Options) remember(id string) error {
	w := o.loadWritten()
	w[id] = o.Port
	return o.saveWritten(w)
}

func (o Options) forget(id string) error {
	w := o.loadWritten()
	if _, ok := w[id]; !ok {
		return nil
	}
	delete(w, id)
	return o.saveWritten(w)
}

// ---- Colima ----

type colimaFile struct {
	Docker struct {
		Proxies struct {
			HTTP string `yaml:"http-proxy"`
		} `yaml:"proxies"`
	} `yaml:"docker"`
	Network struct {
		Gateway string `yaml:"gatewayAddress"`
	} `yaml:"network"`
}

func (o Options) colimaInstalled() bool {
	_, err := os.Stat(o.path(colimaConfig))
	return err == nil
}

func (o Options) readColima() (colimaFile, string, error) {
	var f colimaFile
	text, err := read(o.path(colimaConfig))
	if err != nil {
		return f, "", err
	}
	err = yaml.Unmarshal([]byte(text), &f)
	return f, text, err
}

func (o Options) gateway() string {
	if f, _, err := o.readColima(); err == nil && f.Network.Gateway != "" {
		return f.Network.Gateway
	}
	return "192.168.5.2"
}

func (o Options) noProxy(extra ...string) string {
	list := append([]string{"localhost", "127.0.0.0/8"}, extra...)
	for _, n := range o.NoProxy {
		if !slices.Contains(list, n) {
			list = append(list, n)
		}
	}
	return strings.Join(list, ",")
}

func (o Options) colimaBlock() []string {
	gw := o.gateway()
	net24 := gw
	if i := strings.LastIndexByte(gw, '.'); i > 0 {
		net24 = gw[:i] + ".0/24"
	}
	addr := fmt.Sprintf("http://%s:%d", gw, o.Port)
	return wrap("  ", "proxies:",
		"  http-proxy: "+addr,
		"  https-proxy: "+addr,
		"  no-proxy: "+o.noProxy(net24, "host.docker.internal", "host.lima.internal"))
}

var colimaDockerKey = regexp.MustCompile(`^docker:\s*(\{\s*\})?\s*(#.*)?$`)

func indentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// dropColimaProxies removes docker.proxies, and the comments around it if
// Colima has left them. The key is what is looked for, not the comments:
// Colima may rewrite this file when it starts.
func dropColimaProxies(text string) string {
	lines := strings.Split(text, "\n")
	at := slices.IndexFunc(lines, colimaDockerKey.MatchString)
	if at < 0 {
		return text
	}
	out := slices.Clone(lines[:at+1])
	kids, j := 0, at+1
	for ; j < len(lines); j++ {
		l, t := lines[j], strings.TrimSpace(lines[j])
		if t != "" && indentOf(l) == 0 {
			break
		}
		if isOpen(l) || isClose(l) {
			continue
		}
		if strings.HasPrefix(t, "proxies:") {
			for j+1 < len(lines) && strings.TrimSpace(lines[j+1]) != "" && indentOf(lines[j+1]) > indentOf(l) {
				j++
			}
			continue
		}
		if t != "" && !strings.HasPrefix(t, "#") {
			kids++
		}
		out = append(out, l)
	}
	if kids == 0 {
		out[at] = "docker: {}"
	}
	return strings.Join(append(out, lines[j:]...), "\n")
}

func setColimaProxies(text string, lines []string) (string, error) {
	text = dropColimaProxies(text)
	all := strings.Split(text, "\n")
	at := slices.IndexFunc(all, colimaDockerKey.MatchString)
	if at < 0 {
		if slices.ContainsFunc(all, func(l string) bool { return strings.HasPrefix(l, "docker:") }) {
			return "", errors.New("colima.yaml: docker is not in a form bowel can add to")
		}
		return appended(text, append([]string{"docker:"}, lines...)), nil
	}
	out := slices.Clone(all[:at])
	out = append(append(out, "docker:"), lines...)
	return strings.Join(append(out, all[at+1:]...), "\n"), nil
}

func (o Options) dockerSocket() string { return o.path(".colima/default/docker.sock") }

// engine asks Colima's Docker daemon, without the docker command.
func (o Options) engine(ctx context.Context, path string) ([]byte, error) {
	hc := http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", o.dockerSocket())
	}}}
	defer hc.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return nil, err
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var b strings.Builder
	buf := make([]byte, 64<<10)
	for {
		n, rerr := res.Body.Read(buf)
		b.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	return []byte(b.String()), nil
}

// daemonProxy is the proxy the running daemon uses. ok: it answered.
func (o Options) daemonProxy(ctx context.Context) (proxy string, ok bool) {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	b, err := o.engine(c, "/info")
	if err != nil {
		return "", false
	}
	var info struct{ HTTPProxy string }
	if json.Unmarshal(b, &info) != nil {
		return "", false
	}
	return info.HTTPProxy, true
}

// ColimaRunning says whether Colima's Docker daemon answers.
func ColimaRunning(ctx context.Context, o Options) bool {
	_, ok := o.daemonProxy(ctx)
	return ok
}

// RestartColima restarts it and waits until the daemon has the proxy in the
// file. With a service keeping Colima up, `brew services restart` is not
// enough: it ends the wrapper but may leave the VM, and the next wrapper
// finds it "already running" and never reads the file. So the service goes
// first, then the VM, and neither races launchd bringing the other back.
func RestartColima(ctx context.Context, o Options) error {
	f, _, err := o.readColima()
	if err != nil {
		return err
	}
	want := f.Docker.Proxies.HTTP
	colima := find("colima")
	if colima == "" {
		return errors.New("colima not found")
	}
	run := func(name string, args ...string) error {
		if out, err := o.cmd(ctx, name, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s %s: %w: %s", filepath.Base(name), strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	brew := find("brew")
	if _, serr := os.Stat(o.path("Library/LaunchAgents/sh.brew.colima.plist")); serr == nil && brew != "" {
		if err := run(brew, "services", "stop", "colima"); err != nil {
			return err
		}
		_ = run(colima, "stop") // the service may have stopped it already
		if err := run(brew, "services", "start", "colima"); err != nil {
			return err
		}
	} else if err := run(colima, "restart"); err != nil {
		return err
	}
	// The service starts it in the background.
	for {
		select {
		case <-ctx.Done():
			return errors.New("colima did not come back")
		case <-time.After(time.Second):
		}
		if live, ok := o.daemonProxy(ctx); ok && live == want {
			return nil
		}
	}
}

func portOf(addr string) int {
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return 0
	}
	p, _ := strconv.Atoi(u.Port())
	return p
}

func hostOf(addr string) string {
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// judge is the state of a tool with this proxy that bowel wrote or not.
func (o Options) judge(t *Tool, value string, ours bool, hosts ...string) {
	t.Value = value
	if ours {
		t.Source, t.Applied, t.Port = SourceBowel, AppliedProxy, portOf(value)
		t.State = On
		if t.Port != o.Port {
			t.State = Stale
		}
		return
	}
	t.Source, t.State = SourceOwn, Other
	if portOf(value) == o.Port && slices.Contains(hosts, hostOf(value)) {
		t.State = On
	}
}

func (o Options) readDockerd(ctx context.Context, w written) Tool {
	t := Tool{ID: Dockerd, State: Off, Source: SourceNone, Guard: GuardNeed, File: "~/" + colimaConfig, Restart: true}
	f, _, err := o.readColima()
	if err != nil {
		t.State = Unknown
		return t
	}
	v := f.Docker.Proxies.HTTP
	if v == "" {
		return t
	}
	_, ours := w[Dockerd]
	gw := f.Network.Gateway
	if gw == "" {
		gw = "192.168.5.2"
	}
	o.judge(&t, v, ours, gw)
	if live, ok := o.daemonProxy(ctx); ok && live != v && ours && t.State == On {
		t.State, t.Pending = Stale, true
	}
	return t
}

// ---- docker's own config: what containers and builds are given ----

const dockerConfig = ".docker/config.json"

func (o Options) containerAddr() string { return fmt.Sprintf("http://%s:%d", o.gateway(), o.Port) }

type containerProxy struct {
	HTTP  string `json:"httpProxy,omitempty"`
	HTTPS string `json:"httpsProxy,omitempty"`
	No    string `json:"noProxy,omitempty"`
}

func (o Options) readDockerConfig() (map[string]json.RawMessage, error) {
	text, err := read(o.path(dockerConfig))
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{}
	if strings.TrimSpace(text) == "" {
		return m, nil
	}
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return nil, fmt.Errorf("~/%s: %w", dockerConfig, err)
	}
	return m, nil
}

func (o Options) writeDockerConfig(m map[string]json.RawMessage) error {
	b, err := json.MarshalIndent(m, "", "\t")
	if err != nil {
		return err
	}
	return write(o.path(dockerConfig), string(b)+"\n", 0o600)
}

func containerValue(m map[string]json.RawMessage) string {
	var p struct {
		Default containerProxy `json:"default"`
	}
	if raw, ok := m["proxies"]; !ok || json.Unmarshal(raw, &p) != nil {
		return ""
	}
	return p.Default.HTTP
}

func (o Options) readContainers(w written) Tool {
	t := Tool{ID: Containers, State: Off, Source: SourceNone, Guard: GuardNeed, File: "~/" + dockerConfig}
	m, err := o.readDockerConfig()
	if err != nil {
		t.State = Unknown
		return t
	}
	if v := containerValue(m); v != "" {
		_, ours := w[Containers]
		o.judge(&t, v, ours, o.gateway(), "host.docker.internal")
	}
	return t
}

func (o Options) containerProxies() containerProxy {
	a := o.containerAddr()
	return containerProxy{HTTP: a, HTTPS: a, No: "localhost,127.0.0.1"}
}

// ---- zsh ----

const shellFile = ".zshenv"

func (o Options) shellBlock() []string {
	a := fmt.Sprintf("http://127.0.0.1:%d", o.Port)
	no := o.noProxy("127.0.0.1", "::1")
	return wrap("",
		fmt.Sprintf("if %s 2>/dev/null; then", o.listening()),
		fmt.Sprintf(`  export http_proxy=%[1]s https_proxy=%[1]s HTTP_PROXY=%[1]s HTTPS_PROXY=%[1]s no_proxy="%[2]s" NO_PROXY="%[2]s"`, a, no),
		"fi")
}

var blockPort = regexp.MustCompile(`127\.0\.0\.1[: ](\d+)`)

func blockPortOf(lines []string) int {
	m := blockPort.FindStringSubmatch(strings.Join(lines, "\n"))
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// shellValue asks an interactive zsh for its proxy. The mark tells the
// answer from what a .zshrc prints.
func (o Options) shellValue(ctx context.Context) (string, bool) {
	name := o.shell()
	if name == "" {
		return "", false
	}
	sh := find(name)
	if sh == "" {
		return "", false
	}
	c, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out, err := o.cmd(c, sh, "-ic", `printf '%s\n' "__BOWEL__${https_proxy:-$HTTPS_PROXY}"`).Output()
	if err != nil && len(out) == 0 {
		return "", false
	}
	if i := strings.LastIndex(string(out), "__BOWEL__"); i >= 0 {
		v, _, _ := strings.Cut(string(out)[i+len("__BOWEL__"):], "\n")
		return strings.TrimSpace(v), true
	}
	return "", false
}

func (o Options) readShell(ctx context.Context) Tool {
	rel := o.shellRel()
	if rel == "" { // a shell bowel writes nothing for (fish, say)
		return Tool{ID: Shell, State: Unknown, Source: SourceNone, Guard: GuardAuto}
	}
	t := Tool{ID: Shell, State: Off, Source: SourceNone, Guard: GuardAuto, File: "~/" + rel}
	text, err := read(o.path(rel))
	if err != nil {
		t.State = Unknown
		return t
	}
	if b := block(text); b != nil {
		t.Source, t.Applied, t.Port = SourceBowel, AppliedProxy, blockPortOf(b)
		t.State = On
		if t.Port != o.Port {
			t.State = Stale
		}
		return t
	}
	v, ok := o.shellValue(ctx)
	if !ok {
		t.State = Unknown
		return t
	}
	if v != "" {
		o.judge(&t, v, false, "127.0.0.1", "localhost")
	}
	return t
}

// following is the state of a tool that does as the shell does.
func following(id string, shell Tool) Tool {
	return Tool{ID: id, State: shell.State, Source: SourceFollow, Value: shell.Value}
}

func hasAnyOf(names ...string) bool {
	return slices.ContainsFunc(names, func(n string) bool { return find(n) != "" })
}

// ---- Git ----

const gitFile = ".gitconfig"

func directBlock() []string {
	return wrap("", `[http "https://github.com"]`, `  proxy = ""`)
}

func (o Options) readGitHTTPS(ctx context.Context, shell Tool) Tool {
	t := following(GitHTTPS, shell)
	t.File = "~/" + gitFile
	text, err := read(o.path(gitFile))
	if err != nil {
		t.State = Unknown
		return t
	}
	if block(text) != nil {
		t.State, t.Source, t.Applied, t.Value = Off, SourceBowel, AppliedDirect, ""
		return t
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := o.cmd(c, git(), "config", "--global", "--get-urlmatch", "http.proxy", "https://github.com").Output()
	if err != nil { // exit 1: not set
		return t
	}
	v := strings.TrimSpace(string(out))
	t.Value = v
	if v == "" {
		t.State, t.Source = Off, SourceOwn
		return t
	}
	o.judge(&t, v, false, "127.0.0.1", "localhost")
	return t
}

const sshFile = ".ssh/config"

func (o Options) sshBlock() []string {
	nc := o.netcat()
	proxy := fmt.Sprintf("nc -X 5 -x 127.0.0.1:%d %%h %%p", o.Port)
	if nc == ncatKind {
		proxy = fmt.Sprintf("ncat --proxy-type socks5 --proxy 127.0.0.1:%d %%h %%p", o.Port)
	}
	return wrap("",
		fmt.Sprintf(`Match host github.com exec "%s"`, o.listeningWith(nc)),
		"  ProxyCommand "+proxy,
		"Match all")
}

func (o Options) readGitSSH(ctx context.Context) Tool {
	t := Tool{ID: GitSSH, State: Off, Source: SourceNone, Guard: GuardAuto, File: "~/" + sshFile}
	text, err := read(o.path(sshFile))
	if err != nil {
		t.State = Unknown
		return t
	}
	if b := block(text); b != nil {
		t.Source, t.Applied, t.Port = SourceBowel, AppliedProxy, blockPortOf(b)
		t.State = On
		if t.Port != o.Port {
			t.State = Stale
		}
		return t
	}
	ssh := find("ssh")
	if ssh == "" {
		t.State = Unknown
		return t
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	args := []string{"-G", "github.com"}
	if text != "" {
		args = append([]string{"-F", o.path(sshFile)}, args...)
	}
	out, err := o.cmd(c, ssh, args...).Output()
	if err != nil {
		t.State = Unknown
		return t
	}
	for _, l := range strings.Split(string(out), "\n") {
		k, v, _ := strings.Cut(l, " ")
		if (k == "proxycommand" || k == "proxyjump") && v != "none" && v != "" {
			t.Source, t.State, t.Value = SourceOwn, Other, v
			if strings.Contains(v, fmt.Sprintf(":%d", o.Port)) {
				t.State = On
			}
		}
	}
	return t
}

// Detect reads every tool found on this machine. On Windows there is
// nothing yet that bowel knows how to connect, and nothing is read.
func Detect(ctx context.Context, o Options) []Tool {
	if !Supported() {
		return nil
	}
	var out []Tool
	w := o.loadWritten()
	if o.colimaInstalled() {
		out = append(out, o.readDockerd(ctx, w), o.readContainers(w))
	}
	shell := o.readShell(ctx)
	out = append(out, shell)
	if hasAnyOf("go", "pip3", "brew") {
		out = append(out, following(Langs, shell))
	}
	if git() != "" {
		out = append(out, o.readGitHTTPS(ctx, shell))
	}
	return append(out, o.readGitSSH(ctx))
}

// ---- Changing ----

func (o Options) writeBlock(rel string, place func(string, []string) string, lines []string, mode os.FileMode) error {
	text, err := read(o.path(rel))
	if err != nil {
		return err
	}
	return write(o.path(rel), place(strip(text), lines), mode)
}

// Apply points a tool at the proxy, replacing what bowel wrote before.
func Apply(o Options, id string) error {
	if !Supported() {
		return errUnsupported
	}
	switch id {
	case Dockerd:
		text, err := read(o.path(colimaConfig))
		if err != nil {
			return err
		}
		out, err := setColimaProxies(text, o.colimaBlock())
		if err != nil {
			return err
		}
		var f colimaFile
		if err := yaml.Unmarshal([]byte(out), &f); err != nil {
			return fmt.Errorf("colima.yaml: %w", err)
		}
		if err := write(o.path(colimaConfig), out, 0o644); err != nil {
			return err
		}
		return o.remember(id)
	case Containers:
		m, err := o.readDockerConfig()
		if err != nil {
			return err
		}
		if _, ours := o.loadWritten()[id]; !ours && containerValue(m) != "" {
			return fmt.Errorf("~/%s already has its own proxies", dockerConfig)
		}
		raw, err := json.Marshal(map[string]containerProxy{"default": o.containerProxies()})
		if err != nil {
			return err
		}
		m["proxies"] = raw
		if err := o.writeDockerConfig(m); err != nil {
			return err
		}
		return o.remember(id)
	case Shell:
		rel := o.shellRel()
		if rel == "" {
			return errors.New(i18n.T("bowel 只会接入 zsh 和 bash 的终端环境", "bowel only connects the terminals of zsh and bash"))
		}
		if o.netcat() == "" && o.shell() != "bash" {
			return errNoNetcat()
		}
		return o.writeBlock(rel, appended, o.shellBlock(), 0o644)
	case GitSSH:
		if o.netcat() == "" {
			return errNoNetcat()
		}
		// At the top: ssh takes the first value it meets.
		return o.writeBlock(sshFile, prepended, o.sshBlock(), 0o600)
	}
	return fmt.Errorf("devtools: %s has no proxy setting of its own", id)
}

// Direct takes a tool off the proxy that it would otherwise inherit.
func Direct(o Options, id string) error {
	if id != GitHTTPS {
		return fmt.Errorf("devtools: %s cannot be made direct", id)
	}
	return o.writeBlock(gitFile, appended, directBlock(), 0o644)
}

// Undo removes what bowel wrote for a tool. changed: there was something.
func Undo(o Options, id string) (changed bool, err error) {
	switch id {
	case Dockerd:
		text, err := read(o.path(colimaConfig))
		if err != nil {
			return false, err
		}
		_, ours := o.loadWritten()[id]
		if !ours && block(text) == nil {
			return false, nil
		}
		if out := dropColimaProxies(text); out != text {
			if err := write(o.path(colimaConfig), out, 0o644); err != nil {
				return false, err
			}
			changed = true
		}
		return changed, o.forget(id)
	case Containers:
		if _, ours := o.loadWritten()[id]; !ours {
			return false, nil
		}
		m, err := o.readDockerConfig()
		if err != nil {
			return false, err
		}
		if _, ok := m["proxies"]; ok {
			delete(m, "proxies")
			if err := o.writeDockerConfig(m); err != nil {
				return false, err
			}
			changed = true
		}
		return changed, o.forget(id)
	}
	// Whatever shell is the user's now, a block in the other's file is
	// bowel's too.
	rels := map[string][]string{Shell: {shellFile, bashFile}, GitHTTPS: {gitFile}, GitSSH: {sshFile}}[id]
	if rels == nil {
		return false, fmt.Errorf("devtools: unknown tool %s", id)
	}
	for _, rel := range rels {
		text, err := read(o.path(rel))
		if err != nil {
			return changed, err
		}
		out := strip(text)
		if out == text {
			continue
		}
		if err := write(o.path(rel), out, 0o644); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// UndoAll undoes every tool it can, and names those it changed.
func UndoAll(o Options) ([]string, error) {
	var changed []string
	var errs []error
	for _, id := range []string{Dockerd, Containers, Shell, GitHTTPS, GitSSH} {
		ok, err := Undo(o, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
		}
		if ok {
			changed = append(changed, id)
		}
	}
	return changed, errors.Join(errs...)
}
