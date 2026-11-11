package i18n

import (
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// The interface was written on and for a Mac, and says so: "this Mac",
// "这台 Mac". On Linux and Windows the same strings name the computer
// instead, in each language's own word, so that no string has to be
// written three times.

// notMac: the computer is not a Mac.
var notMac = runtime.GOOS != "darwin"

var (
	macWord   = regexp.MustCompile(`\bMac\b`)
	macSpaced = regexp.MustCompile(` ?\bMac\b ?`) // 这台 Mac 上: the spaces go with the Latin word
	computers sync.Map                            // string by language -> string
)

type computerKey struct {
	s    string
	lang int32
}

// computer is s with "Mac" as l's word for a computer.
func computer(s string, l int32) string {
	if !strings.Contains(s, "Mac") {
		return s
	}
	k := computerKey{s, l}
	if v, ok := computers.Load(k); ok {
		return v.(string)
	}
	var out string
	switch l {
	case en:
		out = macWord.ReplaceAllString(s, "computer")
	case ja:
		out = macSpaced.ReplaceAllString(s, "パソコン")
	case hant:
		out = macSpaced.ReplaceAllString(s, "電腦")
	default:
		out = macSpaced.ReplaceAllString(s, "电脑")
	}
	computers.Store(k, out)
	return out
}
