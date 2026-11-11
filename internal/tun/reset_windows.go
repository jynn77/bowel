//go:build windows

package tun

import (
	"fmt"
	"net/netip"
	"syscall"
	"unsafe"

	"github.com/oboo/moho/internal/i18n"
)

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
