package main

// Developer networks: what internal/devnet found of this machine, kept in
// step with the core and the system proxy. See docs/dev-network.md.

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/oboo/moho/internal/core"
	"github.com/oboo/moho/internal/devnet"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/mihomo"
	"github.com/oboo/moho/internal/routes"
	"github.com/oboo/moho/internal/settings"
	"github.com/oboo/moho/internal/sysproxy"
)

// devDetect reads the machine's networks. Tests replace it.
var devDetect = (&devnet.Detector{}).Detect

// devEvery is how soon a network that came or went is noticed.
const devEvery = 20 * time.Second

// ipv6Watch finds out whether the network has IPv6. Tests replace it.
var ipv6Watch interface {
	Check(primary, forced string) devnet.IPv6State
	Reset()
} = &devnet.IPv6Watcher{}

type devState struct {
	mu  sync.Mutex
	env devnet.Env
	// tried is the overlay last given to the core, by its fingerprint, so
	// that one it refused is not offered again every few seconds.
	tried string
	// triedIPv6 is what automatic IPv6 had found for the config last given.
	triedIPv6 bool
	// fellBack: the core refused what was detected, and has the catalog.
	fellBack bool
	// bypass is the list the system proxy was last given.
	bypass []string
}

func (a *App) devEnv() devnet.Env {
	a.dev.mu.Lock()
	defer a.dev.mu.Unlock()
	return a.dev.env
}

// detectDev reads the machine within limit.
func (a *App) detectDev(limit time.Duration) devnet.Env {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	env := devDetect(ctx)
	env.IPv6 = readIPv6(a.settings.Get(), env.PrimaryIface)
	a.dev.mu.Lock()
	a.dev.env = env
	a.dev.mu.Unlock()
	return env
}

// readIPv6 asks about the network only for Auto, the one mode that needs it.
func readIPv6(s settings.Settings, primary string) devnet.IPv6State {
	if s.IPv6 != core.IPv6Auto {
		ipv6Watch.Reset()
		return devnet.IPv6State{}
	}
	return ipv6Watch.Check(primary, s.Interface)
}

// ensureIPv6 makes sure Auto is known before a config depends on it, as
// when the setting has just been changed to it.
func (a *App) ensureIPv6(s settings.Settings) {
	env := a.devEnv()
	if s.IPv6 != core.IPv6Auto || env.IPv6.Known() {
		return
	}
	found := readIPv6(s, env.PrimaryIface)
	a.dev.mu.Lock()
	a.dev.env.IPv6 = found
	a.dev.mu.Unlock()
}

// overlayOf is what s and env add to the core's config and the bypass list.
func overlayOf(s settings.Settings, env devnet.Env) devnet.Overlay {
	if s.DevOff {
		return devnet.Overlay{}
	}
	var proxied []string
	if s.ImportedRouting == "" { // the user's rules are paused under an imported config
		proxied = proxiedRules(routes.Build(s.Routing, s.Rules), s.Rules)
	}
	return devnet.Compile(devnet.Options{Disabled: s.DevOffScenarios, Extra: s.DevExtra, Proxied: proxied}, env)
}

// proxiedRules are the user's active rules that lead to a proxy or a block,
// which the overlay must leave a way for.
func proxiedRules(all []routes.Route, users []routes.UserRule) []string {
	var out []string
	for _, u := range routes.Active(all, users) {
		i := slices.IndexFunc(all, func(r routes.Route) bool { return r.ID == u.Route })
		if i >= 0 && !all[i].Direct {
			out = append(out, u.Mihomo())
		}
	}
	return out
}

// bypassOf is the system proxy's bypass list under s.
func (a *App) bypassOf(s settings.Settings) []string {
	return overlayOf(s, a.devEnv()).MergeBypass(s.Bypass)
}

