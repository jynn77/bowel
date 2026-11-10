package locate

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oboo/moho/internal/geo"
)

const sampleTrace = `fl=12f345
h=www.cloudflare.com
ip=203.0.113.7
ts=1762500000.123
visit_scheme=https
uag=Go-http-client/1.1
colo=NRT
sliver=none
http=http/1.1
loc=JP
tls=TLSv1.3
sni=plaintext
warp=off
gateway=off
rbi=off
kex=X25519
`

func TestParseTrace(t *testing.T) {
	ip, loc, colo := parseTrace(sampleTrace)
	if ip != "203.0.113.7" || loc != "JP" || colo != "NRT" {
		t.Fatalf("got %q %q %q", ip, loc, colo)
	}
	ip, loc, colo = parseTrace("ip=2001:db8::1\r\nloc=HK\r\ncolo=HKG\r\n")
	if ip != "2001:db8::1" || loc != "HK" || colo != "HKG" {
		t.Fatalf("CRLF: got %q %q %q", ip, loc, colo)
	}
	if ip, loc, colo = parseTrace("<html>nope</html>"); ip+loc+colo != "" {
		t.Fatalf("garbage: got %q %q %q", ip, loc, colo)
	}
}

func TestParseAnswer(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		ip, code   string
		city, colo string
		coord      bool
	}{
		{"cloudflare", sampleTrace, "203.0.113.7", "JP", "", "NRT", false},
		{"ip.sb", `{"ip":"154.9.241.186","city":"Los Angeles","country_code":"US","latitude":34.0515,"longitude":-118.2707}`,
			"154.9.241.186", "US", "Los Angeles", "", true},
		{"ipwho.is", `{"ip":"82.38.69.104","success":true,"country_code":"GB","city":"London","latitude":51.5,"longitude":-0.12}`,
			"82.38.69.104", "GB", "London", "", true},
		{"ip-api", `{"status":"success","countryCode":"HK","city":"Hong Kong","query":"104.28.157.195","lat":22.3,"lon":114.2}`,
			"104.28.157.195", "HK", "Hong Kong", "", true},
		{"ip-api fail", `{"status":"fail","message":"private range","query":"10.0.0.1"}`, "", "", "", "", false},
		{"ipwho.is fail", `{"success":false,"message":"reserved range","ip":"10.0.0.1"}`, "", "", "", "", false},
		{"ipinfo.io", `{"ip":"203.0.113.5","city":"Osaka","region":"Osaka","country":"JP","loc":"34.69,135.50"}`,
			"203.0.113.5", "JP", "Osaka", "", false},
		{"full country name", `{"ip":"203.0.113.6","country":"Japan"}`, "", "", "", "", false},
		{"no country", `{"ip":"203.0.113.9","city":"Nowhere"}`, "", "", "", "", false},
		{"html", "<html>nope</html>", "", "", "", "", false},
	}
	for _, tt := range tests {
		a, ok := parseAnswer([]byte(tt.body))
		if tt.code == "" {
			if ok {
				t.Errorf("%s: parsed %+v, want nothing", tt.name, a)
			}
			continue
		}
		if !ok || a.ip != tt.ip || a.code != tt.code || a.city != tt.city || a.colo != tt.colo || a.coord != tt.coord {
			t.Errorf("%s: got %+v ok=%v, want ip=%s code=%s city=%s colo=%s coord=%v",
				tt.name, a, ok, tt.ip, tt.code, tt.city, tt.colo, tt.coord)
		}
	}
}

func TestPresets(t *testing.T) {
	if len(Presets) < 2 || Presets[0].URL != DefaultSource {
		t.Fatalf("presets: %+v", Presets)
	}
	for _, p := range Presets {
		if p.Name == "" || !strings.HasPrefix(p.URL, "https://") && !strings.HasPrefix(p.URL, "http://") {
			t.Errorf("%q is no http(s) address", p.URL)
		}
		if got, ok := PresetFor(p.URL); !ok || got != p {
			t.Errorf("PresetFor(%q) = %+v %v", p.URL, got, ok)
		}
	}
	if _, ok := PresetFor("https://example.com/exit"); ok {
		t.Error("an address outside the presets was found among them")
	}
}

