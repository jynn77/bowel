package geo

import (
	"math"
	"strings"
	"testing"
)

func TestFromName(t *testing.T) {
	tests := []struct {
		name    string
		country string
		city    string // "" when ok=false
	}{
		// Examples from the API doc.
		{"🇭🇰 香港 03 IPLC", "HK", "香港"},
		{"日本东京 01", "JP", "东京"},
		{"JP-Tokyo-02", "JP", "东京"},
		{"SG 新加坡", "SG", "新加坡"},
		{"US Los Angeles", "US", "洛杉矶"},
		{"美国 圣何塞", "US", "圣何塞"},
		{"台湾 Hinet", "TW", "台湾"},
		{"Relay-7 专线", "", ""},

		// Flags.
		{"🇹🇼 01", "TW", "台湾"},
		{"🇹🇼 台北 HiNet", "TW", "台北"},
		{"🇯🇵 01 x1.5", "JP", "日本"},
		{"🇰🇷 IEPL", "KR", "韩国"},
		{"🇬🇧", "GB", "英国"},
		{"🇺🇸🇯🇵 relay", "US", "美国"}, // first flag wins
		{"🇪🇺 EU 01", "", ""},       // flag of no country in the table

		// Emoji + Chinese + English.
		{"🇯🇵 日本 大阪 02", "JP", "大阪"},
		{"🇺🇸 美国 Seattle 01", "US", "西雅图"},
		{"🇸🇬 狮城 BGP", "SG", "新加坡"},

		// Codes are whole tokens only.
		{"IPLC 01", "", ""},    // not PL
		{"AUS 01", "", ""},     // not US
		{"USA 02", "US", "美国"}, // USA is
		{"HKG-03", "HK", "香港"}, // HKG is
		{"hk03", "HK", "香港"},   // digits split tokens, case-insensitive
		{"Premium-jp", "JP", "日本"},
		{"UK 01", "GB", "英国"},
		{"GB-London", "GB", "伦敦"},
		{"VIP 01", "", ""},
		{"GIA 01", "", ""},
		{"Made in HK", "HK", "香港"}, // lower-case "in" is a word, not India
		{"IN 01", "IN", "印度"},
		{"my node", "", ""},
		{"MY 01", "MY", "马来西亚"},

		// City beats country; first city wins.
		{"美国 洛杉矶", "US", "洛杉矶"},
		{"US 01 洛杉矶", "US", "洛杉矶"},
		{"日本 → 美国 圣何塞", "US", "圣何塞"},
		{"东京 大阪", "JP", "东京"},
		{"Osaka Tokyo", "JP", "大阪"},
		{"Tokyo 大阪", "JP", "东京"},

		// Multi-word English names.
		{"LosAngeles 01", "US", "洛杉矶"},
		{"Los_Angeles", "US", "洛杉矶"},
		{"US-San-Jose-01", "US", "圣何塞"},
		{"Silicon Valley", "US", "硅谷"},
		{"New York 02", "US", "纽约"},
		{"New Taipei", "TW", "新北"},
		{"Taipei", "TW", "台北"},
		{"HongKong 01", "HK", "香港"},
		{"Hong Kong IPLC", "HK", "香港"},
		{"São Paulo", "BR", "圣保罗"},
		{"Sao Paulo", "BR", "圣保罗"},
		{"ＨＫ 01", "HK", "香港"}, // full-width
		{"South Korea", "KR", "韩国"},
		{"Kuala Lumpur", "MY", "吉隆坡"},
		{"Ho Chi Minh", "VN", "胡志明"},

		// Chinese substrings, and matches inside longer ones.
		{"罗马尼亚 01", "RO", "罗马尼亚"}, // not 罗马
		{"罗马 01", "IT", "罗马"},
		{"印度尼西亚", "ID", "印度尼西亚"}, // not 印度
		{"印尼 雅加达", "ID", "雅加达"},
		{"印度 孟买", "IN", "孟买"},
		{"印度", "IN", "印度"},
		{"新德里", "IN", "新德里"},
		{"墨西哥城", "MX", "墨西哥城"},
		{"中国台湾 01", "TW", "台湾"},
		{"中国香港 01", "HK", "香港"},
		{"内蒙古 01", "CN", "中国"}, // not Mongolia
		{"澳门 01", "MO", "澳门"},
		{"澳洲 01", "AU", "澳大利亚"},
		{"台湾彰化", "TW", "彰化"},
		{"韩国春川", "KR", "春川"},
		{"日本埼玉", "JP", "埼玉"},
		{"美国凤凰城", "US", "凤凰城"},

		// Mainland China is used only when nothing else matches.
		{"上海 → 东京 IEPL", "JP", "东京"},
		{"广港 IPLC 香港", "HK", "香港"},
		{"美国 CN2 GIA", "US", "美国"},
		{"CN2 日本", "JP", "日本"},
		{"CN2 GIA 洛杉矶", "US", "洛杉矶"},
		{"上海电信 01", "CN", "上海"},
		{"中国 01", "CN", "中国"},

		// Zero-width characters between the letters of a name, as some
		// providers write it so that a filter does not read the place: they
		// are dropped, not treated as word breaks.
		{"[Vless] H\u200bK\u200b \u200bT\u200bG:@MiaChatChannel", "HK", "香港"},
		{"[Vless] J\u200bP\u200b \u200bT\u200bG 2", "JP", "日本"},
		{"U\u200bS\u200b 洛杉矶", "US", "洛杉矶"},
		{"香\u200b港 01", "HK", "香港"},
		{"日\u200b本\u200b东\u200b京", "JP", "东京"},

		// Nothing.
		{"", "", ""},
		{"剩余流量：100GB", "", ""}, // gigabytes, not Great Britain
		{"剩余流量：98.5 GB", "", ""},
		{"套餐到期：2026-12-31", "", ""},
		{"Auto", "", ""},
	}
	for _, tt := range tests {
		p, ok := FromName(tt.name)
		if tt.city == "" {
			if ok {
				t.Errorf("FromName(%q) = %+v, want ok=false", tt.name, p)
			}
			continue
		}
		if !ok || p.Country != tt.country || p.City != tt.city {
			t.Errorf("FromName(%q) = %+v, %v; want %s %s", tt.name, p, ok, tt.country, tt.city)
		}
	}
}

