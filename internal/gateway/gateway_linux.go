package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/netif"
	"github.com/oboo/moho/internal/tun"
)

// OS is Linux.
//
// Forwarding is changed through sudo, as on macOS: a rule in
// /etc/sudoers.d lets this user run exactly the two sysctl commands, and
// nothing else, without a password. Prepare installs the rule (checked
// with visudo first) under one pkexec, together with TUN's installation
// when that is not done yet. The rule is a file of its own: deleting it
// undoes it. Run as root, bowel writes the kernel's switch itself.
func OS() System { return &linux{uid: os.Getuid()} }

// Supported says whether this system can be made a gateway.
func Supported() bool { return true }

type linux struct {
	uid int

	mu       sync.Mutex
	slowAt   time.Time // when the firewall was read
	firewall bool
}

const (
	sysctlName = "net.ipv4.ip_forward"
	procSwitch = "/proc/sys/net/ipv4/ip_forward"
)

func (l *linux) Forwarding() (bool, error) {
	b, err := os.ReadFile(procSwitch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(b)) != "0", nil
}

func (l *linux) rulePath() string { return fmt.Sprintf("/etc/sudoers.d/bowel-gateway-%d", l.uid) }

func (l *linux) Ready() bool { return l.uid == 0 || ruleInstalled(l.rulePath()) }

// ruleInstalled says whether path is a regular file owned by root, as
// sudo wants its rules.
func ruleInstalled(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == 0
}

// rootBin is a command of root's by its full path, which is what a
// sudoers rule names: the first of the system's directories that has it.
func rootBin(name string) string {
	for _, d := range []string{"/usr/sbin", "/sbin", "/usr/bin", "/bin"} {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return filepath.Join("/usr/sbin", name)
}

// forwardCmd is what the rule allows and SetForwarding runs.
func forwardCmd(sysctl string, on bool) []string {
	v := "0"
	if on {
		v = "1"
	}
	return []string{sysctl, "-w", sysctlName + "=" + v}
}

// rule is the sudoers rule for uid. A user is named by "#uid", so that no
// name needs escaping.
func rule(uid int, sysctl string) string {
	return fmt.Sprintf("# Written by bowel for its gateway mode: lets this user turn IPv4\n"+
		"# forwarding on and off without a password. Delete the file to undo it.\n"+
		"#%d ALL = (root) NOPASSWD: %s, %s\n", uid, strings.Join(forwardCmd(sysctl, false), " "), strings.Join(forwardCmd(sysctl, true), " "))
}

func (l *linux) SetForwarding(on bool) error {
	var out []byte
	var err error
	if l.uid == 0 {
		v := "0\n"
		if on {
			v = "1\n"
		}
		err = os.WriteFile(procSwitch, []byte(v), 0o644)
	} else {
		args := append([]string{"-n"}, forwardCmd(rootBin("sysctl"), on)...)
		out, err = exec.Command(rootBin("sudo"), args...).CombinedOutput()
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf(i18n.T("无法更改 IP 转发：%s", "Couldn't change IP forwarding: %s"), msg)
	}
	if cur, err := l.Forwarding(); err == nil && cur != on {
		return errors.New(i18n.T("IP 转发没有改过来", "IP forwarding didn't change"))
	}
	return nil
}

// prepareSteps installs the rule written at tmp: it goes beside the
// others under a name sudo skips (it has a dot), is checked there, where
// no one but root can change it any more, and only then takes its name.
func prepareSteps(tmp, dst string) []tun.Step {
	stage := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+".new")
	return []tun.Step{
		{rootBin("install") + " -o root -g root -m 0440 ", tmp, " ", stage},
		{rootBin("visudo") + " -cqf ", stage},
		{rootBin("mv") + " -f ", stage, " ", dst},
	}
}

