//go:build windows

// Package tun prepares the core for TUN mode on Windows. Where macOS keeps a
// setuid copy of the core, Windows grants no privilege to a file: what opens
// a TUN device is the process, and the core inherits that from the app. So
// TUN works when bowel itself runs with an administrator's token, and
// Installed reports exactly that.
package tun

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"github.com/oboo/moho/internal/i18n"
)

// ErrCanceled is returned by Install and Uninstall when the user dismissed
// the administrator prompt. Windows shows none of its own, so it is what
// Admin returns when the app has no such privilege to begin with.
var ErrCanceled error = canceled{}

// canceled says it in the interface's language when it is said.
type canceled struct{}

func (canceled) Error() string {
	return i18n.T("用户取消了授权", "The authorization was canceled")
}

// dllName is the WinTUN driver: mihomo loads it to create the adapter, and
// it has to sit in the core's own directory. The official mihomo zip does
// not carry it, so the app ships it beside the bundled core.
const dllName = "wintun.dll"

var (
	elevatedOnce sync.Once
	elevatedNow  bool
)

// elevated reports whether this process holds an administrator's token.
// A process cannot gain one, so it is asked once and remembered.
func elevated() bool {
	elevatedOnce.Do(func() {
		proc := syscall.NewLazyDLL("shell32.dll").NewProc("IsUserAnAdmin")
		if err := proc.Find(); err != nil {
			return
		}
		r, _, _ := proc.Call()
		elevatedNow = r != 0
	})
	return elevatedNow
}

// Installed reports whether TUN can run from path: the copy is there, and
// this process may open a TUN device.
func Installed(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && elevated()
}

// Install copies src to dst with the WinTUN driver beside it, and says so
// when the app is not an administrator: Windows has no per-file privilege
// to grant, so the app has to be started that way.
func Install(src, dst string) error {
	_, err := InstallStep(src, dst)
	return err
}

// InstallStep does the copy and returns nothing for Admin to run: no step
// of it needs a token this process does not already have.
func InstallStep(src, dst string) (Step, error) {
	if !elevated() {
		return nil, errors.New(i18n.T(
			"TUN 模式需要管理员权限：请退出 bowel，右键「以管理员身份运行」后重试",
			"TUN mode needs administrator rights: quit bowel and start it again as an administrator"))
	}
	if err := copyFile(src, dst); err != nil {
		return nil, err
	}
	srcDLL := filepath.Join(filepath.Dir(src), dllName)
	if _, err := os.Stat(srcDLL); err != nil {
		return nil, fmt.Errorf(i18n.T("缺少 %s：它要和内核放在同一目录", "No %s: it must sit next to the core"), dllName)
	}
	if err := copyFile(srcDLL, filepath.Join(filepath.Dir(dst), dllName)); err != nil {
		return nil, err
	}
	return nil, nil
}

// Step is one command run as administrator on macOS. Windows has no
// equivalent single authorisation, so it is only the type the gateway
// package passes around.
type Step []string

// Prompt is what the administrator dialog says for TUN alone.
func Prompt() string {
	return i18n.T("bowel 需要授权以启用 TUN 模式", "bowel needs authorization to turn on TUN mode")
}

// Admin reports whether the steps can run as an administrator. Windows
// cannot raise the app's own privilege from inside it, so nothing is done
// and the user is told to restart the app that way instead: the copy the
// steps would make is already made by Install, which asks for the token.
func Admin(prompt string, steps ...Step) error {
	if len(steps) == 0 {
		return nil
	}
	if !elevated() {
		return ErrCanceled
	}
	return nil
}

