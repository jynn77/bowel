// Package core runs the mihomo core as a child process (a sidecar) and
// controls it through its external controller.
package core

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/nowindow"
)

type State string

const (
	Stopped  State = "stopped"
	Starting State = "starting"
	Running  State = "running"
	Failed   State = "error"
)

// Status is what the UI shows about the core.
type Status struct {
	State   State  `json:"state"`
	Version string `json:"version,omitempty"`
	// Error is why the core failed or, while it runs, the proxy port it
	// could not open.
	Error     string    `json:"error,omitempty"`
	PID       int       `json:"pid,omitempty"`
	StartedAt time.Time `json:"startedAt,omitzero"`
}

// ErrClosed is what starting a core says after Close.
var ErrClosed = errors.New("the core manager is closed")

// StartError is a core that did not come up, as against a config it
// refused or a reload that failed: the one failure another executable may
// not have. It reads as the error it holds.
type StartError struct{ Err error }

func (e *StartError) Error() string { return e.Err.Error() }
func (e *StartError) Unwrap() error { return e.Err }

const (
	stopGrace = 3 * time.Second // for the core to exit when asked
	killGrace = time.Second     // for it to be gone once killed
)

// Manager owns one mihomo process.
type Manager struct {
	Bin string // the mihomo executable
	Dir string // its home directory: config, geo data, cache
	// SafePaths are directories outside Dir the core may read files from
	// (mihomo refuses provider paths elsewhere).
	SafePaths []string

	// OnStatus and OnTraffic are called from the manager's goroutines.
	OnStatus func(Status)
	// OnListenError is called when Status's Error changes while the core
	// runs, which unlike OnStatus says the core itself did not change.
	OnListenError func()
	OnTraffic     func(mihomo.Traffic)

	// Spawn, if set, is asked first to start the core at bin some other
	// way than as a child of the app, and returns nil when bin is not its
	// to start. Windows runs the copy TUN needs through a scheduled task,
	// elevated, which a child of the app could not be. Such a core prints
	// to no one: its log is read from the controller instead.
	Spawn func(bin string) (Process, error)

	mu        sync.Mutex
	cmd       Process
	exited    chan struct{}
	client    *mihomo.Client
	status    Status
	listenErr string // the proxy port the running core could not open
	stopping  bool
	closed    bool // by Close
	cancel    context.CancelFunc

	// The controller's address: a unix socket, or TCP with a secret on
	// Windows, chosen once per run of the app.
	socket, tcpAddr, secret string

	logs logRing
}

// Process is a running core.
type Process interface {
	Pid() int
	// Wait returns once the core has exited.
	Wait() error
	// Stop asks the core to end, and may be called again to insist.
	Stop()
}

// child is the core as a child of the app.
type child struct{ cmd *exec.Cmd }

func (c child) Pid() int    { return c.cmd.Process.Pid }
func (c child) Wait() error { return c.cmd.Wait() }
func (c child) Stop()       { interrupt(c.cmd) }
func (c child) kill()       { _ = c.cmd.Process.Kill() }

// New prepares a manager; it starts nothing.
func New(bin, dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	m := &Manager{Bin: bin, Dir: dir, status: Status{State: Stopped}}
	socket := filepath.Join(dir, "mihomo.sock")
	// Unix socket paths are limited to ~104 bytes; fall back to TCP.
	if runtime.GOOS == "windows" || len(socket) > 100 {
		port, err := freePort()
		if err != nil {
			return nil, err
		}
		m.tcpAddr = "127.0.0.1:" + strconv.Itoa(port)
		m.secret = randomHex(16)
	} else {
		m.socket = socket
	}
	return m, nil
}

// ConfigPath is where the runtime config is written.
func (m *Manager) ConfigPath() string { return filepath.Join(m.Dir, "config.yaml") }

