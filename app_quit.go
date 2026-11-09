package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/oboo/moho/internal/sysproxy"

	"github.com/egoist/mygo"
)

// quitLimit is the most quitting may take. The steps are bounded on their
// own and normally end well before it; it is there for what no one foresaw,
// as the session may be logging out and waiting for this app.
const quitLimit = 8 * time.Second

// quitStep is one thing to put right before the app goes.
type quitStep struct {
	name string
	run  func(ctx context.Context)
}

// runQuitSteps runs the steps side by side until they are done or ctx ends,
// and returns those that did not finish. A step that panics counts as done,
// so that it cannot take the others down with it.
func runQuitSteps(ctx context.Context, steps []quitStep) (unfinished []string) {
	type result struct {
		name string
		took time.Duration
	}
	done := make(chan result, len(steps))
	for _, s := range steps {
		go func() {
			start := time.Now()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("quit: %s: %v", s.name, r)
				}
				done <- result{s.name, time.Since(start)}
			}()
			s.run(ctx)
		}()
	}
	left := make([]string, 0, len(steps))
	for _, s := range steps {
		left = append(left, s.name)
	}
	var took []string
	for len(left) > 0 {
		select {
		case r := <-done:
			took = append(took, fmt.Sprintf("%s %dms", r.name, r.took.Milliseconds()))
			left = slices.DeleteFunc(left, func(n string) bool { return n == r.name })
		case <-ctx.Done():
			log.Printf("quit: gave up on %s (%s)", strings.Join(left, ", "), strings.Join(took, ", "))
			return left
		}
	}
	log.Printf("quit: %s", strings.Join(took, ", "))
	return nil
}

// beforeQuit takes bowel off the screen at once and cleans up behind it,
// with the main thread's loop still turning so that the app is not seen
// to hang. On the main thread.
func (a *App) beforeQuit(e *mygo.QuitEvent) {
	if a.leaving {
		e.PreventDefault() // a second quit, while the first is under way
		return
	}
	a.leaving = true
	defer func() { a.leaving = false }()
	for _, w := range mygo.Windows() {
		w.Hide()
	}
	if a.tray != nil {
		a.tray.Destroy()
	}
	if runtime.GOOS == "darwin" {
		mygo.App.Dock.Hide()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.shutdown()
	}()
	pumpUntil(done)
}

func (a *App) isQuitting() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.quitting
}

// shutdown puts back what bowel changed on the system and ends the core,
// in at most quitLimit. It runs once, however often it is called. What it
// could not finish is left for the next launch: the copy of the system
// proxy stays on disk, and a core still there is killed before a new one
// starts.
func (a *App) shutdown() {
	a.quitOnce.Do(func() {
		a.mu.Lock()
		a.quitting = true
		a.mu.Unlock()
		stopMCP()
		ctx, cancel := context.WithTimeout(context.Background(), quitLimit)
		defer cancel()
		// Side by side, so that the slow ones do not add up. Any order is
		// fine: all of it goes in the same moment anyway.
		runQuitSteps(ctx, []quitStep{
			{"gateway", a.quitGateway},
			{"system proxy", a.quitSystemProxy},
			{"core", a.quitCore},
		})
	})
}

// quitGateway ends the gateway: other devices lose the network now,
// forwarding goes back as it was, and the Mac may sleep again.
func (a *App) quitGateway(context.Context) {
	if err := a.gw.Disable(); err != nil {
		log.Println("gateway:", err)
	}
}

// quitSystemProxy puts back what the system proxy had before bowel took
// it over: it must never point at a proxy that is gone.
func (a *App) quitSystemProxy(ctx context.Context) {
	a.proxyMu.Lock()
	defer a.proxyMu.Unlock()
	port := a.settings.Get().MixedPort
	s, err := sysproxy.Check(port)
	if err != nil {
		log.Println("system proxy:", err)
		return
	}
	if !s.Ours {
		a.dropProxyBackup() // another program has the proxy now
		return
	}
	if err := a.disableProxy(ctx, port); err != nil {
		log.Println("system proxy:", err)
	}
}

// quitCore ends the core for good, and saves the traffic counted so far.
func (a *App) quitCore(context.Context) {
	// TUN first: the core takes it down properly, and only then is it stopped.
	// Stopping is a kill here, and a killed core cannot take its route table
	// entries with it.
	if s := a.settings.Get(); s.Tun && a.takeoverOn(s) {
		a.closeTun()
	}
	a.core.Close()
	a.repairTun() // for what a killed core could not do, and a crash before it
	a.mu.Lock()
	cancel := a.monCancel
	a.monCancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.mon.Flush()
}
