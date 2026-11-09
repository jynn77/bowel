// Package locate finds where a node really exits: from a probe through the
// node (asking the service the settings name, Cloudflare's /cdn-cgi/trace by
// default) when there is one, else from the node's name.
package locate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oboo/moho/internal/geo"
	"github.com/oboo/moho/internal/mihomo"
)

// ProbeGroup is the selector the probe listener is bound to (core.ProbeGroup;
// repeated here so locate does not depend on core).
const ProbeGroup = "MOHO-PROBE"

// Direct is the node that measures the user's own location.
const Direct = "DIRECT"

// Sources of a Result.
const (
	SourceProbe   = "probe"
	SourceName    = "name"
	SourceUnknown = "unknown"
)

const (
	// DefaultSource is what a probe asks when the settings name nothing:
	// Cloudflare's trace, which every node can reach, has no quota, and
	// reports the data centre that answered (colo) as well as the country.
	DefaultSource = "https://www.cloudflare.com/cdn-cgi/trace"
	// probeTimeout is how long one request may take. A node that cannot
	// reach the service, or answers too slowly, is not worth waiting on.
	probeTimeout = 8 * time.Second
	// maxAge is how long a probe is trusted by Guess.
	maxAge = 24 * time.Hour
)

// userAgent is what the probe calls itself; some services refuse an empty one.
const userAgent = "bowel"

// Preset is one service the settings offer by name: an address that answers
// with the exit's address and country, in either shape parseAnswer reads.
type Preset struct {
	Name string
	URL  string
}

// Presets are the services the exit lookup setting offers, the default
// first. Any other http(s) address works as well: these are only the ones
// worth naming.
var Presets = []Preset{
	{"Cloudflare", DefaultSource},
	{"ip.sb", "https://api.ip.sb/geoip"},
	{"ipwho.is", "https://ipwho.is/"},
	{"ip-api", "http://ip-api.com/json/?fields=status,countryCode,city,lat,lon,query"},
	{"ipinfo.io", "https://ipinfo.io/json"},
}

// PresetFor reports the preset whose URL is u, and whether there is one.
func PresetFor(u string) (Preset, bool) {
	for _, p := range Presets {
		if p.URL == u {
			return p, true
		}
	}
	return Preset{}, false
}

// answer is what a service said about the connection it saw: an address, a
// country, and sometimes a city and coordinates of its own.
type answer struct {
	ip    string
	code  string  // ISO 3166-1 alpha-2
	city  string  // the service's own name for the city, often English
	lat   float64 //
	lon   float64 //
	coord bool    // lat/lon came from the service
	colo  string  // Cloudflare's data centre code, when the service reports one
}

// Result is where a node exits, and how Moho knows.
type Result struct {
	Place  *geo.Place `json:"place,omitempty"`
	Source string     `json:"source"` // "probe", "name", "unknown"
	IP     string     `json:"ip,omitempty"`
	At     time.Time  `json:"at"`
}

// Locator caches probes by node and keeps them in a JSON file.
type Locator struct {
	path  string
	now   func() time.Time
	slot  chan struct{} // one probe at a time
	mu    sync.Mutex
	cache map[string]Result
	// source is what probes ask (settings.LocateURL); "" is DefaultSource.
	source string
	// named is geo.FromName by display name: a snapshot asks for every
	// node's, every 2 s, and names don't move.
	named map[string]namedPlace
	// Login, when set, gives the probe listener's user and password.
	Login func() *url.Userinfo
}

type namedPlace struct {
	place geo.Place
	ok    bool
}

// New returns a locator whose probes are kept in cachePath (none if empty),
// loading the ones already there.
func New(cachePath string) *Locator {
	l := &Locator{path: cachePath, now: time.Now, slot: make(chan struct{}, 1), cache: map[string]Result{}}
	if cachePath != "" {
		if b, err := os.ReadFile(cachePath); err == nil {
			_ = json.Unmarshal(b, &l.cache)
		}
		if l.cache == nil {
			l.cache = map[string]Result{}
		}
	}
	return l
}

// SetSource changes what probes ask, as the settings' exit lookup URL: an
// http(s) address answering with the exit's address and country, either in
// Cloudflare's key=value trace format or as JSON (ip.sb, ipwho.is, ip-api
// and their like). Empty asks DefaultSource.
func (l *Locator) SetSource(u string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.source = strings.TrimSpace(u)
}

// sourceURL is what this Locator's probes ask; l.mu is not held.
func (l *Locator) sourceURL() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.source == "" {
		return DefaultSource
	}
	return l.source
}

// login is what the probe listener asks for, nil when it asks nothing.
func (l *Locator) login() *url.Userinfo {
	if l.Login == nil {
		return nil
	}
	return l.Login()
}

// Guess answers without the network: a cached probe (at most 24 h old), else
// geo.FromName(display), else unknown.
func (l *Locator) Guess(node, display string) Result {
	now := l.now()
	l.mu.Lock()
	r, ok := l.cache[node]
	l.mu.Unlock()
	if ok && r.Place != nil && now.Sub(r.At) <= maxAge {
		return r
	}
	l.mu.Lock()
	n, seen := l.named[display]
	l.mu.Unlock()
	if !seen {
		n.place, n.ok = geo.FromName(display)
		l.mu.Lock()
		if l.named == nil || len(l.named) >= 4096 { // names that come and go
			l.named = map[string]namedPlace{}
		}
		l.named[display] = n
		l.mu.Unlock()
	}
	if n.ok {
		p := n.place
		return Result{Place: &p, Source: SourceName, At: now}
	}
	return Result{Source: SourceUnknown, At: now}
}

