package main

// `bowel daemon`: bowel without a window, for servers, SSH and scripts.
// The core, the monitors and the two sockets (agents' and the command
// line's) run as in the app; the window, the menu bar icon, the Dock icon
// and updating itself do not, since MyGo's loop is never started. It stays
// in the foreground until it is told to stop (SIGINT, SIGTERM or
// `bowel stop`), which is what systemd, launchd and a Windows service
// wrapper expect of what they run, and leaves as the app does: the system
// proxy and the gateway go back, and the core ends.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/egoist/mygo"

	"github.com/oboo/moho/internal/i18n"
)

// dataDirEnv names a data folder other than the user's default: for a
// service's own, or to try things beside the app.
const dataDirEnv = "BOWEL_DATA_DIR"

// ownDataDir is whether the data folder was named (dataDirEnv, --data-dir).
var ownDataDir bool

// useDataDir makes dir the data folder of this run.
func useDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mygo.App.SetPath(mygo.PathUserData, dir)
	ownDataDir = true
	return nil
}

// running says whether a bowel serves the data folder already.
func running(data string) bool {
	for _, p := range []string{cliSocket(data), mcpSocket(data)} {
		if c, err := net.DialTimeout("unix", p, time.Second); err == nil {
			c.Close()
			return true
		}
	}
	return false
}

func runDaemon([]string) error {
	data, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return err
	}
	already := errors.New(i18n.T("bowel 已经在运行；用 `bowel status` 查看，或先 `bowel stop`", "bowel is already running; see `bowel status`, or `bowel stop` it first"))
	if running(data) {
		return already
	}
	// The app's own lock, so that opening the app beside the daemon hands
	// over instead of starting a second core. A data folder of its own has
	// only its sockets to tell.
	if !ownDataDir && !mygo.App.RequestSingleInstanceLock() {
		return already
	}
	a, err := newApp()
	if err != nil {
		return err
	}
	a.headless, a.stop = true, make(chan struct{})
	if mygo.IsDev() {
		a.separateFromRelease()
	}
	// No window to ask in: an agent's change under "ask" is refused, and
	// says how the user can let it through.
	askAgent = func(context.Context, string, string, string) (bool, bool, error) {
		return false, false, errors.New(i18n.T("bowel 在无窗口模式下运行，没法弹窗询问。请让用户自己执行这条命令，或用 `bowel settings set mcpPolicy allow` 允许 agent 变更。",
			"bowel runs without a window and can't ask. Have the user run this command, or allow agents' changes with `bowel settings set mcpPolicy allow`."))
	}
	a.startHeadless()
	log.Printf("bowel daemon: pid %d, data %s, port %d", os.Getpid(), data, a.settings.Get().MixedPort)

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case s := <-sig:
		log.Printf("bowel daemon: %v, stopping", s)
	case <-a.stop:
		log.Print("bowel daemon: stopping")
	}
	go func() { // a second signal does not wait
		<-sig
		os.Exit(1)
	}()
	a.shutdown()
	return nil
}

// startHeadless is start without what needs the window or MyGo's loop.
func (a *App) startHeadless() {
	a.upd.notify = a.emit
	a.copyGeo()
	go func() {
		a.detectDev(2 * time.Second)
		if err := a.applyConfig(); err != nil {
			log.Println("core:", err)
		}
	}()
	go a.loop()
	go a.devLoop()
	a.startMCP()
	a.startCLI()
}

// cliVersion is this executable's version, for `bowel version`.
func cliVersion() string {
	if v := mygo.App.Version(); v != "" {
		return v
	}
	return "dev"
}

// cliLanguage is the language commands speak: the app's own, as saved, so
// that both say the same words.
func cliLanguage(data string) string {
	if l := os.Getenv("BOWEL_LANG"); i18n.Known(l) {
		return l
	}
	return savedLanguage(data)
}

// savedLanguage is settings.json's language; with none saved yet, the
// one the environment asks for (LC_ALL, LC_MESSAGES, LANG), else English.
func savedLanguage(data string) string {
	var st struct {
		Language string `json:"language"`
	}
	if b, err := os.ReadFile(filepath.Join(data, "settings.json")); err == nil {
		if json.Unmarshal(b, &st) == nil && i18n.Known(st.Language) {
			return st.Language
		}
		return "zh" // the app's default, which it speaks then
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.ToLower(os.Getenv(k))
		switch {
		case v == "":
			continue
		case strings.HasPrefix(v, "zh_tw"), strings.HasPrefix(v, "zh_hk"), strings.Contains(v, "hant"):
			return "zh-Hant"
		case strings.HasPrefix(v, "zh"):
			return "zh"
		case strings.HasPrefix(v, "ja"):
			return "ja"
		}
		break
	}
	return "en"
}
