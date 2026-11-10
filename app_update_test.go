package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeUpdates replaces what installs updates, and reports each relaunch.
func fakeUpdates(t *testing.T) chan struct{} {
	t.Helper()
	saved, settle := osUpdate, updateSettle
	t.Cleanup(func() { osUpdate, updateSettle = saved, settle })
	relaunched := make(chan struct{}, 1)
	osUpdate.relaunch = func() { relaunched <- struct{}{} }
	return relaunched
}

func updateApp(t *testing.T) *App {
	t.Helper()
	return &App{dataDir: t.TempDir(), pending: map[string]bool{}}
}

func offer(a *App, install func(context.Context, func(done, total int64)) error) {
	a.upd.found, a.upd.status = &foundUpdate{Version: "9.9.9", install: install}, updateAvailable
}

func waitDone(t *testing.T, a *App) {
	t.Helper()
	a.upd.mu.Lock()
	done := a.upd.done
	a.upd.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the update did not end")
	}
}

func TestCheckUpdateOffersAndWithdraws(t *testing.T) {
	fakeUpdates(t)
	a := updateApp(t)
	osUpdate.check = func(context.Context) (*foundUpdate, error) { return &foundUpdate{Version: "9.9.9"}, nil }
	if err := a.checkUpdate(); err != nil {
		t.Fatal(err)
	}
	if v := a.updateView(); v == nil || v.Status != updateAvailable || v.Version != "9.9.9" {
		t.Fatalf("view = %+v", v)
	}
	osUpdate.check = func(context.Context) (*foundUpdate, error) { return nil, nil }
	if err := a.checkUpdate(); err != nil {
		t.Fatal(err)
	}
	if v := a.updateView(); v != nil {
		t.Fatalf("view = %+v", v)
	}
}

// TestCheckLeavesDownloadAlone: a periodic check cannot take away an update
// that is being installed.
func TestCheckLeavesDownloadAlone(t *testing.T) {
	fakeUpdates(t)
	a := updateApp(t)
	a.upd.found, a.upd.status = &foundUpdate{Version: "9.9.9"}, updateDownloading
	osUpdate.check = func(context.Context) (*foundUpdate, error) { return nil, nil }
	if err := a.checkUpdate(); err != nil {
		t.Fatal(err)
	}
	if v := a.updateView(); v == nil || v.Status != updateDownloading {
		t.Fatalf("view = %+v", v)
	}
}

func TestUpdateInstallsThenRelaunches(t *testing.T) {
	relaunched := fakeUpdates(t)
	a := updateApp(t)
	offer(a, func(_ context.Context, progress func(int64, int64)) error {
		progress(50, 100)
		if got := a.updateView().Percent; got != 50 {
			t.Errorf("percent = %d", got)
		}
		return nil
	})
	a.startUpdate()
	a.startUpdate() // a second click starts nothing
	waitDone(t, a)
	select {
	case <-relaunched:
	default:
		t.Fatal("did not relaunch")
	}
	if v := a.updateView(); v.Status != updateRestarting {
		t.Fatalf("view = %+v", v)
	}
	if !exists(filepath.Join(a.dataDir, updateMarker)) {
		t.Error("no word for the next launch")
	}
}

func TestUpdateFailureNeverRelaunches(t *testing.T) {
	relaunched := fakeUpdates(t)
	a := updateApp(t)
	offer(a, func(context.Context, func(int64, int64)) error { return errors.New("bad signature") })
	a.startUpdate()
	waitDone(t, a)
	if v := a.updateView(); v.Status != updateFailed || v.Error != "bad signature" {
		t.Fatalf("view = %+v", v)
	}
	select {
	case <-relaunched:
		t.Fatal("relaunched after a failure")
	default:
	}
	if exists(filepath.Join(a.dataDir, updateMarker)) {
		t.Error("marker written after a failure")
	}
}

