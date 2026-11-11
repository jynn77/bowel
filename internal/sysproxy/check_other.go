//go:build !darwin && !windows && !linux

package sysproxy

// Check has nothing to read where Enable does nothing: it reports the
// proxy as off.
func Check(port int) (Status, error) { return Status{}, nil }