func (l *linux) Prepare(prompt string, before ...tun.Step) error {
	if l.uid == 0 {
		return tun.Admin(prompt, before...)
	}
	if _, err := os.Stat(filepath.Dir(l.rulePath())); err != nil {
		return errors.New(i18n.T("这个系统没有 /etc/sudoers.d：网关模式要靠 sudo 来开关 IP 转发，请先安装 sudo",
			"This system has no /etc/sudoers.d: gateway mode turns IP forwarding on and off through sudo, so install sudo first"))
	}
	f, err := os.CreateTemp("", "bowel-gateway-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(rule(l.uid, rootBin("sysctl")))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	steps := append(append([]tun.Step{}, before...), prepareSteps(f.Name(), l.rulePath())...)
	if err := tun.Admin(prompt, steps...); err != nil {
		return err
	}
	if !l.Ready() {
		return errors.New(i18n.T("授权后仍然无法更改 IP 转发", "IP forwarding still can't be changed after the authorization"))
	}
	return nil
}

// KeepAwake holds a systemd inhibitor against sleep and idle for as long
// as bowel runs: the lock is the command's, which waits on bowel's
// process and ends with it. Where there is no systemd-inhibit nothing is
// held, and the gateway works until the machine sleeps.
func (l *linux) KeepAwake() (func(), error) {
	bin, err := exec.LookPath("systemd-inhibit")
	if err != nil {
		return func() {}, nil
	}
	cmd := exec.Command(bin, inhibitArgs(os.Getpid())...)
	if err := cmd.Start(); err != nil {
		return func() {}, nil
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	return func() {
		_ = cmd.Process.Kill()
		<-done
	}, nil
}

func inhibitArgs(pid int) []string {
	return []string{"--what=sleep:idle", "--who=bowel", "--why=Gateway mode: other devices reach the network through this computer",
		"--mode=block", "tail", "--pid=" + strconv.Itoa(pid), "-f", "/dev/null"}
}

func (l *linux) AnswersDNS(addr string) bool { return answersDNS(addr, time.Second) }

// onLAN says whether an interface can be on the LAN: hardware (or a
// virtual machine's), as the kernel has it. Names say nothing here, and
// Docker's bridge has a private address too.
func onLAN(name string) bool { return netif.Physical(name) }

func (l *linux) Network() Network {
	n := Network{}
	// The default route's interface off any tunnel: TUN (bowel's own, or
	// another VPN's) takes the traffic, and is not on the LAN.
	if iface, router := netif.DefaultRoute(); iface != "" {
		if ifc, err := net.InterfaceByName(iface); err == nil {
			n.IP, n.Iface, n.Router = ifaceIPv4(ifc), iface, router
		}
	}
	if n.IP == "" {
		n = scanLAN()
	}
	if n.Iface == "" {
		return n
	}
	l.mu.Lock()
	if time.Since(l.slowAt) > 30*time.Second {
		l.firewall, l.slowAt = firewallOn(), time.Now()
	}
	n.Firewall = l.firewall
	l.mu.Unlock()
	n.WiFi = netif.WiFi(n.Iface)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	n.DHCP = netif.Leased(ctx, n.Iface)
	return n
}

// PrimaryInterface is the interface the machine reaches the network
// through, "" if it has none.
func PrimaryInterface() string {
	iface, _ := netif.DefaultRoute()
	return iface
}

// firewallOn says whether a firewall that filters by default is on: ufw
// or firewalld, either of which may drop port 53 and what is forwarded.
func firewallOn() bool {
	if b, err := os.ReadFile("/etc/ufw/ufw.conf"); err == nil && ufwEnabled(string(b)) {
		return true
	}
	if bin, err := exec.LookPath("systemctl"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, bin, "is-active", "--quiet", "firewalld").Run() == nil
	}
	return false
}

func ufwEnabled(conf string) bool {
	for _, line := range strings.Split(conf, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k == "ENABLED" {
			return strings.EqualFold(strings.Trim(v, `"' `), "yes")
		}
	}
	return false
}
