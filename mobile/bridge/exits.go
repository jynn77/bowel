package bridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/oboo/moho/internal/locate"
	"github.com/oboo/moho/internal/mihomo"
	"gopkg.in/yaml.v3"
)

const (
	probeUser = "bowel"
	// probeTimeout covers pointing the probe group at a node and the request.
	probeTimeout = 12 * time.Second
	// probeRetry is how long a node whose probe failed is left alone.
	probeRetry = 10 * time.Minute
	// directNode is the name the core and the location cache give the phone's own way out.
	directNode = "DIRECT"
	// youEvery is how long a measured place of the phone is kept before
	// measuring again: phones travel.
	youEvery = 6 * time.Hour
)

// lockProbe gives the probe listener a password. Unlike on a Mac, any app on
// the phone could otherwise use a listener on 127.0.0.1 as a way out through
// the user's nodes. A core already running keeps the password it started with.
func (x *bridge) lockProbe(cfg map[string]any) error {
	pass := ""
	if c, _ := x.core(); c != nil {
		if u := x.probeLogin(); u != nil {
			pass, _ = u.Password()
		}
	}
	if pass == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		pass = hex.EncodeToString(b)
	}
	ls, _ := cfg["listeners"].([]any)
	for _, l := range ls {
		if m, ok := l.(map[string]any); ok {
			m["users"] = []any{map[string]any{"username": probeUser, "password": pass}}
		}
	}
	// Set along with a user's own proxy password; it would open this one too.
	delete(cfg, "skip-auth-prefixes")
	return nil
}

// probeLogin reads the password from the config the core runs on, so that it
// is still known when the app restarts under a tunnel that kept going.
func (x *bridge) probeLogin() *url.Userinfo {
	raw, err := os.ReadFile(filepath.Join(x.dir, "config.yaml"))
	if err != nil {
		return nil
	}
	var cfg struct {
		Listeners []struct {
			Users []struct {
				Username string `yaml:"username"`
				Password string `yaml:"password"`
			} `yaml:"users"`
		} `yaml:"listeners"`
	}
	if yaml.Unmarshal(raw, &cfg) != nil || len(cfg.Listeners) == 0 || len(cfg.Listeners[0].Users) == 0 {
		return nil
	}
	u := cfg.Listeners[0].Users[0]
	return url.UserPassword(u.Username, u.Password)
}

// locateExits measures where the routes' current nodes really exit. Nodes
// with no working test are skipped: a subscription's fake "traffic left"
// nodes can only time out.
func (x *bridge) locateExits() {
	if !x.locating.CompareAndSwap(false, true) {
		return
	}
	defer x.locating.Store(false)
	x.locateYou()
	c, err := x.core()
	if err != nil {
		return
	}
	x.mu.Lock()
	px := x.proxies
	x.mu.Unlock()
	found := false
	seen := map[string]bool{}
	for _, r := range x.routes() {
		n := currentNode(px, r)
		if r.Direct || n == "" || n == "DIRECT" || seen[n] {
			continue
		}
		seen[n] = true
		if x.probeNode(c, n, px[n]) {
			found = true
		}
	}
	if found {
		x.push()
	}
}

// locateYou measures where the phone is, for the map's starting point. With
// the core up it goes out through the core's DIRECT, as this app's own traffic
// would otherwise leave through the node. With the VPN off there is no core
// and nothing in the way, so the app asks itself. In between it waits: a
// request made now could leave through a tunnel that's coming up.
func (x *bridge) locateYou() {
	r := x.loc.Guess(directNode, "")
	if r.Source == locate.SourceProbe && r.Place != nil && time.Since(r.At) < youEvery {
		return
	}
	c, _ := x.core()
	x.mu.Lock()
	off := x.vpn == "off"
	if (c == nil && !off) || time.Since(x.youProbed) < probeRetry {
		x.mu.Unlock()
		return
	}
	x.youProbed = time.Now()
	x.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	var got locate.Result
	if c != nil {
		got, _ = x.loc.Probe(ctx, c, probePort, directNode)
	} else {
		got, _ = x.loc.Direct(ctx)
	}
	if got.Place != nil {
		x.push()
	}
}

// youPlace is where the phone was last measured to be, nil if that's unknown.
func (x *bridge) youPlace() *placeData {
	r := x.loc.Guess(directNode, "")
	if r.Source != locate.SourceProbe || r.Place == nil {
		return nil
	}
	p := r.Place
	return &placeData{Country: p.Country, City: p.City, Flag: p.Flag, Lat: p.Lat, Lon: p.Lon}
}

// probeNode reports whether it learned the node's place.
func (x *bridge) probeNode(c *mihomo.Client, node string, p mihomo.Proxy) bool {
	if l := len(p.History); l == 0 || p.History[l-1].Delay == 0 {
		return false
	}
	if x.loc.Guess(node, display(node)).Source == locate.SourceProbe {
		return false
	}
	x.mu.Lock()
	if x.probedAt == nil {
		x.probedAt = map[string]time.Time{}
	}
	wait := time.Since(x.probedAt[node]) < probeRetry
	if !wait {
		x.probedAt[node] = time.Now()
	}
	x.mu.Unlock()
	if wait {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	r, _ := x.loc.Probe(ctx, c, probePort, node)
	return r.Place != nil
}

// located puts a measured exit in place of the guess from the node's name.
func (x *bridge) located(n nodeData) nodeData {
	r := x.loc.Guess(n.Name, n.Display)
	if r.Source == locate.SourceProbe && r.Place != nil {
		p := r.Place
		n.Where = placeData{Country: p.Country, City: p.City, Flag: p.Flag, Lat: p.Lat, Lon: p.Lon}
		n.Source = "实测"
	}
	return n
}
