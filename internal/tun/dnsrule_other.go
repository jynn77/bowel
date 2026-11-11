//go:build !linux

package tun

// NeedsRule is always false: only Linux leaves a polkit rule.
func NeedsRule(string) bool { return false }
