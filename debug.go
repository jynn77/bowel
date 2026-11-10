package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/settings"
)

// A development build runs beside the released app on the same Mac. Its
// data is apart already ("bowel Dev"), so what's left is the ports and the
// system's one proxy setting.
const (
	devMixedPort = 17899
	devDNSPort   = 11053
)

// separateFromRelease moves a development build off the released app's
// ports and starts it without taking over: it is turned on by hand in its
// window, for as long as that run. A port the user chose stays; the default
// one is never kept.
func (a *App) separateFromRelease() {
	core.LocalDNSPort = devDNSPort
	if _, err := a.settings.Update(func(s *settings.Settings) {
		s.Paused = true
		if s.MixedPort == settings.Defaults.MixedPort {
			s.MixedPort = devMixedPort
		}
	}); err != nil {
		log.Println("dev separation:", err)
	}
}

// debugDriver lets scripts drive a development build: with MOHO_DEBUG_DIR
// set under `mygo dev`, it runs the command written to <dir>/cmd and
// writes "ok" or an error to <dir>/out. Commands: "shot" (the main window
// to shot.png), "page <home|activity|providers>", "x <cell>" (expand a
// cell, "" to collapse), "sub <route>", "tools" (the developer tools list,
// past the first-run screen). Off otherwise.
func (a *App) debugDriver() {
	dir := os.Getenv("MOHO_DEBUG_DIR")
	if dir == "" || !mygo.IsDev() {
		return
	}
	cmdFile := filepath.Join(dir, "cmd")
	for range time.Tick(250 * time.Millisecond) {
		b, err := os.ReadFile(cmdFile)
		if err != nil {
			continue
		}
		_ = os.Remove(cmdFile)
		out := a.debugRun(dir, strings.TrimSpace(string(b)))
		_ = os.WriteFile(filepath.Join(dir, "out.tmp"), []byte(out), 0o600)
		_ = os.Rename(filepath.Join(dir, "out.tmp"), filepath.Join(dir, "out"))
	}
}

func (a *App) debugRun(dir, cmd string) string {
	u := a.uiRef()
	if u == nil || u.win == nil {
		return "error: no window"
	}
	verb, arg, _ := strings.Cut(cmd, " ")
	switch verb {
	case "shot":
		png, err := u.win.CapturePage()
		if err != nil {
			return "error: " + err.Error()
		}
		if err := os.WriteFile(filepath.Join(dir, "shot.png"), png, 0o600); err != nil {
			return "error: " + err.Error()
		}
	case "page":
		u.win.Update(func() { u.page = arg })
	case "x":
		u.win.Update(func() { u.home.x, u.home.sub = arg, "" })
	case "sub":
		u.win.Update(func() { u.home.x, u.home.sub = "routes", arg })
	case "tools":
		u.win.Update(func() {
			u.setup.on = false
			u.page, u.home.x, u.home.sub, u.home.toolsList = "home", "mode", "", true
		})
	case "show":
		a.showWindow()
	default:
		return "error: unknown command"
	}
	return "ok"
}
