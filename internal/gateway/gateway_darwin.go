package gateway

import (
	"bufio"
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

	"golang.org/x/sys/unix"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/tun"
)

// OS is macOS.
//
// Forwarding is changed through sudo, with a rule in /etc/sudoers.d that
// lets this user run exactly the two sysctl commands, and nothing else,
// without a password. Prepare installs the rule (and checks it with
// visudo first), so the administrator dialog shows once, together with
// TUN's when TUN is not installed yet. The rule is a file of its own:
// deleting it undoes it.
func OS() System { return &darwin{uid: os.Getuid()} }

type darwin struct {
	uid int

	mu       sync.Mutex
	slowAt   time.Time // when wifi and firewall were read
	wifi     map[string]bool
	firewall bool
}

// Supported says whether this system can be made a gateway.
func Supported() bool { return true }

// onLAN says whether an interface can be on the LAN, which on macOS its
// name tells.
func onLAN(name string) bool { return physical(name) }

const sysctlName = "net.inet.ip.forwarding"

func (d *darwin) Forwarding() (bool, error) {
	v, err := unix.SysctlUint32(sysctlName)
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

func (d *darwin) rulePath() string {
	return fmt.Sprintf("/private/etc/sudoers.d/bowel-gateway-%d", d.uid)
}

func (d *darwin) Ready() bool { return ruleInstalled(d.rulePath()) }

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

// forwardCmd is what the rule allows and SetForwarding runs.
func forwardCmd(on bool) []string {
	v := "0"
	if on {
		v = "1"
	}
	return []string{"/usr/sbin/sysctl", "-w", sysctlName + "=" + v}
}

// rule is the sudoers rule for uid. A user is named by "#uid", so that no
// name needs escaping.
func rule(uid int) string {
	return fmt.Sprintf("# Written by bowel for its gateway mode: lets this user turn IPv4\n"+
		"# forwarding on and off without a password. Delete the file to undo it.\n"+
		"#%d ALL = (root) NOPASSWD: %s, %s\n", uid, strings.Join(forwardCmd(false), " "), strings.Join(forwardCmd(true), " "))
}

func (d *darwin) SetForwarding(on bool) error {
	args := append([]string{"-n"}, forwardCmd(on)...)
	out, err := exec.Command("/usr/bin/sudo", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf(i18n.T("无法更改 IP 转发：%s", "Couldn't change IP forwarding: %s"), msg)
	}
	if cur, err := d.Forwarding(); err == nil && cur != on {
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
		{"/usr/bin/install -o root -g wheel -m 0440 ", tmp, " ", stage},
		{"/usr/sbin/visudo -cqf ", stage},
		{"/bin/mv -f ", stage, " ", dst},
	}
}

func (d *darwin) Prepare(prompt string, before ...tun.Step) error {
	f, err := os.CreateTemp("", "bowel-gateway-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(rule(d.uid))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	steps := append(append([]tun.Step{}, before...), prepareSteps(f.Name(), d.rulePath())...)
	if err := tun.Admin(prompt, steps...); err != nil {
		return err
	}
	if !d.Ready() {
		return errors.New(i18n.T("授权后仍然无法更改 IP 转发", "IP forwarding still can't be changed after the authorization"))
	}
	return nil
}

// KeepAwake runs caffeinate for as long as bowel runs: -i keeps the
// system from idle sleep, -s from sleep altogether on power. It ends by
// itself if bowel goes away without stopping it.
func (d *darwin) KeepAwake() (func(), error) {
	cmd := exec.Command("/usr/bin/caffeinate", "-i", "-s", "-w", strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	return func() {
		_ = cmd.Process.Kill()
		<-done
	}, nil
}

func (d *darwin) AnswersDNS(addr string) bool { return answersDNS(addr, time.Second) }

func (d *darwin) Network() Network {
	n := Network{}
	// The system's primary interface, unless it is a tunnel: TUN (bowel's
	// own, or another VPN's) is often primary, and not on the LAN.
	if iface, router := primary(); physical(iface) {
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
	if n.Router == "" {
		if out, err := exec.Command("/usr/sbin/ipconfig", "getoption", n.Iface, "router").Output(); err == nil {
			n.Router = strings.TrimSpace(string(out))
		}
	}
	d.mu.Lock()
	if time.Since(d.slowAt) > 30*time.Second {
		d.wifi, d.firewall, d.slowAt = wifiPorts(), firewallOn(), time.Now()
	}
	n.WiFi, n.Firewall = d.wifi[n.Iface], d.firewall
	d.mu.Unlock()
	n.DHCP = leased(n.Iface)
	return n
}

// primary is the interface and router of the network the Mac uses, as
// the system's configuration has them.
func primary() (iface, router string) {
	cmd := exec.Command("/usr/sbin/scutil")
	cmd.Stdin = strings.NewReader("show State:/Network/Global/IPv4\n")
	out, err := cmd.Output()
	if err != nil {
		return "", ""
	}
	return parseGlobal(string(out))
}

// PrimaryInterface is the interface the Mac reaches the network through,
// "" if it has none.
func PrimaryInterface() string {
	iface, _ := primary()
	return iface
}

func parseGlobal(s string) (iface, router string) {
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), " : ")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "PrimaryInterface":
			iface = strings.TrimSpace(v)
		case "Router":
			router = strings.TrimSpace(v)
		}
	}
	return iface, router
}

// wifiPorts are the devices of the Mac's Wi-Fi hardware ports.
func wifiPorts() map[string]bool {
	out, err := exec.Command("/usr/sbin/networksetup", "-listallhardwareports").Output()
	if err != nil {
		return map[string]bool{}
	}
	return parsePorts(string(out))
}

func parsePorts(s string) map[string]bool {
	m := map[string]bool{}
	wifi := false
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "Hardware Port: "); ok {
			wifi = v == "Wi-Fi" || v == "AirPort"
		} else if v, ok := strings.CutPrefix(line, "Device: "); ok && wifi {
			m[strings.TrimSpace(v)] = true
		}
	}
	return m
}

// leased says whether iface got its address from DHCP.
func leased(iface string) bool {
	out, err := exec.Command("/usr/sbin/ipconfig", "getpacket", iface).Output()
	return err == nil && strings.Contains(string(out), "BOOTREPLY")
}

func firewallOn() bool {
	out, err := exec.Command("/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate").Output()
	if err != nil {
		return false
	}
	// "Firewall is enabled. (State = 1)"; 2 blocks all incoming.
	s := string(out)
	return strings.Contains(s, "State = 1") || strings.Contains(s, "State = 2") || strings.Contains(s, "is enabled")
}
