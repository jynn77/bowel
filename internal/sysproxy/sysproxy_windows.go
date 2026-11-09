// Package sysproxy points the system's HTTP, HTTPS and SOCKS proxies at the
// core.
package sysproxy

// On Windows the system proxy is WinINet's, which most programs follow
// (browsers, the Store, WinHTTP clients that ask for the user's settings).
// It is read and written through InternetQueryOption and InternetSetOption
// with per-connection options, which keep the registry and Windows'
// Settings page in step, and then announced so that running programs pick
// it up. Only the LAN settings are changed: they serve every connection
// but dial-up and VPN entries that have their own.

import (
	"context"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// DefaultBypass is what a service gets back when Forget clears one that
// was all Moho's: Windows starts with none.
var DefaultBypass []string

var (
	wininet         = windows.NewLazySystemDLL("wininet.dll")
	procSetOption   = wininet.NewProc("InternetSetOptionW")
	procQueryOption = wininet.NewProc("InternetQueryOptionW")
	procGlobalFree  = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalFree")
)

const (
	optSettingsChanged = 39 // INTERNET_OPTION_SETTINGS_CHANGED
	optRefresh         = 37 // INTERNET_OPTION_REFRESH
	optPerConnection   = 75 // INTERNET_OPTION_PER_CONNECTION_OPTION

	connFlags   = 1  // INTERNET_PER_CONN_FLAGS
	connServer  = 2  // INTERNET_PER_CONN_PROXY_SERVER
	connBypass  = 3  // INTERNET_PER_CONN_PROXY_BYPASS
	connPAC     = 4  // INTERNET_PER_CONN_AUTOCONFIG_URL
	connFlagsUI = 10 // INTERNET_PER_CONN_FLAGS_UI: the flags as the user set them
)

const ptrSize = unsafe.Sizeof(uintptr(0))

// perConnOption is INTERNET_PER_CONN_OPTIONW. Its value is a union of a
// DWORD, a string and a FILETIME, eight bytes on every platform.
type perConnOption struct {
	option uint32
	value  [8 / ptrSize]uintptr // value[0] holds the DWORD or the string
}

// perConnOptionList is INTERNET_PER_CONN_OPTION_LISTW.
type perConnOptionList struct {
	size       uint32
	connection *uint16 // nil: the LAN settings
	count      uint32
	failed     uint32
	options    *perConnOption
}

// The layouts of the C structs: 16 and 32 bytes on 64-bit Windows, 12 and
// 20 on 32-bit. A mismatch fails the build.
var (
	_ [0]struct{} = [unsafe.Sizeof(perConnOption{}) - (8 + ptrSize)]struct{}{}
	_ [0]struct{} = [unsafe.Sizeof(perConnOptionList{}) - (8 + 3*ptrSize)]struct{}{}
)

// str reads a string option's value that WinINet filled in. Writes go
// through strOption.
func (o *perConnOption) str() **uint16 { return (**uint16)(unsafe.Pointer(&o.value[0])) }

func setOption(option uintptr, buf unsafe.Pointer, size uintptr) error {
	if r, _, err := procSetOption.Call(0, option, uintptr(buf), size); r == 0 {
		return fmt.Errorf("InternetSetOption %d: %w", option, err)
	}
	return nil
}

// write sets the LAN settings' options and tells running programs.
func write(opts []perConnOption, keep ...[]uint16) error {
	list := perConnOptionList{count: uint32(len(opts)), options: &opts[0]}
	list.size = uint32(unsafe.Sizeof(list))
	err := setOption(optPerConnection, unsafe.Pointer(&list), unsafe.Sizeof(list))
	runtime.KeepAlive(opts)
	runtime.KeepAlive(keep)
	if err != nil {
		return err
	}
	_ = setOption(optSettingsChanged, nil, 0)
	_ = setOption(optRefresh, nil, 0)
	return nil
}

func strOption(option uint32, s string) (perConnOption, []uint16) {
	u, err := windows.UTF16FromString(s)
	if err != nil { // a NUL in s; none of Moho's strings has one
		u = []uint16{0}
	}
	// The address goes in as the integer the field is: a store through
	// str()'s **uint16 view of a local is dropped by the compiler, and
	// WinINet got a NULL string (the proxy on, with no server). The
	// caller keeps u alive, and heap memory doesn't move.
	o := perConnOption{option: option}
	o.value[0] = uintptr(unsafe.Pointer(&u[0]))
	return o, u
}

// apply writes ws whole.
func apply(ws winSettings) error {
	server, su := strOption(connServer, ws.server)
	bypass, bu := strOption(connBypass, ws.override)
	pac, pu := strOption(connPAC, ws.pac)
	opts := []perConnOption{{option: connFlags, value: [8 / ptrSize]uintptr{uintptr(ws.flags)}}, server, bypass, pac}
	if err := write(opts, su, bu, pu); err != nil {
		// WinINet refused the per-connection write. An elevated process
		// does that on some machines, and then the switch looked like it
		// did nothing at all. The registry holds the same setting, so it
		// is written directly and announced the same way.
		return writeRegistry(ws)
	}
	return nil
}

// internetKey is where WinINet keeps the LAN settings, and where Windows'
// own settings page reads them.
const internetKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// readRegistry reads the settings the per-connection call would have given,
// for when that call fails.
func readRegistry() (winSettings, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetKey, registry.QUERY_VALUE)
	if err != nil {
		return winSettings{}, err
	}
	defer k.Close()
	ws := winSettings{flags: winDirect}
	if d, _, err := k.GetIntegerValue("ProxyEnable"); err == nil && d != 0 {
		ws.flags |= winProxy
	}
	ws.server, _, _ = k.GetStringValue("ProxyServer")
	ws.override, _, _ = k.GetStringValue("ProxyOverride")
	ws.pac, _, _ = k.GetStringValue("AutoConfigURL")
	// Whether the script is switched on lives in the blob alone: a URL by
	// itself is not taken as on, so that this reading never calls a proxy
	// that is ours "hijacked".
	return ws, nil
}

