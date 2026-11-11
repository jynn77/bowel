package main

// The command line's side in the app. `bowel status`, `bowel mode global`
// and the rest (internal/cli) are a thin client of the running bowel, the
// app or `bowel daemon`: they call the same tools agents do (app_mcp.go's
// mcpServer), and a few only the command line has, over a second socket
// in the data folder, cli.sock, in the protocol of the first.
//
// Who is asking is what the two sockets tell apart. On mcp.sock it is an
// agent, and a change follows the user's policy. On cli.sock it is the
// user, at a shell, and nothing is asked: unless the command says an
// agent ran it (internal/cli sees the agent's environment), and then the
// policy holds as on mcp.sock. That is a guard rail, not a wall: a program
// of this user that wants round it can also write settings.json.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/oboo/moho/internal/backup"
	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/mcp"
	"github.com/oboo/moho/internal/provider"
	"github.com/oboo/moho/internal/routes"
)

// cliSocket is where the app serves the command line, beside mcp.sock.
func cliSocket(dataDir string) string {
	p := filepath.Join(dataDir, "cli.sock")
	if len(p) < 100 {
		return p
	}
	// Told apart by the folder, so that two long ones don't share a socket.
	sum := sha256.Sum256([]byte(dataDir))
	return filepath.Join(os.TempDir(), fmt.Sprintf("bowel-%d-%x-cli.sock", os.Getuid(), sum[:4]))
}

// isLocal says the session is the user's own command, not an agent's.
func isLocal(s *mcp.Session) bool { return s != nil && s.Get("local") == true }

// cliServer is mcpServer with the command line's own tools, for sessions
// that are the user's unless they say an agent runs them.
func (a *App) cliServer() *mcp.Server {
	srv := a.mcpServer()
	srv.Name, srv.Title, srv.Instructions = "bowel-cli", "bowel CLI", ""
	for i, t := range srv.Tools {
		if t.Name == "list_connections" || t.Name == "get_logs" {
			run := t.Run
			srv.Tools[i].Run = func(ctx context.Context, s *mcp.Session, raw json.RawMessage) (any, error) {
				a.watched()
				return run(ctx, s, raw)
			}
		}
	}
	srv.Tools = append(srv.Tools, a.cliTools()...)
	srv.OnInit = func(s *mcp.Session, params json.RawMessage) {
		var p struct {
			Agent string `json:"agent"`
		}
		_ = json.Unmarshal(params, &p)
		if p.Agent == "" {
			s.Set("local", true)
		}
	}
	return srv
}

// watched notes that a command reads the connections or the logs now, so
// that they are polled every second while it does (pollEvery).
func (a *App) watched() {
	a.watchedAt.Store(time.Now().Unix())
	a.mon.Wake()
}

