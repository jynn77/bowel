package gateway

import (
	"net"
	"time"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/netif"
	"github.com/oboo/moho/internal/tun"
)

// OS is Windows, which bowel cannot make a gateway yet: it reads the
// network, for the rest of the app to use, and changes nothing.
//
// What is missing is not one call. Windows forwards per adapter, and both
// the LAN's and TUN's must be switched, each time TUN's adapter is made
// anew; nothing like a sudoers rule lets a user's process do that without
// being elevated; and the firewall drops both what arrives to be
// forwarded and the resolver's port 53 until rules are added for them.
// Each needs to be tried on Windows before it is written.
func OS() System { return windowsOS{} }

// Supported says whether this system can be made a gateway.
func Supported() bool { return false }

var errUnsupported error = unsupported{}

type unsupported struct{}

func (unsupported) Error() string {
	return i18n.T("Windows 上还不能开启网关模式", "Gateway mode can't be turned on under Windows yet")
}

type windowsOS struct{}

func (windowsOS) Forwarding() (bool, error)         { return false, nil }
func (windowsOS) SetForwarding(bool) error          { return errUnsupported }
func (windowsOS) Ready() bool                       { return false }
func (windowsOS) Prepare(string, ...tun.Step) error { return errUnsupported }
func (windowsOS) KeepAwake() (func(), error)        { return nil, errUnsupported }
func (windowsOS) AnswersDNS(addr string) bool       { return answersDNS(addr, time.Second) }

// onLAN says whether an adapter can be on the LAN: Ethernet or Wi-Fi, as
// Windows has it.
func onLAN(name string) bool { return netif.Physical(name) }

func (windowsOS) Network() Network {
	n := Network{}
	if iface, router := netif.DefaultRoute(); iface != "" {
		if ifc, err := net.InterfaceByName(iface); err == nil {
			n.IP, n.Iface, n.Router = ifaceIPv4(ifc), iface, router
		}
	}
	if n.IP == "" {
		n = scanLAN()
	}
	if a, ok := netif.ByName(n.Iface); ok {
		n.WiFi, n.DHCP = netif.WiFi(a.Name), a.DHCP
	}
	return n
}

// PrimaryInterface is the adapter the machine reaches the network
// through, "" if it has none.
func PrimaryInterface() string {
	iface, _ := netif.DefaultRoute()
	return iface
}
