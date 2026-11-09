// Package geo guesses where a proxy node exits, so the UI can draw an arc on
// a map from the user to each node.
//
// Two sources are supported: the node's name as proxy providers write it
// ("🇭🇰 香港 03 IPLC", "JP-Tokyo-02", "美国 圣何塞"), and a Cloudflare
// /cdn-cgi/trace answer fetched through the node (loc + colo). All data lives
// in the Go tables in data.go.
package geo

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Place is where a node exits (or where the user is).
type Place struct {
	Country string  `json:"country"` // ISO 3166-1 alpha-2, e.g. "HK", "JP", "US"
	City    string  `json:"city"`    // display name in Simplified Chinese, e.g. "东京"; the country's name when the city is unknown, e.g. "日本"
	Flag    string  `json:"flag"`    // flag emoji, e.g. "🇯🇵"
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

// FromName guesses a node's place from its name, as proxy providers name them:
// "🇭🇰 香港 03 IPLC", "日本东京 01", "JP-Tokyo-02", "SG 新加坡", "US Los Angeles", "美国 圣何塞",
// "台湾 Hinet", "Relay-7 专线" (→ false). City beats country. ok=false when nothing matches.
//
// Matching rules:
//   - flag emoji (regional indicator pairs) name a country;
//   - Chinese names match as substrings ("日本东京" → 东京);
//   - English names and codes match whole ASCII-letter tokens, case-insensitively
//     ("AUS" is not "US", "IPLC" is not "PL"); multi-word names also match
//     written together ("HongKong", "LosAngeles"). Codes that are common English
//     words (IN, IT, MY, NO, AT, BE, CO, EG) only count when written in capitals,
//     and "GB" after a number is gigabytes ("剩余流量：100GB");
//   - a match lying inside a longer one is ignored ("罗马尼亚" is not 罗马,
//     "印度尼西亚" is not 印度);
//   - a city beats a country; among equals the one occurring first wins;
//   - mainland China (CN) is usually where a relay enters, not where it exits
//     ("上海-东京 IPLC", "CN2 日本"), so it is used only when nothing else matches.
//
// When only a country is found, City is the country's name and the coordinates
// are those of its main city.
func FromName(name string) (Place, bool) {
	ms := dropContained(findMatches(name))
	if len(ms) == 0 {
		return Place{}, false
	}
	best := ms[0]
	for _, m := range ms[1:] {
		if r, br := m.rank(), best.rank(); r < br || r == br && m.start < best.start {
			best = m
		}
	}
	return best.place(), true
}

// FromTrace builds a place from a Cloudflare /cdn-cgi/trace answer: loc is the ISO
// country, colo the IATA code of the Cloudflare data centre that answered (close to
// the exit), e.g. ("HK","HKG"), ("JP","NRT"), ("US","LAX"). Unknown colo → the
// country's main city; unknown country → ok=false.
//
// A colo in a different country than loc (traffic routed to a neighbour's data
// centre, e.g. ("VN","SIN")) is ignored and loc decides. In the fallback case
// City is the country's name and the coordinates are those of its main city.
func FromTrace(loc, colo string) (Place, bool) {
	c, ok := countryByCode[strings.ToUpper(strings.TrimSpace(loc))]
	if !ok {
		return Place{}, false
	}
	if ct, ok := coloCity[strings.ToUpper(strings.TrimSpace(colo))]; ok && ct.country == c {
		return ct.place(), true
	}
	return c.place(), true
}

// Country returns the place of a country's main city by ISO code
// (case-insensitive). City is the country's name, e.g. Country("JP") →
// {JP 日本 🇯🇵 35.68 139.69}; ok=false for an unknown code.
func Country(code string) (Place, bool) {
	c, ok := countryByCode[strings.ToUpper(strings.TrimSpace(code))]
	if !ok {
		return Place{}, false
	}
	return c.place(), true
}

// weakCountry is matched by FromName only when nothing else is; see FromName.
const weakCountry = "CN"

// upperOnly lists codes that are also common English words: as tokens they
// count only when written in capitals ("IN 01" is India, "Made in HK" is not).
var upperOnly = map[string]bool{
	"in": true, "it": true, "my": true, "no": true,
	"at": true, "be": true, "co": true, "eg": true,
}

// country and city are the resolved forms of the rows in data.go.
type country struct {
	code    string
	name    string
	main    *city
	aliases []string
}

type city struct {
	country *country
	name    string
	lat     float64
	lon     float64
	aliases []string
}

func (c *country) place() Place {
	return Place{Country: c.code, City: c.name, Flag: flag(c.code), Lat: c.main.lat, Lon: c.main.lon}
}

func (c *city) place() Place {
	return Place{Country: c.country.code, City: c.name, Flag: flag(c.country.code), Lat: c.lat, Lon: c.lon}
}

// flag turns an ISO 3166-1 alpha-2 code into its flag emoji: each letter maps
// to the regional indicator symbol U+1F1E6 + (letter - 'A').
func flag(code string) string {
	if len(code) != 2 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < 2; i++ {
		c := code[i]
		if c < 'A' || c > 'Z' {
			return ""
		}
		b.WriteRune(riA + rune(c-'A'))
	}
	return b.String()
}

const riA, riZ = rune(0x1F1E6), rune(0x1F1FF) // regional indicators A and Z

// ---- index, built once from the tables in data.go ----

// target is what an alias points at: a city (with its country) or a country alone.
type target struct {
	country *country
	city    *city // nil for a country
}

type zhAlias struct {
	text string
	t    target
}

type enAlias struct {
	words []string // lower case
	t     target
}

var (
	countryByCode = map[string]*country{}
	cityByName    = map[string]*city{}
	coloCity      = map[string]*city{}

	zhAliases []zhAlias                // matched as substrings
	enByFirst = map[string][]enAlias{} // keyed by first word
	enJoined  = map[string][]enAlias{} // multi-word aliases written together, keyed by the joined form
)

func init() {
	for _, r := range countryRows {
		if _, dup := countryByCode[r.code]; dup {
			panic("geo: duplicate country " + r.code)
		}
		countryByCode[r.code] = &country{code: r.code, name: r.name, aliases: append([]string{r.code}, r.aliases...)}
	}
	for _, r := range cityRows {
		c, ok := countryByCode[r.country]
		if !ok {
			panic(fmt.Sprintf("geo: city %s has unknown country %s", r.name, r.country))
		}
		if _, dup := cityByName[r.name]; dup {
			panic("geo: duplicate city " + r.name)
		}
		cityByName[r.name] = &city{country: c, name: r.name, lat: r.lat, lon: r.lon, aliases: r.aliases}
	}
	for _, r := range countryRows {
		c := countryByCode[r.code]
		m, ok := cityByName[r.main]
		if !ok || m.country != c {
			panic(fmt.Sprintf("geo: country %s has bad main city %s", r.code, r.main))
		}
		c.main = m
		for _, a := range c.aliases {
			addAlias(a, target{country: c})
		}
	}
	for _, r := range cityRows {
		c := cityByName[r.name]
		for _, a := range c.aliases {
			addAlias(a, target{country: c.country, city: c})
		}
	}
	for code, name := range coloRows {
		c, ok := cityByName[name]
		if !ok {
			panic(fmt.Sprintf("geo: colo %s has unknown city %s", code, name))
		}
		coloCity[code] = c
	}
}

func addAlias(a string, t target) {
	if !isASCII(a) {
		zhAliases = append(zhAliases, zhAlias{a, t})
		return
	}
	words := strings.Fields(strings.ToLower(a))
	if len(words) == 0 {
		return
	}
	ea := enAlias{words, t}
	enByFirst[words[0]] = append(enByFirst[words[0]], ea)
	if len(words) > 1 {
		j := strings.Join(words, "")
		enJoined[j] = append(enJoined[j], ea)
	}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// ---- matching ----

// match is one alias found in a name, at byte span [start, end).
type match struct {
	start, end int
	t          target
}

// rank orders matches: strong city, strong country, weak city, weak country.
func (m match) rank() int {
	r := 0
	if m.t.city == nil {
		r++
	}
	if m.t.country.code == weakCountry {
		r += 2
	}
	return r
}

func (m match) place() Place {
	if m.t.city != nil {
		return m.t.city.place()
	}
	return m.t.country.place()
}

func findMatches(s string) []match {
	s = StripInvisible(s)
	var ms []match

	// Flag emoji: a pair of regional indicators spells the ISO code.
	for i := 0; i < len(s); {
		r1, n1 := utf8.DecodeRuneInString(s[i:])
		if r1 >= riA && r1 <= riZ {
			r2, n2 := utf8.DecodeRuneInString(s[i+n1:])
			if r2 >= riA && r2 <= riZ {
				code := string([]byte{byte('A' + r1 - riA), byte('A' + r2 - riA)})
				if c, ok := countryByCode[code]; ok {
					ms = append(ms, match{i, i + n1 + n2, target{country: c}})
				}
				i += n1 + n2
				continue
			}
		}
		i += n1
	}

	// Chinese names: every substring occurrence.
	for _, a := range zhAliases {
		for off := 0; ; {
			j := strings.Index(s[off:], a.text)
			if j < 0 {
				break
			}
			start := off + j
			ms = append(ms, match{start, start + len(a.text), a.t})
			off = start + len(a.text)
		}
	}

	// English names and codes: whole tokens.
	toks := tokenize(s)
	for i, tk := range toks {
		if upperOnly[tk.text] && !tk.upper || tk.text == "gb" && afterNumber(s, tk.start) {
			continue
		}
		for _, a := range enByFirst[tk.text] {
			if n := len(a.words); i+n <= len(toks) && wordsAt(toks[i:i+n], a.words) {
				ms = append(ms, match{tk.start, toks[i+n-1].end, a.t})
			}
		}
		for _, a := range enJoined[tk.text] {
			ms = append(ms, match{tk.start, tk.end, a.t})
		}
	}
	return ms
}

// afterNumber reports whether s[:i] ends in a digit, maybe followed by spaces
// ("100GB", "98.5 GB": gigabytes in a provider's traffic-info node).
func afterNumber(s string, i int) bool {
	t := strings.TrimRight(s[:i], " ")
	return t != "" && t[len(t)-1] >= '0' && t[len(t)-1] <= '9'
}

func wordsAt(toks []token, words []string) bool {
	for i, w := range words {
		if toks[i].text != w {
			return false
		}
	}
	return true
}

// dropContained removes matches lying strictly inside a longer match.
func dropContained(ms []match) []match {
	out := ms[:0:0]
	for i, m := range ms {
		inside := false
		for j, o := range ms {
			if i != j && o.start <= m.start && m.end <= o.end && o.end-o.start > m.end-m.start {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, m)
		}
	}
	return out
}

// InvisibleChars is the zero-width characters providers hide inside a name
// so that it is not read, by a person or by a filter, as one string: it is
// what the matcher drops before matching.
const InvisibleChars = "\u00ad\u180e\u200b\u200c\u200d\u200e\u200f\u2060\u2061\u2062\u2063\u2064\ufeff"

// InvisibleRule is one name rule matching the zero-width characters
// providers hide inside a name (InvisibleChars). It is the settings'
// default: a regular expression, as the name rules' syntax asks, written
// between two slashes.
const InvisibleRule = "/[\\x{00AD}\\x{180E}\\x{200B}-\\x{200F}\\x{2060}-\\x{2064}\\x{FEFF}]/"

// StripInvisible drops the zero-width characters providers hide inside a
// name so that it is not read, by a person or by a filter: "[Vless] H\u200b
// K\u200b \u200bT\u200bG:@channel" is "HK TG:@channel" written with a
// zero-width space between every letter. Dropped before matching, the pieces
// join back into words ("H\u200bK" is the token "hk", "日\u200b本" is the
// substring "日本"); a real space stays a separator. Name rules are read
// from what they match too: a rule is written from the name as shown, and
// the shown name has none of these in it.
func StripInvisible(s string) string {
	if !strings.ContainsFunc(s, invisible) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if invisible(r) {
			return -1
		}
		return r
	}, s)
}