func TestFromNameCountryUsesMainCity(t *testing.T) {
	p, ok := FromName("台湾 Hinet")
	if !ok {
		t.Fatal("no match")
	}
	want := Place{Country: "TW", City: "台湾", Flag: "🇹🇼", Lat: 25.03, Lon: 121.57}
	if p != want {
		t.Errorf("got %+v, want %+v", p, want)
	}
}

func TestFromNameAllCodes(t *testing.T) {
	codes := "HK HKG JP SG US USA TW KR UK GB DE FR NL RU IN AU CA TR AR BR MY TH VN PH ID AE IT ES CH SE PL ZA MX IL CN MO"
	alias := map[string]string{"HKG": "HK", "USA": "US", "UK": "GB"}
	for _, code := range strings.Fields(codes) {
		want := code
		if a, ok := alias[code]; ok {
			want = a
		}
		for _, name := range []string{code + " 01", "Premium-" + code + "-02", strings.ToLower(code) + "03"} {
			if code == "IN" || code == "IT" || code == "MY" {
				if name == strings.ToLower(code)+"03" {
					continue // lower case: an English word
				}
			}
			p, ok := FromName(name)
			if !ok || p.Country != want {
				t.Errorf("FromName(%q) = %+v, %v; want country %s", name, p, ok, want)
			}
		}
	}
}

