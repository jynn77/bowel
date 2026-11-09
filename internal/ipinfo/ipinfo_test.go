package ipinfo

import (
	"testing"
	"time"
)

// The hosting flag decides when the service gives one; without it the
// network's name is all there is to go by.
func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		want    Quality
		wantErr bool
	}{
		{
			"ip-api, a data centre",
			`{"status":"success","countryCode":"HK","isp":"Lucidacloud Limited","org":"Xnnet Limited","as":"AS139659 LUCIDACLOUD LIMITED","hosting":true,"proxy":false,"mobile":false,"query":"45.136.13.153"}`,
			Quality{Kind: Data, ISP: "Lucidacloud Limited", Org: "Xnnet Limited", ASN: "AS139659"}, false,
		},
		{
			"ip-api, a home line behind a proxy",
			`{"status":"success","isp":"China Telecom","as":"AS4134 CHINANET-BACKBONE","hosting":false,"proxy":true}`,
			Quality{Kind: Home, ISP: "China Telecom", ASN: "AS4134", Proxy: true}, false,
		},
		{
			"ipwho.is, no flag, a hosting name",
			`{"ip":"1.2.3.4","success":true,"connection":{"asn":24940,"org":"Hetzner Online GmbH","isp":"Hetzner Online GmbH"}}`,
			Quality{Kind: Data, ISP: "Hetzner Online GmbH", Org: "Hetzner Online GmbH", ASN: "AS24940"}, false,
		},
		{
			"ip.sb, an ISP's name",
			`{"ip":"5.6.7.8","isp":"Charter Communications","organization":"Charter Communications","asn":20115}`,
			Quality{Kind: Home, ISP: "Charter Communications", Org: "Charter Communications", ASN: "AS20115"}, false,
		},
		{
			"ipapi.co, an AS string",
			`{"ip":"9.9.9.9","org":"Cloudflare, Inc.","asn":"AS13335"}`,
			Quality{Kind: Data, Org: "Cloudflare, Inc.", ASN: "AS13335"}, false,
		},
		{"ip-api said no", `{"status":"fail","message":"reserved range"}`, Quality{}, true},
		{"nothing at all", `{}`, Quality{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, ok := parse([]byte(tc.body))
			if !ok {
				if !tc.wantErr {
					t.Fatal("parse said nothing")
				}
				return
			}
			if tc.wantErr {
				t.Fatal("parse said something about nothing")
			}
			if q.Kind != tc.want.Kind || q.ISP != tc.want.ISP || q.Org != tc.want.Org || q.ASN != tc.want.ASN || q.Proxy != tc.want.Proxy {
				t.Fatalf("parse = %+v, want %+v", q, tc.want)
			}
		})
	}
}

// The address goes where {ip} is, or at the end.
func TestTarget(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"http://ip-api.com/json/{ip}?fields=a", "http://ip-api.com/json/1.2.3.4?fields=a"},
		{"https://ipwho.is/{ip}", "https://ipwho.is/1.2.3.4"},
		{"https://ipapi.co/", "https://ipapi.co/1.2.3.4"},
		{"https://example.net/who", "https://example.net/who/1.2.3.4"},
	} {
		if got := target(tc.src, "1.2.3.4"); got != tc.want {
			t.Errorf("target(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}

// A clean home line is 100; a data centre costs a little, a known proxy a
// lot.
func TestScore(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    Quality
		want int
	}{
		{"a clean home line", Quality{Kind: Home}, 100},
		{"a data centre", Quality{Kind: Data}, 88},
		{"a proxy", Quality{Kind: Home, Proxy: true}, 55},
		{"a mobile proxy in a data centre", Quality{Kind: Data, Proxy: true, Mobile: true}, 39},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := score(tc.q); got != tc.want {
				t.Fatalf("score = %d, want %d", got, tc.want)
			}
		})
	}
}

// The grade is the address and the node's latency together.
func TestGrade(t *testing.T) {
	for _, tc := range []struct {
		name  string
		q     Quality
		delay int
		want  string
	}{
		{"an untouched home line", Quality{Score: 100}, 80, "excellent"},
		{"a clean data centre", Quality{Score: 88}, 120, "excellent"},
		{"a known proxy", Quality{Score: 55}, 100, "fair"},
		{"a data centre far away", Quality{Score: 88}, 450, "good"},
		{"a proxy, far away", Quality{Score: 55}, 900, "poor"},
		{"untested", Quality{Score: 88}, 0, "excellent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.q.Grade(tc.delay); got != tc.want {
				t.Fatalf("grade = %q, want %q", got, tc.want)
			}
		})
	}
}

// A network's name is the only clue a flagless service leaves.
func TestKindOf(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"LUCIDACLOUD LIMITED", Data},
		{"DigitalOcean, LLC", Data},
		{"China Telecom", Home},
		{"中国联通", Home},
		{"Charter Communications", Home},
		{"Some Network We Never Heard Of", Unset},
	} {
		if got := kindOf(tc.name); got != tc.want {
			t.Errorf("kindOf(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Answers are trusted only from the service the settings name now.
func TestStoreSource(t *testing.T) {
	s := Open("")
	s.SetSource("https://a.example/{ip}")
	if err := s.Save(Quality{IP: "1.2.3.4", Kind: Home, Source: "https://a.example/{ip}", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("1.2.3.4"); !ok {
		t.Fatal("an answer from the current service was not trusted")
	}
	s.SetSource("https://b.example/{ip}")
	if _, ok := s.Get("1.2.3.4"); ok {
		t.Fatal("an answer from another service was trusted")
	}
}