func TestPlace(t *testing.T) {
	// Cloudflare's data centre pins the city.
	if p, ok := place(answer{code: "JP", colo: "NRT"}); !ok || p.City != "东京" {
		t.Errorf("colo: %+v %v", p, ok)
	}
	// A city the tables know, in the country the service named.
	if p, ok := place(answer{code: "HK", city: "Hong Kong"}); !ok || p.City != "香港" {
		t.Errorf("city: %+v %v", p, ok)
	}
	// A city they don't know: the country, with the service's coordinates.
	p, ok := place(answer{code: "US", city: "Glenview", lat: 42.07, lon: -87.79, coord: true})
	if !ok || p.City != "美国" || math.Abs(p.Lat-42.07) > 1e-9 || math.Abs(p.Lon+87.79) > 1e-9 {
		t.Errorf("unknown city: %+v %v", p, ok)
	}
	// A city of another country is not this one's.
	if p, ok := place(answer{code: "US", city: "Tokyo"}); !ok || p.City != "美国" {
		t.Errorf("other country's city: %+v %v", p, ok)
	}
	// A country the tables don't know is no place.
	if _, ok := place(answer{code: "ZZ"}); ok {
		t.Error("unknown country accepted")
	}
}

func TestGuess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "locate.json")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	hk, _ := geo.FromTrace("HK", "HKG")

	l := New(path)
	l.now = func() time.Time { return now }
	l.mu.Lock()
	l.cache["fresh"] = Result{Place: &hk, Source: SourceProbe, IP: "198.51.100.1", At: now.Add(-23 * time.Hour)}
	l.cache["stale"] = Result{Place: &hk, Source: SourceProbe, At: now.Add(-25 * time.Hour)}
	l.cache["noplace"] = Result{Source: SourceProbe, IP: "198.51.100.2", At: now}
	if err := l.saveLocked(); err != nil {
		t.Fatal(err)
	}
	l.mu.Unlock()

	// A new locator loads what the first one saved.
	l = New(path)
	l.now = func() time.Time { return now }

	if r := l.Guess("fresh", "🇯🇵 日本 01"); r.Source != SourceProbe || r.Place == nil || r.Place.Country != "HK" || r.IP != "198.51.100.1" {
		t.Errorf("fresh probe: %+v", r)
	}
	if r := l.Guess("stale", "🇯🇵 日本 01"); r.Source != SourceName || r.Place == nil || r.Place.Country != "JP" {
		t.Errorf("stale probe → name: %+v", r)
	}
	if r := l.Guess("noplace", "US Los Angeles"); r.Source != SourceName || r.Place == nil || r.Place.Country != "US" {
		t.Errorf("probe without place → name: %+v", r)
	}
	if r := l.Guess("other", "Relay-7 专线"); r.Source != SourceUnknown || r.Place != nil {
		t.Errorf("unknown: %+v", r)
	}
	if r := l.Guess("other", "新加坡 03"); r.Source != SourceName || r.Place.Country != "SG" {
		t.Errorf("name: %+v", r)
	}

	// A missing or broken cache file is an empty cache.
	_ = os.WriteFile(path, []byte("{"), 0o600)
	if r := New(path).Guess("fresh", ""); r.Source != SourceUnknown {
		t.Errorf("broken cache: %+v", r)
	}
	if r := New("").Guess("fresh", ""); r.Source != SourceUnknown {
		t.Errorf("no cache: %+v", r)
	}
}

func TestRecordKeepsTheDevicesPlace(t *testing.T) {
	l := New(filepath.Join(t.TempDir(), "locations.json"))
	if r := l.Guess("DIRECT", ""); r.Place != nil {
		t.Fatalf("known before any measurement: %+v", r)
	}
	if _, err := l.record("DIRECT", "ip=203.0.113.9\nloc=JP\ncolo=NRT\n"); err != nil {
		t.Fatal(err)
	}
	r := New(l.path).Guess("DIRECT", "")
	if r.Source != SourceProbe || r.Place == nil || r.Place.Country != "JP" {
		t.Fatalf("after a trace, from the file: %+v", r)
	}
	if _, err := l.record("x", "nothing useful"); err == nil {
		t.Fatal("a trace with no ip or loc was accepted")
	}
}
