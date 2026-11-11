//go:build !windows

package main

// attachConsole is for Windows, where a GUI program has no console.
func attachConsole() {}
