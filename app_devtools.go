package main

// Developer tools: which of them reach the proxy, kept fresh for the 模式
// cell, and the changes to them. See internal/devtools.

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/devtools"
)

type toolsState struct {
	run   sync.Mutex // one detection at a time
	mu    sync.Mutex
	list  []devtools.Tool
	ticks int
	// sources are where the package tools download from. Reading them is a
	// few small files, done whenever they are on screen; measuring them is
	// a dozen requests, done every few minutes and when one is changed.
	sources  []devtools.Source
	probing  sync.Mutex // one measurement at a time
	probedAt time.Time
}

// toolsEvery is how many devLoop ticks pass between detections: reading a
// tool starts an interactive zsh, too much to do every 20 seconds.
const toolsEvery = 15

func (a *App) toolOptions() devtools.Options {
	home, _ := os.UserHomeDir()
	s := a.settings.Get()
	return devtools.Options{Home: home, DataDir: a.dataDir, Port: s.MixedPort, NoProxy: overlayOf(s, a.devEnv()).NoProxy}
}

// refreshTools reads the tools again, unless that is already happening.
func (a *App) refreshTools() {
	if !a.tools.run.TryLock() {
		return
	}
	defer a.tools.run.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	list := devtools.Detect(ctx, a.toolOptions())
	a.tools.mu.Lock()
	a.tools.list = list
	a.tools.mu.Unlock()
	a.emit()
}

func (a *App) toolsView() []devtools.Tool {
	a.tools.mu.Lock()
	defer a.tools.mu.Unlock()
	return a.tools.list
}

// refreshSources reads the download sources again, and says so only when
// one changed: the page that shows them asks every few seconds.
func (a *App) refreshSources() {
	list := devtools.Sources(a.toolOptions())
	a.tools.mu.Lock()
	same := slices.EqualFunc(a.tools.sources, list, func(x, y devtools.Source) bool {
		return x.ID == y.ID && x.Mirror == y.Mirror && x.File == y.File
	})
	// What was measured still holds: it covers every way, in use or not.
	for i := range list {
		for _, old := range a.tools.sources {
			if old.ID == list[i].ID {
				list[i].Probes = old.Probes
			}
		}
	}
	a.tools.sources = list
	a.tools.mu.Unlock()
	if !same {
		a.emit()
	}
}

// probeSources measures every way of reaching each source, unless that is
// already happening. The official address is asked through bowel only
// while the core is running: otherwise nothing is known about it.
func (a *App) probeSources() {
	if !a.tools.probing.TryLock() {
		return
	}
	defer a.tools.probing.Unlock()
	a.refreshSources()
	port := 0
	if a.core.Status().State == core.Running {
		port = a.settings.Get().MixedPort
	}
	prober := devtools.NewProber(port)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list := a.sourcesView()
	probes := make([][]devtools.Probe, len(list))
	var wg sync.WaitGroup
	for i, s := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			probes[i] = prober.Probe(ctx, s)
		}()
	}
	wg.Wait()
	a.tools.mu.Lock()
	for i, s := range list {
		for j := range a.tools.sources {
			if a.tools.sources[j].ID == s.ID {
				a.tools.sources[j].Probes = probes[i]
			}
		}
	}
	a.tools.probedAt = time.Now()
	a.tools.mu.Unlock()
	a.emit()
}

// probeSourcesIfOlder measures again when the last time was longer ago.
func (a *App) probeSourcesIfOlder(d time.Duration) {
	a.tools.mu.Lock()
	old := time.Since(a.tools.probedAt) > d
	a.tools.mu.Unlock()
	if old {
		a.probeSources()
	}
}

// setSource points a tool at a mirror, or with "" at its official source.
func (a *App) setSource(id, mirror string) error {
	defer a.begin("source:" + id)()
	if err := devtools.SetMirror(a.toolOptions(), id, mirror); err != nil {
		return err
	}
	a.refreshSources()
	go a.probeSources()
	return nil
}

func (a *App) sourcesView() []devtools.Source {
	a.tools.mu.Lock()
	defer a.tools.mu.Unlock()
	return a.tools.sources
}

// restartColima restarts Colima if it is running: stopped, it reads the
// file when it next starts.
func (a *App) restartColima(o devtools.Options) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if !devtools.ColimaRunning(ctx, o) {
		return nil
	}
	return devtools.RestartColima(ctx, o)
}

// changeTool does one of devtools' kinds to a tool.
func (a *App) changeTool(id, kind string) error {
	defer a.begin("tool:" + id)()
	o := a.toolOptions()
	var err error
	switch kind {
	case devtools.KindApply, devtools.KindUpdate:
		err = devtools.Apply(o, id)
	case devtools.KindDirect:
		err = devtools.Direct(o, id)
	case devtools.KindUndo:
		_, err = devtools.Undo(o, id)
	default:
		err = fmt.Errorf("devtools: unknown change %q", kind)
	}
	if err != nil {
		return err
	}
	if id == devtools.Dockerd {
		err = a.restartColima(o)
	}
	a.refreshTools()
	return err
}

// undoAllTools removes everything bowel wrote.
func (a *App) undoAllTools() error {
	defer a.begin("tools")()
	o := a.toolOptions()
	changed, err := devtools.UndoAll(o)
	for _, id := range changed {
		if id == devtools.Dockerd {
			if rerr := a.restartColima(o); rerr != nil && err == nil {
				err = rerr
			}
		}
	}
	a.refreshTools()
	return err
}

// runDevtoolsCLI is `bowel devtools off`: the way out when bowel will not
// open, or is being uninstalled.
func runDevtoolsCLI(args []string) error {
	if len(args) != 1 || args[0] != "off" {
		return fmt.Errorf("usage: bowel devtools off")
	}
	data, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	changed, err := devtools.UndoAll(devtools.Options{Home: home, DataDir: data})
	if err != nil {
		log.Println(err)
	}
	if len(changed) == 0 && err == nil {
		fmt.Println("bowel had not written anything.")
	}
	for _, id := range changed {
		fmt.Println("removed:", id)
		if id == devtools.Dockerd {
			fmt.Println("Colima must restart for this to take effect.")
		}
	}
	return err
}