// Uninstall removes dst, which is in the user's own directory and needs no
// privilege. A missing dst is not an error.
func Uninstall(dst string) error {
	err := os.Remove(dst)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// copyFile replaces dst with src, making dst's directory.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// ---- Putting the system back ----

// TUN hands the whole internet to its adapter: sing-tun adds a default route
// through the gateway below, which outranks the real one (its interface has
// the lowest metric there is). While the adapter answers, that is the point.
// Once it is gone the same route sends every packet nowhere, and the machine
// is offline in a way no setting in Moho can undo — which is why what
// follows exists.
const (
	// tunNet is the address sing-tun gives its adapter by default, as mihomo
	// configures it: 198.18.0.1/30, with .2 as the gateway. Moho never moves
	// it, so a route through it is TUN's and no one else's.
	tunNet = "198.18.0.0/30"
	// defaultBits: TUN's route is the default one, or half of it (0.0.0.0/1
	// and 128.0.0.0/1, the pair that beats a real default route everywhere).
	defaultBits = 1
)

var tunSubnet = netip.MustParsePrefix(tunNet)

var (
	iphlpapi                  = syscall.NewLazyDLL("iphlpapi.dll")
	procGetIPForwardTable2    = iphlpapi.NewProc("GetIpForwardTable2")
	procFreeMibTable          = iphlpapi.NewProc("FreeMibTable")
	procDeleteIPForwardEntry2 = iphlpapi.NewProc("DeleteIpForwardEntry2")
	dnsapi                    = syscall.NewLazyDLL("dnsapi.dll")
	procFlushResolverCache    = dnsapi.NewProc("DnsFlushResolverCache")
)

const afInet = 2 // AF_INET

// sockaddrInet is Windows' SOCKADDR_INET: an IPv4 or IPv6 address in one
// fixed-size shell. Only the IPv4 part is read here, and it starts at addr
// in both shapes.
type sockaddrInet struct {
	family   uint16
	port     uint16
	addr     [16]byte
	flowInfo uint32
	scopeID  uint32
}

// ipAddressPrefix is IP_ADDRESS_PREFIX.
type ipAddressPrefix struct {
	prefix sockaddrInet
	bits   uint8
	_      [3]byte
}

// forwardRow2 is MIB_IPFORWARD_ROW2, in order and at the offsets the API
// writes it.
type forwardRow2 struct {
	luid              uint64
	index             uint32
	dst               ipAddressPrefix
	nextHop           sockaddrInet
	siteBits          uint8
	_                 [3]byte
	validLifetime     uint32
	preferredLifetime uint32
	metric            uint32
	protocol          uint32
	loopback          uint8
	autoconfigure     uint8
	publish           uint8
	immortal          uint8
	age               uint32
	origin            uint32
}

// forwardTable2 is MIB_IPFORWARD_TABLE2: a count and that many rows.
type forwardTable2 struct {
	count uint32
	_     uint32
	rows  [1]forwardRow2
}

// forward is one row of the IPv4 routing table, as Reset needs it.
type forward struct {
	index   uint32
	dst     netip.Prefix
	nextHop netip.Addr
	row     unsafe.Pointer // the row the API may delete
}

// leftover reports whether f is a route TUN made and did not take back: one
// that sends the whole internet into the tunnel's gateway. Routes to a
// narrower prefix are not TUN's doing, and are left alone.
func leftover(f forward) bool {
	return f.dst.Bits() <= defaultBits && tunSubnet.Contains(f.nextHop)
}

// Reset takes back the routes TUN left on this machine and empties the
// resolver cache its fake IPs filled.
//
// The core does this itself when it closes TUN: sing-tun ends the session,
// closes the adapter (which takes the routes and the address with it) and
// flushes the cache. A core that is killed cannot, and on Windows every stop
// is a kill, so what it left has to be found and removed here. Called while
// TUN is on, this would take the routes a running TUN needs: call it only
// once TUN is off.
func Reset() error {
	rows, release, err := forwarding()
	if err != nil {
		return err
	}
	defer release()
	var firstErr error
	for _, f := range rows {
		if !leftover(f) {
			continue
		}
		if r, _, _ := procDeleteIPForwardEntry2.Call(uintptr(f.row)); r != 0 && firstErr == nil {
			firstErr = fmt.Errorf(i18n.T("无法删除 TUN 留下的路由（错误 %d）", "Could not remove a route TUN left behind (error %d)"), r)
		}
	}
	if err := procFlushResolverCache.Find(); err == nil {
		procFlushResolverCache.Call()
	}
	return firstErr
}

// forwarding is the IPv4 routing table, and how to free it.
func forwarding() ([]forward, func(), error) {
	var t *forwardTable2
	if r, _, _ := procGetIPForwardTable2.Call(afInet, uintptr(unsafe.Pointer(&t))); r != 0 {
		return nil, func() {}, fmt.Errorf(i18n.T("无法读取路由表（错误 %d）", "Could not read the routing table (error %d)"), r)
	}
	release := func() { procFreeMibTable.Call(uintptr(unsafe.Pointer(t))) }
	rows := make([]forward, 0, t.count)
	size := unsafe.Sizeof(forwardRow2{})
	for i := 0; i < int(t.count); i++ {
		r := (*forwardRow2)(unsafe.Add(unsafe.Pointer(&t.rows[0]), uintptr(i)*size))
		rows = append(rows, forward{
			index:   r.index,
			dst:     netip.PrefixFrom(ipv4(r.dst.prefix.addr), int(r.dst.bits)),
			nextHop: ipv4(r.nextHop.addr),
			row:     unsafe.Pointer(r),
		})
	}
	return rows, release, nil
}

// ipv4 is the address in a SOCKADDR_INET's bytes, which for AF_INET is the
// first four.
func ipv4(b [16]byte) netip.Addr { return netip.AddrFrom4([4]byte{b[0], b[1], b[2], b[3]}) }