// writeRegistry writes the settings without WinINet.
func writeRegistry(ws winSettings) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, internetKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("writing the system proxy: %w", err)
	}
	defer k.Close()
	var on uint32
	if ws.flags&winProxy != 0 {
		on = 1
	}
	if err := k.SetDWordValue("ProxyEnable", on); err != nil {
		return err
	}
	for _, v := range []struct{ name, value string }{
		{"ProxyServer", ws.server},
		{"ProxyOverride", ws.override},
		{"AutoConfigURL", ws.pac},
	} {
		if v.value == "" {
			_ = k.DeleteValue(v.name) // absent, as Windows leaves it
			continue
		}
		if err := k.SetStringValue(v.name, v.value); err != nil {
			return err
		}
	}
	_ = setOption(optSettingsChanged, nil, 0)
	_ = setOption(optRefresh, nil, 0)
	return nil
}

// read reads the LAN settings, through WinINet, or from the registry when it
// will not answer.
func read() (winSettings, error) {
	ws, err := readWinINet()
	if err == nil {
		return ws, nil
	}
	return readRegistry()
}

// readWinINet reads the LAN settings.
func readWinINet() (winSettings, error) {
	query := func(flagsOption uint32) ([]perConnOption, error) {
		opts := []perConnOption{{option: flagsOption}, {option: connServer}, {option: connBypass}, {option: connPAC}}
		list := perConnOptionList{count: uint32(len(opts)), options: &opts[0]}
		list.size = uint32(unsafe.Sizeof(list))
		n := list.size
		r, _, err := procQueryOption.Call(0, optPerConnection, uintptr(unsafe.Pointer(&list)), uintptr(unsafe.Pointer(&n)))
		runtime.KeepAlive(opts)
		if r == 0 {
			return nil, fmt.Errorf("InternetQueryOption: %w", err)
		}
		return opts, nil
	}
	// FLAGS_UI is the real switch since Windows 7; older systems refuse it.
	opts, err := query(connFlagsUI)
	if err != nil {
		if opts, err = query(connFlags); err != nil {
			return winSettings{}, err
		}
	}
	take := func(o *perConnOption) string {
		p := *o.str()
		if p == nil {
			return ""
		}
		s := windows.UTF16PtrToString(p)
		procGlobalFree.Call(o.value[0])
		return s
	}
	return winSettings{
		flags:    uint32(opts[0].value[0]),
		server:   take(&opts[1]),
		override: take(&opts[2]),
		pac:      take(&opts[3]),
	}, nil
}

// Enable points the LAN settings' proxies at host:port, with the hosts and
// networks in bypass left out of them. It clears any automatic configuration
// the machine had: a script comes before the fixed proxies, and one left in
// place is read back as someone else's proxy — which the app then reports as
// hijacked, after its own write. Snapshot keeps the script, so Restore puts
// it back.
func Enable(host string, port int, bypass []string) error {
	if _, err := read(); err != nil {
		return err
	}
	e := Endpoint{On: true, Host: host, Port: port}
	return apply(winSettings{
		flags:    winDirect | winProxy,
		server:   winServer(e, e, Endpoint{}),
		override: winBypass(bypass),
		pac:      "",
	})
}

// Disable turns the proxies off and leaves the rest as it is.
func Disable() error {
	cur, err := read()
	if err != nil {
		return err
	}
	opts := []perConnOption{{option: connFlags, value: [8 / ptrSize]uintptr{uintptr(cur.flags&^winProxy | winDirect)}}}
	return write(opts)
}

// Snapshot reads the LAN settings. It changes nothing.
func Snapshot() (Saved, error) {
	ws, err := read()
	if err != nil {
		return Saved{}, err
	}
	return Saved{Services: []Service{winService(ws)}}, nil
}

// Restore puts the LAN settings back as saved has them; with none saved
// it turns the proxies off.
func Restore(_ context.Context, saved Saved) error {
	if len(saved.Services) == 0 {
		return Disable()
	}
	return apply(saved.Services[0].winSettings())
}

// Check reads the LAN settings and tells whether they point at the core's
// mixed port. It changes nothing.
func Check(port int) (Status, error) {
	ws, err := read()
	if err != nil {
		return Status{}, err
	}
	return winStatus(ws, port), nil
}
