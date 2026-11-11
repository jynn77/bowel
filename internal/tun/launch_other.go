//go:build !windows

package tun

// Launch is the app run as the launcher of Windows' second task. Nothing
// runs it anywhere else.
func Launch([]string) int { return 2 }