func invisible(r rune) bool {
	return strings.ContainsRune(InvisibleChars, r)
}

// token is a run of ASCII letters (after folding accents and full-width
// forms), lower-cased, at byte span [start, end) of the original string.
type token struct {
	text       string
	start, end int
	upper      bool // every letter was written in capitals
}

func tokenize(s string) []token {
	var (
		toks  []token
		b     strings.Builder
		start = -1
		upper bool
	)
	flush := func(end int) {
		if start >= 0 {
			toks = append(toks, token{b.String(), start, end, upper})
			start = -1
		}
	}
	for i, r := range s {
		r = fold(r)
		isUpper, isLower := r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z'
		if !isUpper && !isLower {
			flush(i)
			continue
		}
		if start < 0 {
			start, upper = i, true
			b.Reset()
		}
		if isUpper {
			r += 'a' - 'A'
		} else {
			upper = false
		}
		b.WriteRune(r)
	}
	flush(len(s))
	return toks
}

// fold maps full-width Latin letters and common accented letters to ASCII,
// so "ＨＫ" and "São Paulo" tokenize like "HK" and "Sao Paulo".
func fold(r rune) rune {
	switch {
	case r < utf8.RuneSelf:
		return r
	case r >= 'Ａ' && r <= 'Ｚ':
		return r - 'Ａ' + 'A'
	case r >= 'ａ' && r <= 'ｚ':
		return r - 'ａ' + 'a'
	}
	if f, ok := accents[r]; ok {
		return f
	}
	return r
}

var accents = func() map[rune]rune {
	m := map[rune]rune{}
	for base, s := range map[rune]string{
		'a': "àáâãäå", 'A': "ÀÁÂÃÄÅ",
		'c': "ç", 'C': "Ç",
		'e': "èéêë", 'E': "ÈÉÊË",
		'i': "ìíîï", 'I': "ÌÍÎÏ",
		'n': "ñ", 'N': "Ñ",
		'o': "òóôõöø", 'O': "ÒÓÔÕÖØ",
		'u': "ùúûü", 'U': "ÙÚÛÜ",
		'y': "ýÿ", 'Y': "Ý",
	} {
		for _, r := range s {
			m[r] = base
		}
	}
	return m
}()