// ControllerFields are the config keys that point mihomo's controller
// where this manager expects it, overriding those of the profile.
func (m *Manager) ControllerFields() map[string]any {
	if m.socket != "" {
		return map[string]any{
			"external-controller":      "",
			"external-controller-unix": m.socket,
			"secret":                   "",
		}
	}
	return map[string]any{
		"external-controller": m.tcpAddr,
		"secret":              m.secret,
	}
}

// TCPAddr is the controller's TCP address when Moho fell back to TCP, ""
// on the unix socket.
func (m *Manager) TCPAddr() string {
	if m.socket != "" {
		return ""
	}
	return m.tcpAddr
}

func (m *Manager) newClient() *mihomo.Client {
	if m.socket != "" {
		return mihomo.NewUnix(m.socket)
	}
	return mihomo.NewTCP(m.tcpAddr, m.secret)
}

// SetBin changes the executable the next start uses (the setuid copy that
// TUN needs, once installed).
func (m *Manager) SetBin(bin string) {
	m.mu.Lock()
	m.Bin = bin
	m.mu.Unlock()
}

// Status returns the current status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	if s.State == Running {
		s.Error = m.listenErr
	}
	return s
}

// Client returns the controller client, or an error while the core is not
// running.
func (m *Manager) Client() (*mihomo.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State != Running || m.client == nil {
		return nil, errors.New(i18n.T("内核未运行", "The core is not running"))
	}
	return m.client, nil
}

func (m *Manager) setStatus(s Status) {
	m.mu.Lock()
	m.status = s
	fn := m.OnStatus
	m.mu.Unlock()
	if fn != nil {
		fn(s)
	}
}

// Test checks a config file with `mihomo -t`, without starting anything.
func (m *Manager) Test(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := nowindow.CommandContext(ctx, m.Bin, "-t", "-d", m.Dir, "-f", path)
	cmd.Env = m.env()
	setProcAttr(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(i18n.T("配置检查失败: %s", "The config check failed: %s"), lastLines(string(out), 3))
	}
	return nil
}

// Check says whether the core would accept cfg, touching nothing.
func (m *Manager) Check(cfg []byte) error {
	tmp := filepath.Join(m.Dir, "config.check.yaml")
	if err := os.WriteFile(tmp, cfg, 0o600); err != nil {
		return err
	}
	defer os.Remove(tmp)
	return m.Test(tmp)
}

// Apply writes cfg as the runtime config and makes the core use it:
// a running core reloads it, a stopped one starts.
func (m *Manager) Apply(cfg []byte) error {
	tmp := filepath.Join(m.Dir, "config.next.yaml")
	if err := os.WriteFile(tmp, cfg, 0o600); err != nil {
		return err
	}
	if err := m.Test(tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, m.ConfigPath()); err != nil {
		return err
	}
	if c, err := m.Client(); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		m.setListenErr("") // a reload tries a port again, and says so if it fails
		return c.Reload(ctx, m.ConfigPath())
	}
	return m.start()
}

// Restart stops the core and starts it again with the current config.
func (m *Manager) Restart() error {
	m.Stop()
	return m.start()
}

func (m *Manager) start() error {
	err := m.run()
	if err != nil && !errors.Is(err, ErrClosed) {
		return &StartError{err}
	}
	return err
}

