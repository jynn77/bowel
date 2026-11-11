package sysproxy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oboo/moho/internal/i18n"
)

// DefaultBypass is GNOME's own list, which KDE's has no equal of: what a
// desktop gets back when Forget clears a list that was all bowel's.
var DefaultBypass = []string{"localhost", "127.0.0.0/8", "::1"}

// perCall bounds one gsettings or kwriteconfig: a stuck one must not hold
// up quitting or a change of settings.
const perCall = 5 * time.Second

func output(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, perCall)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%s %s: %s", filepath.Base(name), strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	return string(out), nil
}

// desktop is one desktop's proxy settings.
type desktop interface {
	name() string
	read(ctx context.Context) (Service, error)
	write(ctx context.Context, sv Service) error
}

// ---- GNOME ----

type gnome struct{ bin string }

func (gnome) name() string { return gnomeName }

func (g gnome) read(ctx context.Context) (Service, error) {
	out, err := output(ctx, g.bin, "list-recursively", gnomeSchema)
	if err != nil {
		return Service{}, err
	}
	return parseGSettings(out).service(), nil
}

func (g gnome) write(ctx context.Context, sv Service) error {
	for _, set := range gnomeSets(sv) {
		if _, err := output(ctx, g.bin, append([]string{"set"}, set...)...); err != nil {
			return err
		}
	}
	return nil
}

// ---- KDE ----

type kde struct {
	bin  string // kwriteconfig6 or kwriteconfig5
	file string // kioslaverc
}

func (kde) name() string { return kdeName }

func (k kde) read(context.Context) (Service, error) {
	b, err := os.ReadFile(k.file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Service{}, err
	}
	return parseKioslaverc(string(b)).service(), nil
}

func (k kde) write(ctx context.Context, sv Service) error {
	for _, kv := range kdeSets(sv) {
		if _, err := output(ctx, k.bin, "--file", kdeFile, "--group", kdeGroup, "--key", kv[0], kv[1]); err != nil {
			return err
		}
	}
	// Running programs read the file again when told to.
	if dbus, err := exec.LookPath("dbus-send"); err == nil {
		_, _ = output(ctx, dbus, "--type=signal", "/KIO/Scheduler", "org.kde.KIO.Scheduler.reparseSlaveConfiguration", "string:")
	}
	return nil
}

func configHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

// isKDE says whether the session is Plasma's.
func isKDE() bool {
	return strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "KDE") || os.Getenv("KDE_FULL_SESSION") != ""
}

// found remembers which desktops a session has: the answer is asked for
// every few seconds, and takes a command to get.
var found struct {
	sync.Mutex
	key  string
	list []desktop
}

// desktops are the settings this session has, its own desktop's first:
// GNOME's where the schema is installed, KDE's in a Plasma session.
// Check goes by the first; the rest are kept in step with it.
func desktops(ctx context.Context) []desktop {
	key := strings.Join([]string{os.Getenv("PATH"), os.Getenv("XDG_CURRENT_DESKTOP"), os.Getenv("KDE_FULL_SESSION"), configHome()}, "\x00")
	found.Lock()
	defer found.Unlock()
	if found.key == key && found.list != nil {
		return found.list
	}
	var out []desktop
	if isKDE() {
		for _, name := range []string{"kwriteconfig6", "kwriteconfig5"} {
			if bin, err := exec.LookPath(name); err == nil {
				out = append(out, kde{bin: bin, file: filepath.Join(configHome(), kdeFile)})
				break
			}
		}
	}
	if bin, err := exec.LookPath("gsettings"); err == nil {
		if _, err := output(ctx, bin, "get", gnomeSchema, "mode"); err == nil {
			out = append(out, gnome{bin: bin})
		}
	}
	// A session found to have none is asked again: its bus may not have
	// been up yet.
	found.key, found.list = key, out
	return out
}

var errNoDesktop error = noDesktop{}

type noDesktop struct{}

func (noDesktop) Error() string {
	return i18n.T("这个桌面没有 bowel 能设置的系统代理（支持 GNOME 一系和 KDE）。可以改用 TUN，或者在「开发工具」里接入终端",
		"This desktop has no system proxy bowel can set (GNOME's family and KDE are supported). Use TUN instead, or connect the terminal under Developer tools")
}

// Enable points every desktop's proxies at host:port, with the hosts and
// networks in bypass left out of them. A script in use is turned off,
// which would come before them; Restore turns it back on.
func Enable(host string, port int, bypass []string) error {
	ctx := context.Background()
	ds := desktops(ctx)
	if len(ds) == 0 {
		return errNoDesktop
	}
	for _, d := range ds {
		cur, err := d.read(ctx)
		if err != nil {
			return err
		}
		if err := d.write(ctx, pointed(cur, host, port, bypass)); err != nil {
			return err
		}
	}
	return nil
}

// Disable turns the proxies of every desktop off and leaves the rest as
// it is. It goes on past a failure, and returns them all.
func Disable() error {
	ctx := context.Background()
	var errs []error
	for _, d := range desktops(ctx) {
		cur, err := d.read(ctx)
		if err == nil {
			err = d.write(ctx, off(cur))
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Snapshot reads every desktop's proxies and bypass list. It changes
// nothing.
func Snapshot() (Saved, error) {
	ctx := context.Background()
	var saved Saved
	for _, d := range desktops(ctx) {
		sv, err := d.read(ctx)
		if err != nil {
			return Saved{}, err
		}
		saved.Services = append(saved.Services, sv)
	}
	return saved, nil
}

// Restore puts every desktop's settings back as saved has them; one that
// saved does not have gets its proxies turned off.
func Restore(ctx context.Context, saved Saved) error {
	var errs []error
	for _, d := range desktops(ctx) {
		var want *Service
		for i := range saved.Services {
			if saved.Services[i].Name == d.name() {
				want = &saved.Services[i]
			}
		}
		if want == nil {
			cur, err := d.read(ctx)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			o := off(cur)
			want = &o
		}
		errs = append(errs, d.write(ctx, *want))
	}
	return errors.Join(errs...)
}

// Check reads the session's own desktop's proxy and tells whether it
// points at the core's mixed port. It changes nothing. A session with no
// desktop bowel knows reads as off.
func Check(port int) (Status, error) {
	ctx := context.Background()
	ds := desktops(ctx)
	if len(ds) == 0 {
		return Status{}, nil
	}
	sv, err := ds[0].read(ctx)
	if err != nil {
		return Status{}, err
	}
	return serviceStatus(sv, port), nil
}
