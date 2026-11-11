//go:build !linux && !windows

package netif

// Tunnel is always false where names say what an interface is.
func Tunnel(string) bool { return false }