func TestFromTrace(t *testing.T) {
	tests := []struct {
		loc, colo string
		country   string
		city      string // "" when ok=false
	}{
		{"HK", "HKG", "HK", "香港"},
		{"JP", "NRT", "JP", "东京"},
		{"JP", "KIX", "JP", "大阪"},
		{"US", "LAX", "US", "洛杉矶"},
		{"US", "SJC", "US", "圣何塞"},
		{"US", "IAD", "US", "阿什本"},
		{"US", "EWR", "US", "纽约"},
		{"TW", "KHH", "TW", "高雄"},
		{"RU", "SVO", "RU", "莫斯科"},
		{"MO", "MFM", "MO", "澳门"},
		{"us", "lax", "US", "洛杉矶"},      // case-insensitive
		{" SG ", " SIN\n", "SG", "新加坡"}, // whitespace
		{"JP", "XYZ", "JP", "日本"},       // unknown colo → country
		{"JP", "", "JP", "日本"},
		{"VN", "SIN", "VN", "越南"}, // colo abroad → loc decides
		{"TW", "HKG", "TW", "台湾"},
		{"XX", "LAX", "", ""}, // unknown country
		{"T1", "FRA", "", ""}, // Tor
		{"", "", "", ""},
	}
	for _, tt := range tests {
		p, ok := FromTrace(tt.loc, tt.colo)
		if tt.city == "" {
			if ok {
				t.Errorf("FromTrace(%q, %q) = %+v, want ok=false", tt.loc, tt.colo, p)
			}
			continue
		}
		if !ok || p.Country != tt.country || p.City != tt.city {
			t.Errorf("FromTrace(%q, %q) = %+v, %v; want %s %s", tt.loc, tt.colo, p, ok, tt.country, tt.city)
		}
	}

	// The fallback uses the main city's coordinates.
	p, _ := FromTrace("JP", "XYZ")
	if p.Lat != 35.68 || p.Lon != 139.69 || p.Flag != "🇯🇵" {
		t.Errorf("FromTrace(JP, XYZ) = %+v, want Tokyo's coordinates", p)
	}
}

func TestColoTable(t *testing.T) {
	required := "HKG NRT KIX SIN TPE KHH ICN LAX SJC SEA PDX PHX LAS DFW ORD EWR IAD ATL MIA FRA AMS LHR CDG DME SVO BOM SYD MEL YYZ YVR YUL IST GRU KUL BKK HAN SGN MNL CGK DXB MFM"
	for _, colo := range strings.Fields(required) {
		if _, ok := coloCity[colo]; !ok {
			t.Errorf("colo %s missing", colo)
		}
	}
	// Every colo resolves through FromTrace to its own city.
	for colo, c := range coloCity {
		p, ok := FromTrace(c.country.code, colo)
		if !ok || p.City != c.name {
			t.Errorf("FromTrace(%s, %s) = %+v, %v; want %s", c.country.code, colo, p, ok, c.name)
		}
	}
}

func TestCountry(t *testing.T) {
	p, ok := Country("JP")
	want := Place{Country: "JP", City: "日本", Flag: "🇯🇵", Lat: 35.68, Lon: 139.69}
	if !ok || p != want {
		t.Errorf("Country(JP) = %+v, %v; want %+v", p, ok, want)
	}
	if p, ok := Country("hk"); !ok || p.City != "香港" || p.Flag != "🇭🇰" {
		t.Errorf("Country(hk) = %+v, %v", p, ok)
	}
	if p, ok := Country("SG"); !ok || p.City != "新加坡" {
		t.Errorf("Country(SG) = %+v, %v", p, ok)
	}
	for _, code := range []string{"", "ZZ", "USA", "日本"} {
		if p, ok := Country(code); ok {
			t.Errorf("Country(%q) = %+v, want ok=false", code, p)
		}
	}
}

func TestFlag(t *testing.T) {
	tests := map[string]string{
		"TW": "\U0001F1F9\U0001F1FC",
		"HK": "🇭🇰",
		"JP": "🇯🇵",
		"US": "🇺🇸",
		"GB": "🇬🇧",
		"jp": "",
		"J":  "",
		"":   "",
	}
	for code, want := range tests {
		if got := flag(code); got != want {
			t.Errorf("flag(%q) = %q, want %q", code, got, want)
		}
	}
	// Every flag round-trips through FromName.
	for code := range countryByCode {
		p, ok := FromName(flag(code) + " node")
		if !ok || p.Country != code {
			t.Errorf("FromName(flag %s) = %+v, %v", code, p, ok)
		}
	}
}