func (m *Manager) run() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if m.cmd != nil {
		m.mu.Unlock()
		return nil
	}
	m.stopping = false
	m.mu.Unlock()

	if _, err := os.Stat(m.Bin); err != nil {
		err = fmt.Errorf(i18n.T("找不到 mihomo 内核 (%s)，先运行 scripts/fetch-core.sh", "No mihomo core at %s; run scripts/fetch-core.sh first"), m.Bin)
		m.setStatus(Status{State: Failed, Error: err.Error()})
		return err
	}
	killStale(filepath.Join(m.Dir, "mihomo.pid"))
	m.setListenErr("")
	if m.socket != "" {
		_ = os.Remove(m.socket)
	}
	m.setStatus(Status{State: Starting})

	var cmd Process
	var spawned bool
	pr, pw := io.Pipe()
	if m.Spawn != nil {
		p, err := m.Spawn(m.Bin)
		if err != nil {
			pw.Close()
			m.setStatus(Status{State: Failed, Error: err.Error()})
			return err
		}
		if p != nil {
			cmd, spawned = p, true
		}
	}
	if cmd == nil {
		c := nowindow.Command(m.Bin, "-d", m.Dir, "-f", m.ConfigPath())
		c.Dir = m.Dir
		c.Env = m.env()
		setProcAttr(c)
		c.Stdout, c.Stderr = pw, pw
		if err := c.Start(); err != nil {
			pw.Close()
			m.setStatus(Status{State: Failed, Error: err.Error()})
			return err
		}
		cmd = child{c}
	}
	_ = os.WriteFile(filepath.Join(m.Dir, "mihomo.pid"), []byte(strconv.Itoa(cmd.Pid())), 0o600)
	go m.readLogs(pr)

	exited := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cmd, m.exited, m.cancel = cmd, exited, cancel
	closed := m.closed
	m.mu.Unlock()

	go func() {
		err := cmd.Wait()
		pw.Close()
		cancel()
		// The status changes before exited closes, so that Stop and start
		// see it once they return.
		defer close(exited)
		m.mu.Lock()
		stopping := m.stopping
		m.cmd, m.client = nil, nil
		m.mu.Unlock()
		_ = os.Remove(filepath.Join(m.Dir, "mihomo.pid"))
		if stopping {
			m.setStatus(Status{State: Stopped})
		} else {
			msg := i18n.T("内核意外退出", "The core quit unexpectedly")
			if err != nil {
				msg += ": " + err.Error()
			}
			if l := m.logs.last(2); l != "" {
				msg += "\n" + l
			}
			m.setStatus(Status{State: Failed, Error: msg})
		}
	}()

	if closed { // Close came while this was starting, and saw no process
		m.Stop()
		return ErrClosed
	}

	// Wait for the controller.
	client := m.newClient()
	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case <-exited:
			return errors.New(m.Status().Error)
		default:
		}
		c, cc := context.WithTimeout(ctx, time.Second)
		v, err := client.Version(c)
		if err == nil {
			err = proxiesLoaded(c, client)
		}
		cc()
		if err == nil {
			m.mu.Lock()
			m.client = client
			m.mu.Unlock()
			m.setStatus(Status{State: Running, Version: v.Version, PID: cmd.Pid(), StartedAt: time.Now()})
			if spawned {
				go m.streamLogs(ctx, client, pw)
			}
			if m.OnTraffic != nil { // a stream no one reads costs a decode a second
				go m.traffic(ctx, client)
			}
			return nil
		}
		if time.Now().After(deadline) {
			m.Stop()
			err := errors.New(i18n.T("内核启动超时: ", "The core took too long to start: ") + m.logs.last(2))
			m.setStatus(Status{State: Failed, Error: err.Error()})
			return err
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// proxiesLoaded reports whether the core has built its proxies and groups.
// mihomo opens its controller before it applies the config, so /version
// answers while /proxies is still empty and selecting a group fails with
// "Resource not found".
func proxiesLoaded(ctx context.Context, c *mihomo.Client) error {
	ps, err := c.Proxies(ctx)
	if err != nil {
		return err
	}
	if _, ok := ps["GLOBAL"]; !ok {
		return errors.New("proxies not loaded yet")
	}
	return nil
}

func (m *Manager) traffic(ctx context.Context, c *mihomo.Client) {
	for ctx.Err() == nil {
		_ = c.StreamTraffic(ctx, func(t mihomo.Traffic) {
			if fn := m.OnTraffic; fn != nil {
				fn(t)
			}
		})
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

// Stop ends the core and waits for it to exit: stopGrace to go by itself,
// then killGrace once killed. A process that outlasts both is given up on,
// and the next start's killStale takes care of it.
func (m *Manager) Stop() {
	m.mu.Lock()
	cmd, exited, cancel := m.cmd, m.exited, m.cancel
	m.stopping = true
	m.mu.Unlock()
	if cmd == nil {
		return
	}
	cancel()
	cmd.Stop()
	select {
	case <-exited:
		return
	case <-time.After(stopGrace):
	}
	if c, ok := cmd.(child); ok {
		c.kill()
	} else {
		cmd.Stop()
	}
	select {
	case <-exited:
	case <-time.After(killGrace):
	}
}

// Close stops the core for good: nothing starts it again, even an Apply
// under way. For when the app quits.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.Stop()
}

// Logs returns the latest lines the core printed.
func (m *Manager) Logs() []string { return m.logs.all() }

// Subscribe calls fn with every new line until the returned function is
// called.
func (m *Manager) Subscribe(fn func(string)) (off func()) { return m.logs.subscribe(fn) }

// streamLogs writes what the controller says of a core that prints to no
// one where readLogs reads it, until ctx ends (the core exited or is being
// stopped). What the core said before its controller answered is lost.
func (m *Manager) streamLogs(ctx context.Context, c *mihomo.Client, w io.Writer) {
	for ctx.Err() == nil {
		_ = c.StreamLogs(ctx, "info", func(l mihomo.Log) {
			fmt.Fprintf(w, "level=%s msg=%q\n", l.Type, l.Payload)
		})
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

func (m *Manager) readLogs(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		m.logs.add(sc.Text())
		m.noteListenError(sc.Text())
	}
}

// proxyListenFailed is what mihomo logs for a proxy port it cannot open. It
// goes on without the port, its controller answering as usual, so this line
// is all there is to tell that nothing is listening. The DNS listener and
// the like are not the proxy, and have their own line.
var proxyListenFailed = regexp.MustCompile(`(?:Start (?:Mixed\(http\+socks\)|HTTP|SOCKS) server error|Listener bowel-port-\d+ listen err): listen \S+ \S*:(\d+): bind:`)

func (m *Manager) noteListenError(line string) {
	if g := proxyListenFailed.FindStringSubmatch(line); g != nil {
		m.setListenErr(fmt.Sprintf(i18n.T("端口 %s 被其他程序占用或被系统保留，代理没有在监听。关掉那个程序后重启 bowel，或换一个端口", "Port %s is taken by another program or reserved by the system, so the proxy isn't listening. Close that program and restart bowel, or choose another port"), g[1]))
	}
}

func (m *Manager) setListenErr(msg string) {
	m.mu.Lock()
	changed := m.listenErr != msg
	m.listenErr = msg
	fn := m.OnListenError
	m.mu.Unlock()
	if changed && fn != nil {
		fn()
	}
}

// logRing keeps the last lines and fans them out.
type logRing struct {
	mu    sync.Mutex
	lines []string
	subs  map[int]func(string)
	next  int
}

const maxLines = 1000

func (r *logRing) add(line string) {
	r.mu.Lock()
	r.lines = append(r.lines, line)
	if len(r.lines) > maxLines {
		r.lines = r.lines[len(r.lines)-maxLines:]
	}
	subs := make([]func(string), 0, len(r.subs))
	for _, fn := range r.subs {
		subs = append(subs, fn)
	}
	r.mu.Unlock()
	for _, fn := range subs {
		fn(line)
	}
}

func (r *logRing) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

func (r *logRing) last(n int) string {
	l := r.all()
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

func (r *logRing) subscribe(fn func(string)) func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.subs == nil {
		r.subs = map[int]func(string){}
	}
	id := r.next
	r.next++
	r.subs[id] = fn
	return func() {
		r.mu.Lock()
		delete(r.subs, id)
		r.mu.Unlock()
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (m *Manager) env() []string {
	env := os.Environ()
	if len(m.SafePaths) > 0 {
		env = append(env, "SAFE_PATHS="+strings.Join(m.SafePaths, string(os.PathListSeparator)))
	}
	return env
}

// FreePort returns a loopback TCP port nothing listens on now.
func FreePort() (int, error) { return freePort() }

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
