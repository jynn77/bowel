//go:build !windows

// The login item, off Windows: the framework's own, which the Mac gets
// through SMAppService and Linux through XDG autostart.
package main

import "github.com/egoist/mygo"

func setOpenAtLoginOS(on bool) error {
	return mygo.App.SetOpenAtLogin(on)
}

func openAtLoginOS() bool {
	return mygo.App.OpenAtLogin()
}
