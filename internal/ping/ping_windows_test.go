//go:build windows

package ping

import "testing"

func TestParseTimeWindows(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want int
		bad  bool
	}{
		{"english", "Reply from 1.1.1.1: bytes=32 time=12ms TTL=56\r\n", 12, false},
		{"sub-millisecond", "Reply from 1.1.1.1: bytes=32 time<1ms TTL=56\r\n", 1, false},
		{"spaced", "Reply from 1.1.1.1: bytes=32 time = 12 ms TTL=56\r\n", 12, false},
		{"chinese", "来自 1.1.1.1 的回复: 字节=32 时间=12ms TTL=56\r\n", 12, false},
		{"japanese", "1.1.1.1 からの応答: バイト数 =32 時間 =12ms TTL=56\r\n", 12, false},
		{"fractional", "Reply from 1.1.1.1: bytes=32 time=12.345ms TTL=56\r\n", 13, false},
		{"timeout", "Request timed out.\r\n\r\nPackets: Sent = 1, Received = 0, Lost = 1\r\n", 0, true},
		{"no time", "Reply from 1.1.1.1: bytes=32 TTL=56\r\n", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ms, err := parseTime([]byte(tc.out))
			if (err != nil) != tc.bad || (!tc.bad && ms != tc.want) {
				t.Fatalf("got %d, %v; want %d, bad=%v", ms, err, tc.want, tc.bad)
			}
		})
	}
}