// applyDev gives the core the config build makes of the overlay. What the
// machine happens to look like must not cost the user their network: if
// the core refuses it, the catalog alone goes in, and the settings page
// says so.
func (a *App) applyDev(s settings.Settings, build func(devnet.Overlay) ([]byte, error)) error {
	env := a.devEnv()
	dev := overlayOf(s, env)
	cfg, err := build(dev)
	if err != nil {
		return err
	}
	err = a.core.Apply(cfg)
	if a.leaveTunCore(err) {
		if cfg, err = build(dev); err != nil {
			return err
		}
		err = a.core.Apply(cfg)
	}
	fellBack := false
	if err != nil && env.Detected() {
		log.Println("developer networks: the core refused what was detected:", err)
		if cfg, berr := build(overlayOf(s, devnet.Env{})); berr == nil && a.core.Apply(cfg) == nil {
			err, fellBack = nil, true
		}
	}
	a.dev.mu.Lock()
	a.dev.tried, a.dev.triedIPv6, a.dev.fellBack = dev.Fingerprint(), env.IPv6.Usable, fellBack
	a.dev.mu.Unlock()
	return err
}

// devLoop follows the machine's networks.
func (a *App) devLoop() {
	t := time.NewTicker(devEvery)
	defer t.Stop()
	for range t.C {
		if a.isQuitting() {
			return
		}
		a.devSync()
		// Without a window nothing shows the tools or the sources.
		if a.tools.ticks++; a.tools.ticks%toolsEvery == 0 && !a.headless {
			a.refreshTools()
			a.probeSources()
		}
	}
}

// devSync reads the machine again and applies what changed. A reload keeps
// the connections there are, so it interrupts nothing, unless Auto changes
// IPv6 under TUN: that makes the core open the TUN anew.
func (a *App) devSync() {
	env := a.detectDev(8 * time.Second)
	s := a.settings.Get()
	a.dev.mu.Lock()
	tried, triedIPv6 := a.dev.tried, a.dev.triedIPv6
	a.dev.mu.Unlock()
	ipv6Changed := s.IPv6 == core.IPv6Auto && env.IPv6.Usable != triedIPv6
	if a.core.Status().State == core.Running && (ipv6Changed || overlayOf(s, env).Fingerprint() != tried) && a.setMu.TryLock() {
		if ipv6Changed {
			log.Printf("automatic IPv6: usable=%v iface=%s reason=%s", env.IPv6.Usable, env.IPv6.Iface, env.IPv6.Reason)
		}
		err := a.applyConfig()
		a.setMu.Unlock()
		if err != nil {
			log.Println("developer networks:", err)
		}
	}
	a.syncBypass()
}

// syncBypass gives the system proxy the bypass list as it stands, if it
// differs and the system proxy is still bowel's. A write of bowel's that the
// OS refused gets its next try here too, since nothing else comes round.
func (a *App) syncBypass() {
	a.proxyMu.Lock()
	defer a.proxyMu.Unlock()
	if a.isQuitting() {
		return
	}
	s := a.settings.Get()
	if !a.takeoverOn(s) || !s.SystemProxy {
		return
	}
	a.retryWrite(s)
	a.dev.mu.Lock()
	last := a.dev.bypass
	a.dev.mu.Unlock()
	if slices.Equal(last, a.bypassOf(s)) {
		return
	}
	if cur, err := sysproxy.Check(s.MixedPort); err != nil || !cur.Ours {
		return
	}
	if err := a.enableProxy(s); err != nil {
		log.Println("system proxy:", err)
	}
}

// closeRerouted closes the connections a change of mode sends elsewhere.
// Those of this machine's own networks go the same way in every mode, and
// an SSH session has no reason to drop.
func closeRerouted(ctx context.Context, c *mihomo.Client, dev devnet.Overlay) {
	snap, err := c.Connections(ctx)
	if err != nil {
		_ = c.CloseAllConnections(ctx)
		return
	}
	for _, x := range snap.Connections {
		if !dev.Keeps(x.RulePayload, x.Chains) {
			_ = c.CloseConnection(ctx, x.ID)
		}
	}
}

// DevNet is what 开发者网络 shows of this machine.
type DevNet struct {
	Env devnet.Env `json:"env"`
	// Warnings are for the user's eyes, in the interface's language.
	Warnings []string `json:"warnings,omitempty"`
	// Exports is a line for a shell, that points it at the proxy.
	Exports string `json:"exports"`
	// Unsets is the line that takes it off again.
	Unsets string `json:"unsets"`
}

