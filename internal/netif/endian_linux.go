package netif

import "unsafe"

// bigEndian is the machine's byte order (s390x, among those Go runs on).
var bigEndian = func() bool {
	x := uint16(1)
	return *(*byte)(unsafe.Pointer(&x)) == 0
}()
