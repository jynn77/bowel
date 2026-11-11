package main

// Updating bowel itself. A running program cannot swap its own code, so an
// update is a new version installed beside this one and a restart into it:
// mygo.Updater downloads it, checks its signature and puts it in place, and
// the restart goes through the ordinary quit, which puts the system proxy
// and the gateway back and ends the core. Should the new version not come
// up, the Mac is left with its own network, not a proxy that is gone.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

const (
	updateAvailable   = "available"
	updateDownloading = "downloading"
	updateRestarting  = "restarting"
	updateFailed      = "failed"

	// updateMarker says that a restart into a new version was asked for, so
	// that the next launch can tell whether it happened.
	updateMarker = "update-restart.json"
)

var (
	// After the core is up, so that the check can use its nodes.
	updateFirstCheck = 20 * time.Second
	updateEvery      = 6 * time.Hour
	updateRetry      = 30 * time.Minute
	// updateSettle is the most a restart waits for changes under way.
	updateSettle = 15 * time.Second
)

// foundUpdate is a newer version that can be installed.
type foundUpdate struct {
	Version string
	install func(ctx context.Context, progress func(done, total int64)) error
}

// osUpdate is what installs updates. Tests replace it, so that they can
// never replace or restart the app.
var osUpdate = struct {
	enabled  func() bool
	check    func(context.Context) (*foundUpdate, error)
	relaunch func()
}{
	enabled: mygo.Updater.Enabled,
	check: func(ctx context.Context) (*foundUpdate, error) {
		up, err := mygo.Updater.Check(ctx)
		if err != nil || up == nil {
			return nil, err
		}
		return &foundUpdate{Version: up.Version, install: up.Install}, nil
	},
	relaunch: func() { mygo.App.Relaunch() },
}

// updateState is where updating has got to.
type updateState struct {
	mu      sync.Mutex
	found   *foundUpdate
	status  string // "" when there is nothing to show
	percent int
	errText string
	cancel  context.CancelFunc
	done    chan struct{} // closed when the download has ended
	notify  func()        // the state changed
	// can: this copy can update itself. checking and latest are the check
	// the user asked for.
	can, checking, latest bool
	just                  *JustUpdated
	from                  string
}

func (u *updateState) changed() {
	if u.notify != nil {
		u.notify()
	}
}

// updateView is the state for the interface, nil when there is no update.
func (a *App) updateView() *UpdateView {
	u := &a.upd
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.status == "" || u.found == nil {
		return nil
	}
	return &UpdateView{Status: u.status, Version: u.found.Version, Percent: u.percent, Error: u.errText}
}

// updateLoop looks for new versions for as long as the app runs. Builds
// that cannot update themselves, as those of a package manager, never look.
func (a *App) updateLoop() {
	if !osUpdate.enabled() {
		return
	}
	a.upd.mu.Lock()
	a.upd.can = true
	a.upd.mu.Unlock()
	a.upd.changed()
	wait := updateFirstCheck
	for {
		time.Sleep(wait)
		wait = updateEvery
		if err := a.checkUpdate(); err != nil {
			log.Println("update:", err) // offline, most likely: nothing to show
			wait = updateRetry
		}
	}
}

// checkUpdate asks whether there is a newer version. It leaves an update
// under way alone, and keeps a failure on show until a newer version comes.
func (a *App) checkUpdate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	up, err := osUpdate.check(ctx)
	if err != nil {
		return err
	}
	u := &a.upd
	u.mu.Lock()
	switch {
	case u.status == updateDownloading || u.status == updateRestarting:
	case up == nil:
		u.found, u.status, u.errText = nil, "", ""
	case u.status == updateFailed && u.found != nil && u.found.Version == up.Version:
		u.found = up
	default:
		u.found, u.status, u.errText = up, updateAvailable, ""
	}
	u.mu.Unlock()
	u.changed()
	return nil
}

// startUpdate downloads the update and restarts into it: the click on 升级
// asks for both. Anything but an update waiting to start is ignored, so
// that a second click does nothing.
func (a *App) startUpdate() {
	if a.isQuitting() {
		return
	}
	u := &a.upd
	u.mu.Lock()
	if u.found == nil || (u.status != updateAvailable && u.status != updateFailed) {
		u.mu.Unlock()
		return
	}
	found := u.found
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	u.status, u.percent, u.errText, u.cancel, u.done = updateDownloading, 0, "", cancel, done
	u.mu.Unlock()
	u.changed()

	go func() {
		defer close(done)
		defer cancel()
		err := found.install(ctx, a.updateProgress)
		u.mu.Lock()
		switch {
		case err == nil:
			u.status = updateRestarting
		case errors.Is(err, context.Canceled) || ctx.Err() != nil:
			u.status, u.percent = updateAvailable, 0
		default:
			log.Println("update:", err)
			u.status, u.percent, u.errText = updateFailed, 0, err.Error()
		}
		u.mu.Unlock()
		u.changed()
		if err == nil {
			a.restartForUpdate(ctx, found.Version)
		}
	}()
}

