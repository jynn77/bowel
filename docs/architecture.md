# How bowel works

The product logic is in [product-ia.md](product-ia.md); the approved interaction prototype is [prototype/index.html](../prototype/index.html). How to build and run is in the [README](../README.md#develop).

## Design

- **Providers supply nodes.** A subscription URL, pasted node links (`vmess://`, `trojan://`, `hysteria2://`, …) or a YAML file each become a mihomo `type: file` proxy-provider. bowel downloads subscriptions itself, to read their usage and expiry. A hidden per-provider suffix keeps node names unique across providers.
- **Routes come from bowel's template** (`internal/routes`): 默认, AI 服务, 流媒体, 开发 and 直连, and the rules that recognise each. Every node can serve every route. Each proxied route has two groups: a selector, and an automatic url-test group the selector can point at ("自动" = fastest + failover).
- **Your own rules come first** (`internal/routes/user.go`, `ui_rules.go`). A rule sends a domain and its subdomains, one name, a keyword or an IP range to a route; they are kept in settings.json and written before the template's. The quickest way to make one is 让它走… on a connection in 日志, which offers the site (api.openai.com → openai.com) or the one name. 设置 > 规则 adds them by hand, says what a typed rule will do before it is added, moves and removes them, and tests an address. A change closes the open connections it would send elsewhere, so it shows at once.
- **Rule sets** (`internal/ruleset`, `app_rulesets.go`, `ui_rulesets.go`). A list of rules kept by someone else, from a link or a local file (mihomo YAML, or `.list` text), is a service of a route group, so it moves, switches off and goes like any other. bowel fetches and checks it itself: 导入 shows the rule count before anything counts, lines the core can't use are left out and counted, and the core is given a clean `type: file` rule-provider. That also lets 测试一个地址 look inside a set, which the core has no call for. Links update daily; a failed update keeps the last good list and says so on its row. An update replaces the file and asks the core to read it again (`PUT /providers/rules/…`), so it closes nothing. The order is fixed: your rules, then rule sets, then the template's. Developer networks come before all of them, so a remote list can't take the LAN or the tailnet. With no rule set in use, your rules and the template's stay sorted together by how specific they are. Rule sets are always `no-resolve`, as your own IP rules are, so a set can't add a DNS lookup to every connection. `IP-ASN` rules need the core's ASN database, which it downloads itself on first use. Desktop only for now: the mobile apps don't import sets, and agents can only read them (`list_rules`, `explain`).
- **A provider's own routing** (`internal/core/imported.go`, `app_imported.go`, `ui_imported.go`). A complete Clash config often brings its own `proxy-groups` and `rules`. These stay unused until the user picks 使用它们 on that provider's row, because most subscriptions bring groups and an update must not change anyone's routing by itself. One provider at a time; `settings.ImportedRouting` names it. While it is in use bowel gives the core that config's nodes, groups, rules, sub-rules and the rule and proxy providers they use, unchanged, and nothing else from it: ports, DNS, TUN, listeners, the controller and scripts are bowel's or ignored. The nodes go at the top level under their own names. A group's `proxies:` can't name a proxy-provider's nodes, so the suffixed copies could not be referenced; unchanged names also mean no reference needs rewriting and a `filter` still means what it meant. The provider's `p-<id>` copy stays for the node list, tests and the location probe, and `listedNode` maps one name to the other. `include-all*` is expanded to the source's own members, so a group doesn't take in bowel's providers or the developer networks' outbounds. Remote providers are the core's to download, with the source's `path` removed; a used `type: file` is refused, as a path means nothing on this machine. Groups that rules point at become routes, so 今日, the map, 活动 and the menu bar work unchanged and MATCH's target is the default route; 路由组 lists every group, and only `select` groups can be switched. The user's rules and rule sets pause and come back with bowel's routing; developer networks still come first. 测试一个地址 and MCP's `explain` don't work here, since the core has no call for it and bowel can't evaluate rules it didn't write. An update of the provider in use is checked first, by reference and with `mihomo -t`: if it fails, or no longer has groups, the last good file stays and the error shows. It never falls back to the template. Disabling the provider goes to bowel's routing until it is enabled again; deleting it ends the choice. Desktop only for now.
- **The core runs as a sidecar process** (`internal/core`), controlled over a unix socket. bowel falls back to TCP with a secret when the socket path is too long. Config changes reload the core without restarting it.
- **The interface is native.** The window and the menu bar panel are MyGo `ui` views over two values: a `Snapshot`, rebuilt whenever anything changes, and a `Tick`, every second. The 今日 board animates its cells' boxes: the expanded cell grows in place and the others are squeezed into summaries chosen to fit. Every view is laid out at its cell's final size, so nothing reflows on the way. Views open and fold like drawers, and shared pieces fly between views (`Match`, with text morphing).
- **Status is measured, not assumed** (§2 of the IA). bowel checks every 2 s that the system proxy still points at it (`scutil --proxy`). The core's state, failing nodes and automatic switches feed the one light: 正常 / 降级 / 异常 / 未接管.
- **Exit locations** (`internal/locate`, `internal/geo`). A node's place is guessed from its name. When it matters (the node a route uses, or one you pick), bowel measures it instead: it requests Cloudflare's `/cdn-cgi/trace` through the node, via a mihomo listener bound to a probe group. The UI labels each place 实测 / 按名称 / 未知.
- **Monitoring** (`internal/monitor`). bowel polls connections every second while the window or the menu bar panel is open (every 5 s otherwise) to get rates per route and per app, new connections, and closed ones. It parses failed dials from the core's log, and keeps today's and this month's totals. On macOS 27 the kernel hides its socket list from a process whose parent is an ad-hoc signed app, so the core, bowel's child, cannot tell which app opened a connection. bowel reads the list itself (`internal/procinfo`) and names those connections. Under `mygo dev` bowel is itself such a child, so the names show as 未知 in development; an app opened from Finder has them.
- **TUN.** The first time you turn it on, macOS asks for an administrator's authorisation. bowel then copies the core to `~/Library/Application Support/bowel/bin/` and makes it setuid root (`internal/tun`), the approach early Clash Verge used. A launchd helper would be the cleaner long-term design. The copy does not follow an update of the app: when its version differs from the bundled core's, bowel runs the bundled core if TUN is off, and otherwise keeps the old copy and offers to replace it, which takes one authorization. The new copy replaces the old one only after that authorization.
- **Gateway mode** (`internal/gateway`, `app_gateway.go`, `ui_gateway.go`). One switch lets other devices on the LAN set their gateway and DNS to this Mac and follow bowel's rules. bowel turns IPv4 forwarding on through a sudoers rule that allows exactly the two `sysctl` commands (checked with `visudo`, installed in the same prompt as TUN), keeps the Mac awake, and puts DNS on `0.0.0.0:53` with fake-ip. The original forwarding value is kept in `gateway-before.json`, so a crash is undone on the next start. Forwarding, port 53 and TUN are measured; a gateway that is wanted but doesn't work makes the state 降级.
- **Skins** (`skins.go`). Six looks for the whole interface: 纸墨 Paper (the default), 报刊 Broadsheet, 荧光屏 Phosphor, 霓虹 Neon, 蓝图 Blueprint and 樱 Sakura. A skin is a palette, the faces of text and big type, corner roundness, a glow and a texture; what the interface does is the same in every skin.
- **Developer networks** (`internal/devnet`, `app_devnet.go`, `ui_devnet.go`; cases and reasons in `docs/dev-network.md`). A catalog of what is local everywhere, plus what bowel reads of this Mac every 20 seconds (Tailscale, VPN routes, split DNS), compile into an overlay on the core's config and the system proxy's bypass list. It is never saved, so no migration and nothing left behind. Tailscale and VPNs get a direct outbound bound to their interface, because under TUN the core's DIRECT uses the default interface. In global and direct mode the core runs in rule mode with the overlay and a single `MATCH`, so the LAN and the tailnet are not sent to a node.
- **Updating itself** (`app_update.go`, `ui_update.go`). A native binary cannot swap its code while it runs, so an update is `mygo.Updater` (signed archive or delta) followed by a restart. bowel checks the latest release 20 s after launch and every 6 hours and shows the new version in the title bar. One click downloads with progress and restarts through the ordinary quit: the system proxy and gateway go back and the core ends, so if the new version fails to come up the Mac keeps its own network. The network drops for a few seconds. Installs the app cannot write to, such as a .deb in /opt, are left to their package manager.
- **Agents (MCP)** (`internal/mcp`, `app_mcp.go`, `ui_mcp.go`). bowel serves an MCP server on a unix socket in its data folder (`mcp.sock`, this user only). `bowel mcp` bridges an agent's stdio to it and opens the app when it isn't running, so an agent registers it as an ordinary stdio server: `claude mcp add --scope user bowel -- /Applications/bowel.app/Contents/MacOS/bowel mcp` (设置 > MCP has the command and a JSON snippet to copy). Agents can always read: status, settings (secrets hidden), routes and nodes, rules, where a host would go, connections, logs, and the three whitelists (direct sites, the system proxy's bypass list, LAN addresses). Changes (mode, takeover, a route's node, rules, whitelists, settings by key, providers, DNS) follow `mcpPolicy`: `ask` (the default: a dialog shows what would change, and can let the rest of that connection through), `allow` or `deny`. Only the user can change the policy. 设置 > MCP lists the changes agents asked for.

## Layout

```
main.go, app.go           wiring: lifecycle, actions, the snapshot and tick
model.go                   the view model: Snapshot, Tick, Conn, …
ui.go                      the window: palette, title bar, pages, shared parts
ui_home.go, ui_map.go      代理: the morphing board and the exit map
ui_activity.go             活动: connections and logs
ui_providers.go            Provider: add, list, nodes
ui_rules.go                the user's rules: 日志's 让它走…, 设置 > 规则
ui_rulesets.go, app_rulesets.go  imported rule sets: the panel, the row, updates
ui_imported.go, app_imported.go  a provider's own routing: the row's offer, the page, what the app sees of it
ui_settings.go             设置: the core's options, in a window of their own (sidebar + pane, searchable)
ui_gateway.go, app_gateway.go  gateway mode: the switch, the guide card, measured checks
ui_providers_qr.go, app_qr.go  QR codes: show a provider's, scan with the camera or the screen
skins.go                   the six skins
ui_panel.go, tray.go       the menu bar icon, its panel and its menu
logo.go                    the logo's squiggle, which the menu bar icon is drawn from
debug.go                   a script hook for development builds (MOHO_DEBUG_DIR)
internal/core              mihomo process, runtime config, the core's options (Options), a provider's own routing (Imported)
internal/devnet            what stays off the proxy: the scenarios, this machine's networks, the overlay on the config
internal/mihomo            controller client
internal/provider          providers: detect, download, store
internal/routes            the routing template, and the user's rules
internal/ruleset           rule lists from a link or a file: read, check, store, look inside
internal/monitor           connections, rates, events, logs, totals
internal/procinfo          which app owns a local socket (macOS)
internal/locate, geo       exit locations
internal/sysproxy          set and check the macOS system proxy
internal/tun               setuid install for TUN
internal/gateway           IPv4 forwarding, LAN detection, the gateway's checks
internal/qr                QR decoding
internal/i18n              简体中文, 繁體中文, English, 日本語
internal/settings          settings.json
internal/mcp               a small MCP server (stdio framing over a unix socket) and the `bowel mcp` bridge
app_mcp.go, ui_mcp.go      the agents' tools and their policy; 设置 > MCP
prototype/                 the interaction prototype and its history
site/                      the website; site/shots/ is rendered by scripts/site-shots.sh
assets/icon/               the app icon: AppIcon.icon (Icon Composer) and its 1024 px render; iOS compiles it too
resources/icon.png         the fallback icon, for macOS before 26 and `mygo dev`
scripts/mobile-brand.sh    writes the Android vectors and Flutter's logo path from the above
entitlements.plist         the camera, which a Developer ID build's hardened runtime needs
```

mihomo is GPL-3.0; bowel runs it as a separate program and does not link it.
