// Package ping measures how long reaching a node's server takes from this
// computer, without going through the node. It uses ICMP: a TCP handshake
// can be answered by a transparent proxy on the way (a router's, say),
// which reports a few milliseconds for a server across the ocean.
package ping

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// pingTime is the time of a reply as the systems' ping commands print
// it, in whatever language: "time=12.3 ms", "时间=12ms", "time<1ms".
// Summaries (min/avg/max = 1/2/3 ms) have no number before their "ms"
// that follows an equals sign.
var pingTime = regexp.MustCompile(`[=<]\s*([0-9]+(?:[.,][0-9]+)?)\s*ms`)

// parseTime reads the first reply's time out of ping's output.
func parseTime(out []byte) (int, error) {
	m := pingTime.FindSubmatch(out)
	if m == nil {
		return 0, errors.New("ping: no reply")
	}
	v, err := strconv.ParseFloat(strings.Replace(string(m[1]), ",", ".", 1), 64)
	if err != nil {
		return 0, err
	}
	return millis(time.Duration(v * float64(time.Millisecond))), nil
}

// millis rounds up: 0 means no result.
func millis(d time.Duration) int {
	return max(1, int((d+time.Millisecond-1)/time.Millisecond))
}
