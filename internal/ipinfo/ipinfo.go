// Package ipinfo says what an exit's address is: a data centre's machine or
// somebody's home line, and how clean the address looks — the two things a
// site that cares about visitors checks before it trusts one.
//
// One service is asked, the one the settings name: they all answer with the
// network's name and AS number, and the better ones also say whether the
// network is a hosting provider's. The answer is kept by address, since an
// address' owner changes rarely.
package ipinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Kind is what kind of network an address belongs to.
const (
	Data  = "datacenter"  // a hosting provider: cloud, VPS, colocation
	Home  = "residential" // a home or office line: an ISP's customer
	Unset = ""            // neither the flag nor the name said
)

// DefaultSource is what is asked when the settings name nothing: ip-api
// answers with a hosting flag, which is the one thing the others only hint
// at. {ip} is where the address goes.
const DefaultSource = "http://ip-api.com/json/{ip}?fields=status,countryCode,isp,org,as,hosting,proxy,mobile,query"

// TTL is how long an answer is trusted: addresses move between networks, but
// not often.
const TTL = 7 * 24 * time.Hour

// timeout bounds one service's answer; a slow one is not worth waiting for.
const timeout = 10 * time.Second

const userAgent = "bowel"

// Quality is what the service said about one address.
type Quality struct {
	IP   string `json:"ip"`
	Kind string `json:"kind"` // Data, Home or Unset
	// Proxy is a proxy, VPN or Tor exit as the service lists them; a node
	// behind one is what sites block first.
	Proxy bool `json:"proxy,omitempty"`
	// Mobile is a mobile carrier's address, which some sites treat apart.
	Mobile bool   `json:"mobile,omitempty"`
	ISP    string `json:"isp,omitempty"`
	Org    string `json:"org,omitempty"`
	ASN    string `json:"asn,omitempty"`
	// Score is the address alone, 0-100; Grade adds the node's latency.
	Score int       `json:"score"`
	At    time.Time `json:"at"`
	// Source is the service that answered, so that switching to another one
	// asks again instead of trusting the last one's word.
	Source string `json:"source,omitempty"`
}

// Grade is how good the exit looks, from the address and the node's latency
// (0 when it has not been tested): "excellent", "good", "fair" or "poor".
func (q Quality) Grade(delay int) string {
	s := q.Score
	switch {
	case delay <= 0: // not tested: the address alone
	case delay > 800:
		s -= 30
	case delay > 400:
		s -= 18
	case delay > 200:
		s -= 8
	}
	switch {
	case s >= 88:
		return "excellent"
	case s >= 70:
		return "good"
	case s >= 45:
		return "fair"
	default:
		return "poor"
	}
}

// Store keeps the answers in a JSON file, so that a restart does not ask
// again for addresses it already knows.
type Store struct {
	path string
	now  func() time.Time

	mu     sync.Mutex
	cache  map[string]Quality
	source string // what to ask; "" is DefaultSource
}

// Open returns the store kept at path (nothing is written there until an
// answer is), loading the answers already in it.
func Open(path string) *Store {
	s := &Store{path: path, now: time.Now, cache: map[string]Quality{}}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &s.cache)
		}
		if s.cache == nil {
			s.cache = map[string]Quality{}
		}
	}
	return s
}

// SetSource changes what a lookup asks, as the settings' address service:
// a URL with {ip} where the address goes, or one to append it to. Empty asks
// DefaultSource.
func (s *Store) SetSource(u string) {
	u = strings.TrimSpace(u)
	if u == "" {
		u = DefaultSource
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.source = u
}

func (s *Store) sourceURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.source == "" {
		return DefaultSource
	}
	return s.source
}

// Get is the answer for ip, if one is still trusted and came from the
// service the settings name now.
func (s *Store) Get(ip string) (Quality, bool) {
	if ip == "" {
		return Quality{}, false
	}
	src := s.sourceURL()
	s.mu.Lock()
	q, ok := s.cache[ip]
	s.mu.Unlock()
	if !ok || q.Source != src || s.now().Sub(q.At) > TTL {
		return Quality{}, false
	}
	return q, true
}

