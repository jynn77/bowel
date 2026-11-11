package netif

import (
	"context"
	"net/netip"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Adapter is a network adapter as Windows has it. Name is the one the
// user sees ("Wi-Fi", "Tailscale"), which is also what Go's net package
// and the core call the interface.
type Adapter struct {
	Name      string
	Index     uint32
	Type      uint32 // IF_TYPE_*
	Up        bool
	DHCP      bool
	Metric    int // the interface's own IPv4 metric, added to its routes'
	DNSSuffix string
	DNS       []string
	Gateways  []string
}

const (
	ifTypePropVirtual = 53 // Wintun, WireGuard, OpenVPN's and most VPNs' adapters
	ifTypeEthernet    = windows.IF_TYPE_ETHERNET_CSMACD
	ifTypeWiFi        = windows.IF_TYPE_IEEE80211

	adapterDHCP = 0x4 // IP_ADAPTER_DHCP_ENABLED
)

// tunnelType says whether an adapter of this type is a tunnel.
func tunnelType(t uint32) bool {
	return t == ifTypePropVirtual || t == windows.IF_TYPE_TUNNEL || t == windows.IF_TYPE_PPP
}

var cache struct {
	sync.Mutex
	at   time.Time
	list []Adapter
}

// Adapters lists the adapters, from a reading at most two seconds old:
// one look at the network asks about many interfaces.
func Adapters() []Adapter {
	cache.Lock()
	defer cache.Unlock()
	if time.Since(cache.at) > 2*time.Second {
		cache.list, cache.at = readAdapters(), time.Now()
	}
	return cache.list
}

func readAdapters() []Adapter {
	const flags = windows.GAA_FLAG_SKIP_ANYCAST | windows.GAA_FLAG_SKIP_MULTICAST | windows.GAA_FLAG_INCLUDE_GATEWAYS
	size := uint32(16 * 1024)
	var buf []byte
	for range 4 {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if err != windows.ERROR_BUFFER_OVERFLOW || size == 0 {
			return nil
		}
		buf = nil
	}
	if buf == nil {
		return nil
	}
	var out []Adapter
	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		ad := Adapter{
			Name: windows.UTF16PtrToString(a.FriendlyName), Index: a.IfIndex, Type: a.IfType,
			Up: a.OperStatus == windows.IfOperStatusUp, DHCP: a.Flags&adapterDHCP != 0, Metric: int(a.Ipv4Metric),
			DNSSuffix: windows.UTF16PtrToString(a.DnsSuffix),
		}
		if ad.Index == 0 {
			ad.Index = a.Ipv6IfIndex
		}
		for d := a.FirstDnsServerAddress; d != nil; d = d.Next {
			if ip, ok := netip.AddrFromSlice(d.Address.IP()); ok {
				ad.DNS = append(ad.DNS, ip.Unmap().String())
			}
		}
		for g := a.FirstGatewayAddress; g != nil; g = g.Next {
			if ip, ok := netip.AddrFromSlice(g.Address.IP()); ok {
				ad.Gateways = append(ad.Gateways, ip.Unmap().String())
			}
		}
		out = append(out, ad)
	}
	return out
}

// ByName is the adapter called name.
func ByName(name string) (Adapter, bool) {
	for _, a := range Adapters() {
		if a.Name == name {
			return a, true
		}
	}
	return Adapter{}, false
}

// Tunnel says whether Windows has name as a tunnel's adapter.
func Tunnel(name string) bool {
	a, ok := ByName(name)
	return ok && tunnelType(a.Type)
}

// Physical says whether name is an Ethernet or Wi-Fi adapter. Virtual
// switches (Hyper-V's, WSL's) are Ethernet to Windows too.
func Physical(name string) bool {
	a, ok := ByName(name)
	return ok && (a.Type == ifTypeEthernet || a.Type == ifTypeWiFi)
}

// WiFi says whether name is a wireless adapter.
func WiFi(name string) bool {
	a, ok := ByName(name)
	return ok && a.Type == ifTypeWiFi
}

// Leased says whether name gets its address from DHCP.
func Leased(_ context.Context, name string) bool {
	a, ok := ByName(name)
	return ok && a.DHCP
}

// Routes are the system's routes, IPv4 and IPv6. Windows has one table.
func Routes(context.Context) []Route {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_UNSPEC, &table); err != nil || table == nil {
		return nil
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	byIndex := map[uint32]Adapter{}
	for _, a := range Adapters() {
		byIndex[a.Index] = a
	}
	var out []Route
	for _, row := range table.Rows() {
		a, ok := byIndex[row.InterfaceIndex]
		if !ok || !a.Up || row.Loopback != 0 || a.Type == windows.IF_TYPE_SOFTWARE_LOOPBACK {
			continue
		}
		dst, ok := sockaddr(&row.DestinationPrefix.Prefix)
		if !ok {
			continue
		}
		r := Route{Iface: a.Name, Metric: int(row.Metric) + a.Metric, Main: true}
		if gw, ok := sockaddr(&row.NextHop); ok && !gw.IsUnspecified() {
			r.Gateway = gw.String()
		}
		if bits := int(row.DestinationPrefix.PrefixLength); bits == 0 {
			r.Default = true
			if dst.Is6() {
				r.Prefix = netip.PrefixFrom(dst, 0) // tells Default it is not IPv4's
			}
		} else if p, err := dst.Prefix(bits); err == nil {
			r.Prefix = p
		} else {
			continue
		}
		out = append(out, r)
	}
	return out
}

// sockaddr reads a SOCKADDR_INET.
func sockaddr(sa *windows.RawSockaddrInet) (netip.Addr, bool) {
	switch sa.Family {
	case windows.AF_INET:
		return netip.AddrFrom4((*windows.RawSockaddrInet4)(unsafe.Pointer(sa)).Addr), true
	case windows.AF_INET6:
		return netip.AddrFrom16((*windows.RawSockaddrInet6)(unsafe.Pointer(sa)).Addr), true
	}
	return netip.Addr{}, false
}

// DefaultRoute is the adapter and gateway of the IPv4 default route off
// any tunnel, "" when there is none.
func DefaultRoute() (iface, gateway string) {
	return Default(Routes(context.Background()), Tunnel)
}
