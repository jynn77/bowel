package sysproxy

import (
	"context"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Endpoint is one of a network service's proxies: web, secure web or SOCKS.
type Endpoint struct {
	On   bool   `json:"on"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

// Service is one network service's proxies and bypass list.
type Service struct {
	Name   string   `json:"name"`
	Web    Endpoint `json:"web"`
	Secure Endpoint `json:"secure"`
	Socks  Endpoint `json:"socks"`
	Bypass []string `json:"bypass"`

	// Windows only: the automatic configuration, which WinINet puts before
	// the proxies above. Enable turns it off; Restore puts it back.
	PAC        string `json:"pac,omitempty"`        // the script's URL, kept while off
	PACOn      bool   `json:"pacOn,omitempty"`      // the script is used
	AutoDetect bool   `json:"autoDetect,omitempty"` // WPAD
}

// Saved is the system proxy of every service, as Snapshot read it, for
// Restore to put back.
type Saved struct {
	Services []Service `json:"services"`
}

func (s *Service) endpoints() []*Endpoint { return []*Endpoint{&s.Web, &s.Secure, &s.Socks} }

// Forget clears what in saved points at the local ports given, so that a
// snapshot taken while Moho already held the proxy does not bring it back:
// those proxies are restored to none. A service all of whose proxies were
// Moho's gets the system's default bypass list back too, as its own is lost.
func (saved *Saved) Forget(ports ...int) {
	for i := range saved.Services {
		sv := &saved.Services[i]
		ours, set := 0, 0
		for _, e := range sv.endpoints() {
			if e.Host == "" {
				continue
			}
			set++
			if local(e.Host) && slices.Contains(ports, e.Port) {
				*e = Endpoint{}
				ours++
			}
		}
		if set > 0 && ours == set {
			sv.Bypass = slices.Clone(DefaultBypass)
		}
	}
}

// TurnOffDead turns off the local proxies in saved that listening says
// nothing serves any more, so that putting them back does not leave the
// system pointing at a dead port. Each keeps its address, for the user to
// turn on again; proxies on other machines are not looked at.
func (saved *Saved) TurnOffDead(listening func(host string, port int) bool) {
	type addr struct {
		host string
		port int
	}
	known := map[addr]bool{}
	for i := range saved.Services {
		for _, e := range saved.Services[i].endpoints() {
			if !e.On || !local(e.Host) {
				continue
			}
			at := addr{e.Host, e.Port}
			up, seen := known[at]
			if !seen {
				up = listening(e.Host, e.Port)
				known[at] = up
			}
			e.On = up
		}
	}
}

// listenLimit is long enough for a local listener, however busy, as the
// kernel answers for it. Windows retries a refused local connection for
// about two seconds, so waiting for the refusal would cost that at quitting.
const listenLimit = 500 * time.Millisecond

// Listening reports whether something at host:port takes connections.
func Listening(ctx context.Context, host string, port int) bool {
	ctx, cancel := context.WithTimeout(ctx, listenLimit)
	defer cancel()
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func local(host string) bool {
	h := strings.ToLower(host)
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}