// Save keeps q, and writes the file.
func (s *Store) Save(q Quality) error {
	if q.IP == "" {
		return nil
	}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[string]Quality{}
	}
	s.cache[q.IP] = q
	b, err := json.MarshalIndent(s.cache, "", "  ")
	s.mu.Unlock()
	if err != nil || s.path == "" {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".ipinfo-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(b)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// Lookup asks the settings' service about ip.
func (s *Store) Lookup(ctx context.Context, ip string) (Quality, error) {
	if ip == "" {
		return Quality{}, fmt.Errorf("ipinfo: no address")
	}
	src := s.sourceURL()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := get(ctx, target(src, ip))
	if err != nil {
		return Quality{}, err
	}
	q, ok := parse(body)
	if !ok {
		return Quality{}, fmt.Errorf("ipinfo: %s said nothing about %s", hostOf(src), ip)
	}
	q.IP, q.At, q.Source = ip, s.now(), src
	q.Score = score(q)
	_ = s.Save(q)
	return q, nil
}

// target is src with the address in it: where {ip} is, or appended.
func target(src, ip string) string {
	if strings.Contains(src, "{ip}") {
		return strings.ReplaceAll(src, "{ip}", ip)
	}
	return strings.TrimRight(src, "/") + "/" + ip
}

// hostOf is the service's name, for what a failure says.
func hostOf(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	return u
}

// score is the address alone: a clean one is 100, and what lists hold
// against it takes away.
func score(q Quality) int {
	s := 100
	if q.Kind == Data {
		s -= 12 // fine to use, but the first thing a strict site blocks
	}
	if q.Proxy {
		s -= 45
	}
	if q.Mobile {
		s -= 4
	}
	return s
}

// answer is the fields the services use, one shape for all of them: ip-api
// has hosting/proxy flags and "as", ipwho.is a connection, ip.sb and
// ipapi.co an organization and an asn.
type answer struct {
	Status string `json:"status"` // ip-api: "success" or "fail"
	// Hosting, Proxy and Mobile are pointers: absent is not false, it is a
	// service that does not say.
	Hosting *bool  `json:"hosting"`
	Proxy   *bool  `json:"proxy"`
	Mobile  *bool  `json:"mobile"`
	Query   string `json:"query"` // ip-api's name for the address
	IP      string `json:"ip"`
	ISP     string `json:"isp"`
	Org     string `json:"org"`
	AS      string `json:"as"`  // ip-api: "AS139659 LUCIDACLOUD LIMITED"
	ASN     any    `json:"asn"` // ip.sb: 139659, ipapi.co: "AS13335"
	// ipwho.is
	Success    *bool `json:"success"`
	Connection *struct {
		ASN int    `json:"asn"`
		Org string `json:"org"`
		ISP string `json:"isp"`
	} `json:"connection"`
	Organization string `json:"organization"` // ip.sb's name for org
}

// parse reads what a service answered.
func parse(body []byte) (Quality, bool) {
	var a answer
	if err := json.Unmarshal(body, &a); err != nil {
		return Quality{}, false
	}
	if strings.EqualFold(a.Status, "fail") || (a.Success != nil && !*a.Success) {
		return Quality{}, false
	}
	q := Quality{ISP: strings.TrimSpace(a.ISP), Org: firstNonEmpty(a.Org, a.Organization), ASN: asnOf(a.AS, a.ASN)}
	if a.Connection != nil {
		if q.ISP == "" {
			q.ISP = strings.TrimSpace(a.Connection.ISP)
		}
		if q.Org == "" {
			q.Org = strings.TrimSpace(a.Connection.Org)
		}
		if q.ASN == "" && a.Connection.ASN > 0 {
			q.ASN = fmt.Sprintf("AS%d", a.Connection.ASN)
		}
	}
	if a.Proxy != nil {
		q.Proxy = *a.Proxy
	}
	if a.Mobile != nil {
		q.Mobile = *a.Mobile
	}
	if a.Hosting == nil && q.ISP == "" && q.Org == "" && q.ASN == "" {
		// An answer with nothing to say about the network is no answer: it
		// is what a service returns for an address it does not know, or for
		// a page that is not this API at all.
		return Quality{}, false
	}
	switch {
	case a.Hosting != nil: // the one service that says outright
		if *a.Hosting {
			q.Kind = Data
		} else {
			q.Kind = Home
		}
	default:
		q.Kind = kindOf(q.Org + " " + q.ISP)
	}
	if q.Kind == Unset {
		q.Kind = Home // nothing said otherwise: it is somebody's line
	}
	return q, true
}

// kindOf guesses from a network's name, for a service that gives no flag.
func kindOf(name string) string {
	n := strings.ToLower(name)
	for _, w := range hostingWords {
		if strings.Contains(n, w) {
			return Data
		}
	}
	for _, w := range homeWords {
		if strings.Contains(n, w) {
			return Home
		}
	}
	return Unset
}

var hostingWords = []string{
	"cloud", "hosting", "datacenter", "data center", "server", "vps", "idc",
	"colo", "dedicated", "limestonenetworks", "ovh", "hetzner", "digitalocean",
	"vultr", "linode", "amazon", "google llc", "microsoft", "alibaba", "tencent",
	"leaseweb", "contabo", "oracle",
}

var homeWords = []string{
	"telecom", "unicom", "mobile", "broadband", "cable", "fiber", "fibre",
	"communications", "comcast", "verizon", "at&t", "charter", "cox", "vodafone",
	"orange", "deutsche telekom", "kddi", "ntt", "电信", "联通", "移动", "铁通",
	"广电", "宽带", "家宽",
}

// asnOf is the AS number out of either shape: "AS139659 LUCIDACLOUD LIMITED",
// 139659, or "AS13335".
func asnOf(as string, raw any) string {
	if f := strings.Fields(strings.TrimSpace(as)); len(f) > 0 {
		if n := strings.ToUpper(f[0]); strings.HasPrefix(n, "AS") && len(n) > 2 {
			return n
		}
	}
	switch v := raw.(type) {
	case float64:
		if v > 0 {
			return fmt.Sprintf("AS%d", int(v))
		}
	case string:
		s := strings.ToUpper(strings.TrimSpace(v))
		if s == "" {
			return ""
		}
		if !strings.HasPrefix(s, "AS") {
			s = "AS" + s
		}
		return s
	}
	return ""
}

// get fetches u and returns its body.
func get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := serviceClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ipinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ipinfo: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("ipinfo: %w", err)
	}
	return b, nil
}

// serviceClient asks the services directly: what is being asked about is a
// node, and the answer is about the node, not about this machine's route.
var serviceClient = &http.Client{Timeout: timeout}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}
