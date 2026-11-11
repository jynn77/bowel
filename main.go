// Moho is a macOS client for the mihomo proxy core.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/egoist/mygo"

	"github.com/oboo/moho/internal/cli"
	"github.com/oboo/moho/internal/tun"
)

func main() {
	// Windows' second TUN task runs a copy of this program, elevated, to
	// start the core (tun.Launch): before anything else, so that nothing
	// of the app runs with those rights.
	if len(os.Args) > 1 && os.Args[1] == tun.LauncherArg {
		os.Exit(tun.Launch(os.Args[2:]))
	}
	// A data folder other than the user's default, for every way in.
	if dir := os.Getenv(dataDirEnv); dir != "" {
		if err := useDataDir(dir); err != nil {
			fmt.Fprintln(os.Stderr, "bowel:", err)
			os.Exit(1)
		}
	}
	// Commands (cli.go): `bowel status`, `bowel daemon`, `bowel mcp`, …
	// Without one, or with what a launcher passes, the app opens.
	if !cli.WantsGUI(os.Args[1:]) {
		os.Exit(runCLI(os.Args[1:]))
	}
	// One app per user, for the data folder, the core and the system proxy
	// are its alone. This one hands over to the app that is running. After
	// the commands above, which run beside the app; a development build has
	// a lock of its own, as it has a data folder of its own.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	app, err := newApp()
	if err != nil {
		log.Fatal(err)
	}
	if mygo.IsDev() {
		app.separateFromRelease()
	}
	mygo.App.OnSecondInstance(app.onSecondInstance)
	mygo.App.WhenReady(app.start)
	// The window hides to the menu bar; quitting comes from the menu.
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnBeforeQuit(app.beforeQuit)

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
