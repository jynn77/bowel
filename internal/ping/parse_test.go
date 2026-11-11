package ping

import "testing"

// Every system's ping, in the languages they come in.
func TestParseTimeEverywhere(t *testing.T) {
	for out, want := range map[string]int{
		"64 bytes from 1.1.1.1: icmp_seq=1 ttl=52 time=12.345 ms\n\n--- 1.1.1.1 ping statistics ---\nrtt min/avg/max/mdev = 12.345/12.345/12.345/0.000 ms\n": 13,
		"64 bytes from 1.1.1.1: seq=0 ttl=52 time=0.412 ms\n":                                                     1,
		"Reply from 1.1.1.1: bytes=32 time=23ms TTL=52\r\n    Minimum = 23ms, Maximum = 23ms, Average = 23ms\r\n": 23,
		"Reply from 192.168.1.1: bytes=32 time<1ms TTL=64\r\n":                                                    1,
		"来自 1.1.1.1 的回复: 字节=32 时间=187ms TTL=52\r\n":                                                               187,
		"Antwort von 1.1.1.1: Bytes=32 Zeit=14ms TTL=52\r\n":                                                      14,
		"64 bytes from 1.1.1.1: icmp_seq=1 ttl=52 time=12,5 ms\n":                                                 13,
	} {
		if ms, err := parseTime([]byte(out)); err != nil || ms != want {
			t.Errorf("%q: %d, %v; want %d", out, ms, err, want)
		}
	}
	for _, out := range []string{
		"Request timed out.\r\n\r\nPing statistics for 1.1.1.1:\r\n    Packets: Sent = 1, Received = 0, Lost = 1 (100% loss),\r\n",
		"Reply from 192.168.1.23: Destination host unreachable.\r\n",
		"PING 1.1.1.1 (1.1.1.1) 56(84) bytes of data.\n\n--- 1.1.1.1 ping statistics ---\n1 packets transmitted, 0 received, 100% packet loss, time 0ms\n",
		"",
	} {
		if ms, err := parseTime([]byte(out)); err == nil {
			t.Errorf("%q read as a reply of %d ms", out, ms)
		}
	}
}