// findProvider is the provider s names: its id, or its name when one has it.
func (a *App) findProvider(s string) (provider.Provider, error) {
	if p, ok := a.providers.Get(s); ok {
		return p, nil
	}
	var hits []provider.Provider
	for _, p := range a.providers.List() {
		if strings.EqualFold(p.Name, s) {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return provider.Provider{}, fmt.Errorf(i18n.T("没有这个 Provider: %s", "No such Provider: %s"), s)
	}
	return provider.Provider{}, fmt.Errorf(i18n.T("有多个 Provider 叫 %s，请用它的 id", "Several Providers are called %s; use the id"), s)
}

// cliTools are the tools only the command line has. Their changes go
// through change, as the agents' do.
func (a *App) cliTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "provider_add", Title: "Add a provider",
			Description: "Adds a provider: a subscription URL or a node link (source), or a file on this machine (file, an absolute path).",
			Input:       obj(map[string]any{"source": str("A subscription URL or a node link."), "file": str("An absolute path.")}),
			Run: func(ctx context.Context, s *mcp.Session, raw json.RawMessage) (any, error) {
				var p struct{ Source, File string }
				if err := mcp.Args(raw, &p); err != nil {
					return nil, err
				}
				if (p.Source == "") == (p.File == "") {
					return nil, errors.New("give source or file")
				}
				var v ProviderView
				err := a.change(ctx, s, words{"添加 Provider", "Add Provider"}, p.Source+p.File, func() error {
					done := a.begin("add")
					defer done()
					var pv provider.Provider
					var err error
					if p.File != "" {
						pv, err = a.providers.AddFile(p.File)
					} else {
						pv, err = a.providers.Add(ctx, a.http, p.Source)
					}
					if err != nil {
						return err
					}
					v, err = a.afterProviderAdded(pv)
					return err
				})
				if err != nil {
					return nil, err
				}
				return map[string]any{"provider": v}, nil
			},
		},
		{
			Name: "provider_remove", Title: "Remove a provider", Destructive: true,
			Description: "Removes a provider, by id or name, and its nodes with it.",
			Input:       obj(map[string]any{"id": str("A provider's id or name.")}, "id"),
			Run: func(ctx context.Context, s *mcp.Session, raw json.RawMessage) (any, error) {
				var p struct{ ID string }
				if err := mcp.Args(raw, &p); err != nil {
					return nil, err
				}
				pv, err := a.findProvider(p.ID)
				if err != nil {
					return nil, err
				}
				err = a.change(ctx, s, words{"删除 Provider", "Remove Provider"}, pv.Name, func() error { return a.deleteProvider(pv.ID) })
				if err != nil {
					return nil, err
				}
				return map[string]any{"removed": a.providerView(pv)}, nil
			},
		},
		{
			Name: "provider_enable", Title: "Turn a provider on or off",
			Description: "Turns a provider on or off, by id or name: off, its nodes are not used.",
			Input:       obj(map[string]any{"id": str("A provider's id or name."), "enabled": map[string]any{"type": "boolean"}}, "id", "enabled"),
			Run: func(ctx context.Context, s *mcp.Session, raw json.RawMessage) (any, error) {
				var p struct {
					ID      string
					Enabled bool
				}
				if err := mcp.Args(raw, &p); err != nil {
					return nil, err
				}
				pv, err := a.findProvider(p.ID)
				if err != nil {
					return nil, err
				}
				what := words{"停用 Provider", "Turn a Provider off"}
				if p.Enabled {
					what = words{"启用 Provider", "Turn a Provider on"}
				}
				if err := a.change(ctx, s, what, pv.Name, func() error { return a.setProviderEnabled(pv.ID, p.Enabled) }); err != nil {
					return nil, err
				}
				return map[string]any{"providers": a.snapshot().Providers}, nil
			},
		},
		{
			Name: "list_nodes", Title: "Nodes on a route",
			Description: "The nodes with their delays on a route's test target, the node the route uses, and the providers' names. With test, the delays are measured first.",
			Input:       obj(map[string]any{"route": str("A route's id."), "test": map[string]any{"type": "boolean"}}),
			Run: func(ctx context.Context, s *mcp.Session, raw json.RawMessage) (any, error) {
				var p struct {
					Route string
					Test  bool
				}
				if err := mcp.Args(raw, &p); err != nil {
					return nil, err
				}
				if p.Route == "" {
					p.Route = routes.Default
				}
				r, err := a.findRoute(p.Route)
				if err != nil {
					return nil, err
				}
				out := map[string]any{"route": r.ID}
				if p.Test {
					if r.Direct {
						return nil, errors.New(i18n.T("这条路由不使用节点", "This route uses no node"))
					}
					if err := a.testRoute(ctx, r.ID); err != nil {
						out["error"] = err.Error()
					}
				}
				snap := a.snapshot()
				for _, rv := range snap.Routes {
					if rv.ID == r.ID {
						out["current"], out["auto"] = rv.Node, rv.Auto
					}
				}
				names := map[string]string{}
				for _, pv := range snap.Providers {
					names[pv.ID] = pv.Name
				}
				out["nodes"], out["providers"] = mcpNodes(snap, r.ID), names
				return out, nil
			},
		},
		{
			Name: "set_gateway", Title: "Set gateway mode", Destructive: true,
			Description: "Turns gateway mode on or off: this machine as the LAN's gateway. On turns TUN and the takeover on too.",
			Input:       obj(map[string]any{"enabled": map[string]any{"type": "boolean"}}, "enabled"),
			Run: func(ctx context.Context, s *mcp.Session, raw json.RawMessage) (any, error) {
				var p struct{ Enabled bool }
				if err := mcp.Args(raw, &p); err != nil {
					return nil, err
				}
				old := a.settings.Get().Gateway
				if old != p.Enabled {
					err := a.change(ctx, s, words{"更改网关模式", "Change gateway mode"}, fmt.Sprintf("gateway: %v → %v", old, p.Enabled),
						func() error { return a.setGateway(p.Enabled) })
					if err != nil {
						return nil, err
					}
				}
				return map[string]any{"changed": old != p.Enabled, "takeover": a.snapshot().Takeover}, nil
			},
		},
		{
			Name: "tail_logs", Title: "Follow the core's logs", ReadOnly: true,
			Description: "The core's log lines after the one numbered after, oldest first, and the number to ask with next. With after 0, the last limit lines.",
			Input: obj(map[string]any{
				"after": map[string]any{"type": "integer", "minimum": 0},
				"level": str("Only this level and worse.", "debug", "info", "warning", "error"),
				"limit": map[string]any{"type": "integer", "minimum": 0, "maximum": 1000},
			}),
			Run: func(ctx context.Context, s *mcp.Session, raw json.RawMessage) (any, error) {
				var p struct {
					After int64
					Level string
					Limit int
				}
				if err := mcp.Args(raw, &p); err != nil {
					return nil, err
				}
				a.watched()
				all := a.logs(p.After)
				last := p.After
				if n := len(all); n > 0 {
					last = all[n-1].Seq
				}
				rank := map[string]int{"debug": 0, "info": 1, "warning": 2, "error": 3}
				out := []LogLine{}
				for _, l := range all {
					if p.Level == "" || rank[l.Level] >= rank[p.Level] {
						out = append(out, l)
					}
				}
				if p.After == 0 && len(out) > p.Limit {
					out = out[len(out)-p.Limit:]
				}
				return map[string]any{"logs": out, "last": last}, nil
			},
		},
		{
			Name: "config_export", Title: "Export the configuration",
			Description: "Writes the settings, providers and rule sets to a file (path, absolute), sealed with password if one is given. Only for the user's own command.",
			Input:       obj(map[string]any{"path": str("An absolute path."), "password": str("Seals the file; empty for none.")}),
			Run: func(ctx context.Context, s *mcp.Session, raw json.RawMessage) (any, error) {
				var p struct{ Path, Password string }
				if err := mcp.Args(raw, &p); err != nil {
					return nil, err
				}
				if err := userOnly(s, p.Path); err != nil {
					return nil, err
				}
				if err := a.exportConfig(p.Path, p.Password); err != nil {
					return nil, err
				}
				return map[string]any{"path": p.Path, "encrypted": p.Password != "", "providers": len(a.providers.List())}, nil
			},
		},
		{
			Name: "config_import", Title: "Import a configuration", Destructive: true,
			Description: "Replaces the settings, providers and rule sets with those of an exported file (path, absolute), keeping this machine's own settings; or, with undo, puts back what the last import replaced. Only for the user's own command.",
			Input:       obj(map[string]any{"path": str("An absolute path."), "password": str("The file's password, if it has one."), "undo": map[string]any{"type": "boolean", "description": "Undo the last import instead."}}),
			Run: func(ctx context.Context, s *mcp.Session, raw json.RawMessage) (any, error) {
				var p struct {
					Path, Password string
					Undo           bool
				}
				if err := mcp.Args(raw, &p); err != nil {
					return nil, err
				}
				if p.Undo {
					p.Path = a.undoImportPath()
				}
				if err := userOnly(s, p.Path); err != nil {
					return nil, err
				}
				var err error
				if p.Undo {
					err = a.undoImport()
				} else {
					err = a.importConfig(p.Path, p.Password)
				}
				if backup.IsPassword(err) {
					// Said apart from other failures: the command asks and
					// tries again.
					return map[string]any{"imported": false, "needPassword": true, "message": err.Error()}, nil
				}
				if err != nil {
					return nil, err
				}
				return map[string]any{"imported": true, "undone": p.Undo, "providers": len(a.providers.List())}, nil
			},
		},
		{
			Name: "stop", Title: "Quit bowel", Destructive: true,
			Description: "Quits bowel: the system proxy and the gateway go back as they were, and the core ends.",
			Run: func(ctx context.Context, s *mcp.Session, _ json.RawMessage) (any, error) {
				err := a.change(ctx, s, words{"退出 bowel", "Quit bowel"}, "bowel", func() error {
					// After the answer has gone: quitting closes this socket.
					time.AfterFunc(200*time.Millisecond, a.quit)
					return nil
				})
				if err != nil {
					return nil, err
				}
				return map[string]any{"stopping": true, "headless": a.headless}, nil
			},
		},
	}
}

// userOnly refuses what only the user may do with a file: an export holds
// the subscription links, and an import replaces everything, so neither is
// an agent's to run, whatever the policy.
func userOnly(s *mcp.Session, path string) error {
	if !isLocal(s) {
		return errors.New(i18n.T("导出和导入配置只能由用户自己操作", "Only the user can export or import the configuration"))
	}
	if !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	return nil
}

// quit ends bowel as its menu's 退出 does, or ends a run without a window.
func (a *App) quit() {
	if !a.headless {
		mygo.App.Quit()
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stop != nil {
		select {
		case <-a.stop:
		default:
			close(a.stop)
		}
	}
}

var cliStop context.CancelFunc

// startCLI serves the command line for the app's life; stopCLI ends that.
func (a *App) startCLI() {
	ctx, cancel := context.WithCancel(context.Background())
	cliStop = cancel
	l, err := mcp.Listen(cliSocket(a.dataDir))
	if err != nil {
		log.Println("cli:", err)
		return
	}
	go a.cliServer().Accept(ctx, l)
}

func stopCLI() {
	if cliStop != nil {
		cliStop()
	}
}
