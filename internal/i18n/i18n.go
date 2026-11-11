// Package i18n holds the interface's language. Strings are written in
// place as pairs, T("中文", "English"), so each reads where it is used.
// Other languages are tables keyed by the Chinese of the pair (ja.go,
// hant.go); a string missing from them falls back to English for Japanese
// and to Simplified Chinese for Traditional.
package i18n

import "sync/atomic"

// Languages are the interface's languages, in the order the settings list them.
var Languages = []string{"zh", "zh-Hant", "en", "ja"}

// Names are the languages as the settings show them, each in itself.
var Names = map[string]string{"zh": "简体中文", "zh-Hant": "繁體中文", "en": "English", "ja": "日本語"}

const (
	zh int32 = iota
	en
	ja
	hant
)

var lang atomic.Int32

// Known says whether l is one of Languages.
func Known(l string) bool {
	_, ok := Names[l]
	return ok
}

// Set changes the interface's language. Unknown languages are Chinese.
func Set(l string) {
	switch l {
	case "en":
		lang.Store(en)
	case "ja":
		lang.Store(ja)
	case "zh-Hant":
		lang.Store(hant)
	default:
		lang.Store(zh)
	}
}

// Lang is the interface's language, one of Languages.
func Lang() string {
	switch lang.Load() {
	case en:
		return "en"
	case ja:
		return "ja"
	case hant:
		return "zh-Hant"
	}
	return "zh"
}

// En says whether the interface is in English.
func En() bool { return lang.Load() == en }

// T is the pair's string in the interface's language. Strings are
// written for a Mac; on other systems "Mac" reads as "computer" does in
// that language (computer.go).
func T(zhs, ens string) string {
	s := pair(zhs, ens)
	if notMac {
		return computer(s, lang.Load())
	}
	return s
}

func pair(zhs, ens string) string {
	switch lang.Load() {
	case en:
		return ens
	case ja:
		if s, ok := jaTable[zhs]; ok {
			return s
		}
		return ens
	case hant:
		if s, ok := hantTable[zhs]; ok {
			return s
		}
	}
	return zhs
}

// Every is the pair's string in each of Languages, for recognising a
// value saved in whichever language the interface showed then.
func Every(zhs, ens string) []string {
	out := []string{zhs, ens}
	if s, ok := jaTable[zhs]; ok {
		out = append(out, s)
	}
	if s, ok := hantTable[zhs]; ok {
		out = append(out, s)
	}
	return out
}