// updateProgress keeps the percentage. Whole percents only, as each one
// rebuilds the snapshot. A failed delta makes the download start over, so
// it may go down.
func (a *App) updateProgress(done, total int64) {
	if total <= 0 {
		return
	}
	pct := min(int(done*100/total), 100)
	u := &a.upd
	u.mu.Lock()
	same := u.percent == pct
	u.percent = pct
	u.mu.Unlock()
	if !same {
		u.changed()
	}
}

// cancelUpdate stops a download. Once it has ended there is nothing to stop.
func (a *App) cancelUpdate() {
	u := &a.upd
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.status == updateDownloading && u.cancel != nil {
		u.cancel()
	}
}

// restartForUpdate restarts into the version just installed, once what is
// being changed has been saved, and leaves word for the next launch.
func (a *App) restartForUpdate(ctx context.Context, version string) {
	wait, cancel := context.WithTimeout(ctx, updateSettle)
	defer cancel()
	a.settle(wait)
	if a.isQuitting() {
		return // the user quit first; the new version runs at the next launch
	}
	if err := a.writeUpdateMarker(updateRestart{From: mygo.App.Version(), To: version}); err != nil {
		log.Println("update:", err) // only the message after the restart is lost
	}
	osUpdate.relaunch()
}

// settle waits for the actions under way, and a change of settings, to end.
func (a *App) settle(ctx context.Context) {
	for {
		a.mu.Lock()
		n := len(a.pending)
		a.mu.Unlock()
		if n == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	done := make(chan struct{})
	go func() {
		a.setMu.Lock()
		defer a.setMu.Unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// quitUpdate ends a download, which removes its files. A restart for an
// update is this very quit, and has nothing left to end.
func (a *App) quitUpdate(ctx context.Context) {
	u := &a.upd
	u.mu.Lock()
	status, cancel, done := u.status, u.cancel, u.done
	u.mu.Unlock()
	if status != updateDownloading || cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// updateRestart is the content of updateMarker.
type updateRestart struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (a *App) writeUpdateMarker(m updateRestart) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	path := filepath.Join(a.dataDir, updateMarker)
	if err := os.WriteFile(path+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// updateLast keeps the update this version came from, for the changelog.
const updateLast = "update-last.json"

// loadUpdateOutcome reads what the last run asked for, once, and what this
// version was updated from.
func (a *App) loadUpdateOutcome(current string) {
	dir := a.dataDir
	var just *JustUpdated
	path := filepath.Join(dir, updateMarker)
	if b, err := os.ReadFile(path); err == nil {
		_ = os.Remove(path)
		var m updateRestart
		if json.Unmarshal(b, &m) == nil && m.To != "" {
			just = &JustUpdated{From: m.From, To: m.To, OK: m.To == current}
			log.Printf("update: asked for %s, running %s", m.To, current)
			if just.OK {
				if b, err := json.Marshal(m); err == nil {
					_ = os.WriteFile(filepath.Join(dir, updateLast), b, 0o600)
				}
			}
		}
	}
	from := ""
	if b, err := os.ReadFile(filepath.Join(dir, updateLast)); err == nil {
		var m updateRestart
		// Only while this is still the version it led to: installed by hand
		// since, it says nothing about this one.
		if json.Unmarshal(b, &m) == nil && m.To == current {
			from = m.From
		}
	}
	u := &a.upd
	u.mu.Lock()
	u.just, u.from = just, from
	u.mu.Unlock()
}

// dismissUpdated takes the word about the last update away.
func (a *App) dismissUpdated() {
	u := &a.upd
	u.mu.Lock()
	u.just = nil
	u.mu.Unlock()
	u.changed()
}

func (a *App) updateInfo() UpdateInfo {
	u := &a.upd
	u.mu.Lock()
	defer u.mu.Unlock()
	return UpdateInfo{Can: u.can, Checking: u.checking, Latest: u.latest, Just: u.just, From: u.from}
}

// checkUpdateNow looks for a new version because the user asked: what it
// finds shows as the periodic check's does, and "nothing newer" is said too.
func (a *App) checkUpdateNow() error {
	u := &a.upd
	u.mu.Lock()
	if u.checking {
		u.mu.Unlock()
		return nil
	}
	u.checking, u.latest = true, false
	u.mu.Unlock()
	u.changed()
	err := a.checkUpdate()
	u.mu.Lock()
	u.checking = false
	u.latest = err == nil && u.found == nil
	u.mu.Unlock()
	u.changed()
	return err
}