func TestCancelGoesBackToOffer(t *testing.T) {
	relaunched := fakeUpdates(t)
	a := updateApp(t)
	started := make(chan struct{})
	offer(a, func(ctx context.Context, _ func(int64, int64)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	a.startUpdate()
	<-started
	a.cancelUpdate()
	waitDone(t, a)
	if v := a.updateView(); v.Status != updateAvailable {
		t.Fatalf("view = %+v", v)
	}
	select {
	case <-relaunched:
		t.Fatal("relaunched after a cancel")
	default:
	}
}

// TestRestartWaitsForChanges: no restart in the middle of a settings change.
func TestRestartWaitsForChanges(t *testing.T) {
	relaunched := fakeUpdates(t)
	a := updateApp(t)
	a.setMu.Lock()
	go a.restartForUpdate(context.Background(), "9.9.9")
	select {
	case <-relaunched:
		t.Fatal("restarted during a change")
	case <-time.After(100 * time.Millisecond):
	}
	a.setMu.Unlock()
	select {
	case <-relaunched:
	case <-time.After(5 * time.Second):
		t.Fatal("never restarted")
	}
}

// TestRestartNotHeldForever: an action that never ends costs the limit.
func TestRestartNotHeldForever(t *testing.T) {
	relaunched := fakeUpdates(t)
	updateSettle = 50 * time.Millisecond
	a := updateApp(t)
	a.pending["stuck"] = true
	a.restartForUpdate(context.Background(), "9.9.9")
	select {
	case <-relaunched:
	default:
		t.Fatal("did not restart")
	}
}

func TestNoRestartOnceQuitting(t *testing.T) {
	relaunched := fakeUpdates(t)
	a := updateApp(t)
	a.quitting = true
	a.restartForUpdate(context.Background(), "9.9.9")
	select {
	case <-relaunched:
		t.Fatal("relaunched while quitting")
	default:
	}
}

func TestQuitEndsDownload(t *testing.T) {
	fakeUpdates(t)
	a := updateApp(t)
	offer(a, func(ctx context.Context, _ func(int64, int64)) error {
		<-ctx.Done()
		return ctx.Err()
	})
	a.startUpdate()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.quitUpdate(ctx)
	if ctx.Err() != nil {
		t.Fatal("quit waited out its limit")
	}
}

func TestUpdateOutcome(t *testing.T) {
	a := updateApp(t)
	a.loadUpdateOutcome("1.0.0")
	if i := a.updateInfo(); i.Just != nil || i.From != "" {
		t.Fatalf("no marker: %+v", i)
	}
	if err := a.writeUpdateMarker(updateRestart{From: "1.0.0", To: "2.0.0"}); err != nil {
		t.Fatal(err)
	}
	a.loadUpdateOutcome("2.0.0")
	if i := a.updateInfo(); i.Just == nil || !i.Just.OK || i.Just.From != "1.0.0" || i.From != "1.0.0" {
		t.Fatalf("updated: %+v", i)
	}
	// The next launch says nothing, and still knows the version before.
	a.loadUpdateOutcome("2.0.0")
	if i := a.updateInfo(); i.Just != nil || i.From != "1.0.0" {
		t.Fatalf("relaunched: %+v", i)
	}
	// Installed by hand since: the version before is no longer known.
	a.loadUpdateOutcome("3.0.0")
	if i := a.updateInfo(); i.From != "" {
		t.Fatalf("by hand: %+v", i)
	}
}

func TestUpdateDidNotHappen(t *testing.T) {
	a := updateApp(t)
	if err := a.writeUpdateMarker(updateRestart{From: "1.0.0", To: "2.0.0"}); err != nil {
		t.Fatal(err)
	}
	a.loadUpdateOutcome("1.0.0")
	if i := a.updateInfo(); i.Just == nil || i.Just.OK || i.From != "" {
		t.Fatalf("%+v", i)
	}
	a.dismissUpdated()
	if i := a.updateInfo(); i.Just != nil {
		t.Fatalf("dismissed: %+v", i)
	}
}

func TestUpdateOutcomeSurvivesGarbage(t *testing.T) {
	a := updateApp(t)
	path := filepath.Join(a.dataDir, updateMarker)
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.loadUpdateOutcome("1.0.0")
	if i := a.updateInfo(); i.Just != nil {
		t.Fatalf("%+v", i)
	}
	if exists(path) {
		t.Error("marker left behind")
	}
}

func TestCheckNowSaysLatest(t *testing.T) {
	fakeUpdates(t)
	a := updateApp(t)
	osUpdate.check = func(context.Context) (*foundUpdate, error) { return nil, nil }
	if err := a.checkUpdateNow(); err != nil {
		t.Fatal(err)
	}
	if i := a.updateInfo(); !i.Latest || i.Checking {
		t.Fatalf("%+v", i)
	}
	osUpdate.check = func(context.Context) (*foundUpdate, error) { return nil, errors.New("offline") }
	if err := a.checkUpdateNow(); err == nil {
		t.Fatal("no error")
	}
	if i := a.updateInfo(); i.Latest || i.Checking {
		t.Fatalf("failed check: %+v", i)
	}
}

const testChangelog = `# Title

intro

## Unreleased

- not yet

## [0.3.0] - 2026-12-01

- three

## [0.2.0] - 2026-11-09

- two a
- two b

## 0.1.0

- one
`

func TestParseChangelog(t *testing.T) {
	all := parseChangelog(testChangelog)
	if len(all) != 3 || all[0].Version != "0.3.0" || all[0].Date != "2026-12-01" || len(all[1].Items) != 2 || all[2].Version != "0.1.0" || all[2].Date != "" {
		t.Fatalf("%+v", all)
	}
	for _, c := range []struct {
		from string
		want int
	}{{"0.2.0", 2}, {"0.3.0", 1}, {"", 3}, {"9.9.9", 3}} {
		if got := len(changelogSince(all, c.from)); got != c.want {
			t.Errorf("since %q: %d", c.from, got)
		}
	}
}

// TestChangelogHasThisVersion: every version says what it changed, and the
// section is what a signed build takes the update's notes from.
func TestChangelogHasThisVersion(t *testing.T) {
	b, err := os.ReadFile("mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, e := range parseChangelog(changelogText) {
		if e.Version == cfg.Version && len(e.Items) > 0 {
			return
		}
	}
	t.Errorf("CHANGELOG.md has no section for %s", cfg.Version)
}