// devReport is the page's data. ownTun: bowel's TUN is one of the tunnels
// holding the default route.
func (a *App) devReport(s settings.Settings, ownTun bool) DevNet {
	a.dev.mu.Lock()
	env, fellBack := a.dev.env, a.dev.fellBack
	a.dev.mu.Unlock()
	r := DevNet{Env: env, Exports: exportsFor(s, overlayOf(s, env)), Unsets: unsetLine(shellKind())}
	if s.DevOff {
		return r
	}
	if fellBack {
		r.Warnings = append(r.Warnings, i18n.T("内核没有接受探测到的网络，现在只用内置的场景", "The core refused the networks that were detected, so only the built-in scenarios are in use"))
	}
	if env.Tailscale.ExitNode != "" && s.Tun {
		r.Warnings = append(r.Warnings, fmt.Sprintf(i18n.T("Tailscale 正在用出口节点 %s，它和 TUN 都要接管默认路由，请关掉其中一个", "Tailscale is using the exit node %s. It and TUN both take the default route, so turn one of them off"), env.Tailscale.ExitNode))
	}
	foreign := slices.Clone(env.DefaultTunnels)
	if ownTun {
		foreign = slices.DeleteFunc(foreign, func(n string) bool { return n == s.TunDevice })
		if s.TunDevice == "" && len(foreign) > 0 {
			foreign = foreign[1:] // one of them is ours, whichever
		}
	}
	if len(foreign) > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(i18n.T("%s 上有别的代理接管了默认路由，流量会先经过它，这里的设置管不到", "Another proxy holds the default route on %s. Traffic reaches it first, out of reach of these settings"), strings.Join(foreign, ", ")))
	}
	return r
}

// exportsFor is the shell line for this proxy, with what stays off it.
func exportsFor(s settings.Settings, dev devnet.Overlay) string {
	noProxy := dev.NoProxy
	if len(noProxy) == 0 {
		noProxy = []string{"localhost", "127.0.0.1", "::1"}
	}
	return exportLine(shellKind(), s.MixedPort, noProxy)
}

// shellKind is the syntax of the user's shell: "fish", or "posix" for zsh,
// bash and the rest.
func shellKind() string {
	if filepath.Base(os.Getenv("SHELL")) == "fish" {
		return "fish"
	}
	return "posix"
}

// proxyVars are set in both cases: curl ignores HTTP_PROXY, and some tools
// read only the capitals. There is no all_proxy: https_proxy already covers
// what it would, and a socks5:// one makes tools without SOCKS support, as
// Python's httpx, fail when they start.
var proxyVars = []string{"http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY"}

// exportLine points a shell at the proxy on port.
func exportLine(kind string, port int, noProxy []string) string {
	addr, no := fmt.Sprintf("http://127.0.0.1:%d", port), strings.Join(noProxy, ",")
	var parts []string
	if kind == "fish" {
		for _, v := range proxyVars {
			parts = append(parts, fmt.Sprintf("set -gx %s %s", v, addr))
		}
		return strings.Join(append(parts, fmt.Sprintf(`set -gx no_proxy "%s"`, no), fmt.Sprintf(`set -gx NO_PROXY "%s"`, no)), "; ")
	}
	for _, v := range proxyVars {
		parts = append(parts, v+"="+addr)
	}
	return "export " + strings.Join(parts, " ") + fmt.Sprintf(` no_proxy="%[1]s" NO_PROXY="%[1]s"`, no)
}

// unsetLine takes a shell off the proxy. all_proxy too, which earlier
// versions of exportLine set.
func unsetLine(kind string) string {
	vars := append(slices.Clone(proxyVars), "all_proxy", "ALL_PROXY", "no_proxy", "NO_PROXY")
	if kind == "fish" {
		parts := make([]string, len(vars))
		for i, v := range vars {
			parts[i] = "set -e " + v
		}
		return strings.Join(parts, "; ")
	}
	return "unset " + strings.Join(vars, " ")
}