// Probe measures where node exits; node "DIRECT" measures the user's own
// location. It points the selector group MOHO-PROBE at node, then asks the
// settings' exit lookup URL through the probe listener 127.0.0.1:probePort,
// and maps the answer to a place (see place). Probes run one at a time;
// results are cached by node and written to the cache file.
func (l *Locator) Probe(ctx context.Context, c *mihomo.Client, probePort int, node string) (Result, error) {
	select {
	case l.slot <- struct{}{}:
	case <-ctx.Done():
		return Result{Source: SourceUnknown, At: l.now()}, ctx.Err()
	}
	defer func() { <-l.slot }()

	if err := c.Select(ctx, ProbeGroup, node); err != nil {
		return Result{Source: SourceUnknown, At: l.now()}, fmt.Errorf("select %s: %w", node, err)
	}
	src := l.sourceURL()
	body, err := fetch(ctx, probePort, src, l.login())
	if err != nil {
		return Result{Source: SourceUnknown, At: l.now()}, err
	}
	a, ok := parseAnswer(body)
	if !ok {
		return Result{Source: SourceUnknown, At: l.now()}, fmt.Errorf("locate: no address or country in the answer of %s", src)
	}
	r := Result{Source: SourceProbe, IP: a.ip, At: l.now()}
	if p, ok := place(a); ok {
		r.Place = &p
	}
	l.mu.Lock()
	l.cache[node] = r
	err = l.saveLocked()
	l.mu.Unlock()
	return r, err
}

// fetch requests src through the probe listener, on a connection of its own:
// a kept-alive one would still go through the previous node. login is what
// the listener asks for, when the core has an authentication of its own.
func fetch(ctx context.Context, probePort int, src string, login *url.Userinfo) ([]byte, error) {
	proxy := &url.URL{Scheme: "http", User: login, Host: "127.0.0.1:" + strconv.Itoa(probePort)}
	client := &http.Client{
		Timeout:   probeTimeout,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("locate: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("locate: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("locate: %s", resp.Status)
	}
	return b, nil
}

// parseAnswer reads what a service answered, whichever of the two shapes the
// settings' URL speaks: JSON, as ip.sb, ipwho.is and ip-api do, or the
// key=value lines of Cloudflare's /cdn-cgi/trace.
func parseAnswer(body []byte) (answer, bool) {
	if t := strings.TrimSpace(string(body)); strings.HasPrefix(t, "{") {
		return parseJSON(string(body))
	}
	ip, loc, colo := parseTrace(string(body))
	if ip == "" && loc == "" {
		return answer{}, false
	}
	return answer{ip: ip, code: strings.ToUpper(loc), colo: colo}, true
}

// address is the fields the JSON services use, one shape for all of them:
// ip.sb and ipwho.is write latitude/longitude, ip-api lat/lon, ip-api calls
// the address query.
type address struct {
	IP           string `json:"ip"`
	Query        string `json:"query"`
	CountryCode  string `json:"country_code"`
	CountryCode2 string `json:"countryCode"`
	// Country is ipinfo.io's name for the code; ipwho.is puts a country's
	// full name there, which is no code and is ignored by its length.
	Country   string  `json:"country"`
	City      string  `json:"city"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Success   *bool   `json:"success"`
	Status    string  `json:"status"`
}

func parseJSON(body string) (answer, bool) {
	var a address
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		return answer{}, false
	}
	if a.Success != nil && !*a.Success || a.Status == "fail" {
		return answer{}, false
	}
	code := strings.ToUpper(strings.TrimSpace(a.CountryCode))
	if code == "" {
		code = strings.ToUpper(strings.TrimSpace(a.CountryCode2))
	}
	if code == "" && len(strings.TrimSpace(a.Country)) == 2 {
		code = strings.ToUpper(strings.TrimSpace(a.Country))
	}
	if code == "" {
		return answer{}, false
	}
	out := answer{ip: firstNonEmpty(a.IP, a.Query), code: code, city: strings.TrimSpace(a.City)}
	for _, c := range [][2]float64{{a.Latitude, a.Longitude}, {a.Lat, a.Lon}} {
		if c[0] != 0 || c[1] != 0 { // 0,0 is the Gulf of Guinea, or no answer
			out.lat, out.lon, out.coord = c[0], c[1], true
			break
		}
	}
	return out, true
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}

// place turns an answer into a place: Cloudflare's data centre first, then the
// city the service named when the tables know it, then the country's main city
// carrying the service's own coordinates.
func place(a answer) (geo.Place, bool) {
	if a.colo != "" {
		if p, ok := geo.FromTrace(a.code, a.colo); ok {
			return p, true
		}
	}
	c, ok := geo.Country(a.code)
	if !ok {
		return geo.Place{}, false
	}
	if a.city != "" {
		if p, ok := geo.FromName(a.city); ok && p.Country == c.Country {
			return p, true
		}
	}
	if a.coord {
		c.Lat, c.Lon = a.lat, a.lon
	}
	return c, true
}

// parseTrace reads the key=value lines of a /cdn-cgi/trace answer.
func parseTrace(body string) (ip, loc, colo string) {
	for _, line := range strings.Split(body, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "ip":
			ip = v
		case "loc":
			loc = v
		case "colo":
			colo = v
		}
	}
	return ip, loc, colo
}

// saveLocked writes the cache file atomically; l.mu is held.
func (l *Locator) saveLocked() error {
	if l.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(l.cache, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.path), ".locate-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(b)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), l.path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}