func TestCoverage(t *testing.T) {
	if n := len(countryByCode); n < 45 {
		t.Errorf("%d countries, want at least 45", n)
	}
	required := `香港 东京 大阪 埼玉 新加坡 台北 新北 彰化 台中 高雄 首尔 春川
		洛杉矶 圣何塞 硅谷 旧金山 西雅图 波特兰 凤凰城 拉斯维加斯 达拉斯 芝加哥 纽约 华盛顿 阿什本 亚特兰大 迈阿密
		伦敦 法兰克福 柏林 巴黎 阿姆斯特丹 莫斯科 孟买 悉尼 墨尔本 多伦多 温哥华 蒙特利尔
		伊斯坦布尔 圣保罗 吉隆坡 曼谷 河内 胡志明 马尼拉 雅加达 迪拜 澳门`
	for _, name := range strings.Fields(required) {
		if _, ok := cityByName[name]; !ok {
			t.Errorf("city %s missing", name)
			continue
		}
		// Each matches by its own Chinese name.
		if p, ok := FromName(name + " 01"); !ok || p.City != name {
			t.Errorf("FromName(%q) = %+v, %v", name, p, ok)
		}
	}
	for _, en := range []string{"Tokyo", "Osaka", "Seoul", "Los Angeles", "San Jose", "Silicon Valley", "Seattle",
		"San Francisco", "Portland", "Phoenix", "Las Vegas", "Dallas", "Chicago", "New York", "Washington", "Ashburn",
		"Atlanta", "Miami", "London", "Frankfurt", "Berlin", "Paris", "Amsterdam", "Moscow", "Mumbai", "Sydney",
		"Melbourne", "Toronto", "Vancouver", "Montreal", "Istanbul", "Kuala Lumpur", "Bangkok", "Hanoi", "Manila",
		"Jakarta", "Dubai", "Macau", "Singapore", "Taipei", "Kaohsiung", "Taichung", "Changhua", "Saitama", "Chuncheon"} {
		if _, ok := FromName(en); !ok {
			t.Errorf("FromName(%q) found nothing", en)
		}
	}
}

func TestData(t *testing.T) {
	// No alias names two different cities.
	seen := map[string]string{}
	for _, c := range cityByName {
		for _, a := range c.aliases {
			k := strings.ToLower(a)
			if other, dup := seen[k]; dup {
				t.Errorf("alias %q used by %s and %s", a, other, c.name)
			}
			seen[k] = c.name
		}
	}
	for _, c := range cityByName {
		if c.lat < -90 || c.lat > 90 || c.lon < -180 || c.lon > 180 {
			t.Errorf("%s: bad coordinates %v,%v", c.name, c.lat, c.lon)
		}
	}
	// Spot-check coordinates against known values (±0.15°).
	known := map[string][2]float64{
		"香港": {22.30, 114.17}, "东京": {35.68, 139.76}, "新加坡": {1.29, 103.85}, "洛杉矶": {34.05, -118.24},
		"伦敦": {51.51, -0.13}, "悉尼": {-33.87, 151.21}, "圣保罗": {-23.55, -46.63}, "台北": {25.03, 121.56},
		"首尔": {37.57, 126.98}, "法兰克福": {50.11, 8.68}, "迪拜": {25.20, 55.27}, "纽约": {40.71, -74.01},
	}
	for name, ll := range known {
		c := cityByName[name]
		if math.Abs(c.lat-ll[0]) > 0.15 || math.Abs(c.lon-ll[1]) > 0.15 {
			t.Errorf("%s at %v,%v, want about %v", name, c.lat, c.lon, ll)
		}
	}
}
