<p align="center">
  <img src="site/icon.png" width="112" alt="bowel">
</p>

<h1 align="center">bowel</h1>

<p align="center">
bowel is a <a href="https://github.com/MetaCubeX/mihomo">mihomo</a> client. Upstream ships macOS only;
<b>this fork adds Windows</b>, and a few things on top of it.
</p>

<p align="center">
  <a href="README.zh-CN.md">简体中文</a>
</p>

## What this fork changes

### Windows

There is no upstream Windows build; this is a port, not a wrapper.

- **Delay test**: `ping.exe` runs with `CREATE_NO_WINDOW` and its delay is read
  out of the output whatever language the line is in. A console per node used to
  flash onto the screen for every test round.
- **TUN**: the core's own copy and `wintun.dll` are staged in
  `%APPDATA%\bowel\bin`, and a device is opened only when bowel itself is
  elevated. Without elevation the switch explains why rather than failing.
- **TUN, on the way out**: the core is asked to drop the tunnel *before* it is
  stopped, and the routes a killed core leaves behind are taken back on the next
  start. Windows has no SIGTERM, so stopping the core used to be
  `Process.Kill()`: the adapter and a default route through `198.18.0.2` (metric
  0, ahead of the LAN's 35) stayed behind, and every packet went into a tunnel
  with no session — which reads as "turning TUN off breaks the network".
- **A resolver, TUN or no TUN**: the core always gets a `dns:` section of its
  own. It needs one for the nodes' names, not only for traffic: a node whose
  server is a domain cannot be dialled without it, and VLESS' ECH asks DNS for
  its config before the TLS handshake starts. With TUN off and the built-in DNS
  switch off, exactly those nodes failed to test while every other provider was
  fine.
- **System proxy**: `InternetSetOption` can be refused for the per-user LAN
  settings in an elevated process — which is how the shortcut runs, since TUN
  needs it. That write now falls back to the registry, where Windows' own
  settings page reads the same values, and the change is announced the usual
  way. Taking over also clears a PAC script the machine had: a script comes
  before the fixed proxies, and one left in place made the app report its own
  takeover as someone else's.
- **Core logs**: the Windows build hides the console it runs the core in, and
  the log page reads the core's own stream instead.
- **Showing files**: the 设置 > 关于 "Running config" button opened the
  Finder's `open -R`, which does not exist here. It selects the file in
  Explorer now, and says so. The MCP bridge's "open the app" starts the exe
  instead of failing on the missing `.app` bundle.

### Elsewhere

- **Exit address quality**: the app asks one address service the settings name
  (ip-api.com by default; ipwho.is, ip.sb and ipapi.co also work) whether the
  exit is a data centre or a home line and whether it is listed as a proxy, then
  grades it — excellent, good, fair, poor — together with the node's delay. Off
  by default, since it sends the exit address outside.
- **More presets**: the test URL, the exit lookup URL and the quality lookup URL
  each have a picker of common addresses beside the field, and any other address
  can still be typed.
- **Provider addresses**: a column showing `host:port` exactly as the
  subscription has it, read from the file (Clash YAML, link lists, or a whole
  base64 blob) and cached until the file changes.
- **Clearing the lists**: the eraser above the tabs empties what the page shows.
  What has ended goes on the first click; live connections and "all" ask for a
  second one, and neither disconnects anything.
- **Your own place**: it is re-measured once the tunnel has settled and every
  ten minutes after that, so switching TUN on no longer leaves the map showing
  the node's country instead of yours.
- **Region names**: zero-width characters are dropped before matching, so a node
  named `H​K` or `日​本` is still recognised.
- **Name rules**: what node names are shown without is yours to say — a list
  of rules, each dropping what it matches from every name the interface
  shows. Plain text matches itself, and a rule between two slashes is a
  regular expression (as in `/\[.*\]/`, which drops a `[Vless]` prefix); as
  many as you like. Rules match the name as shown: the zero-width characters
  the core's name is full of go before the rules read it, so what you see is
  what a rule can drop. The default drops the zero-width characters
  subscriptions split names with. The core keeps the names it was given, so
  choices, rules and the running config are unchanged.

## Install

1. Download `bowel-setup-<version>-amd64.exe` from
   [Releases](https://github.com/jynn77/bowel/releases) and run it.
2. Add a provider (a subscription URL, node links or a QR code) and turn on
   takeover.

Needs Windows 10 or later, amd64. The installer puts bowel where you pick, and
the shortcut runs as administrator so TUN can open a device. The portable build
is `bowel.exe` beside it.

## Build from source

Needs Go 1.27.3+ (`GOTOOLCHAIN=auto` fetches it). No Node or Bun.

```bash
GOOS=windows GOARCH=amd64 ./scripts/fetch-core.sh   # mihomo.exe into resources/windows-amd64/bin
# put wintun.dll (0.14.1 amd64, wintun.net) beside it — TUN needs it
go tool mygo build -platform windows/amd64 -o build .
# → build/windows-amd64/bowel-setup-<version>-amd64.exe   (NSIS installer)
# → build/windows-amd64/bowel.exe                          (portable)
```

- How it works and where the code is: [docs/architecture.md](docs/architecture.md)
- Settings are searchable with `Ctrl+F`; the interface comes in English,
  Simplified Chinese, Traditional Chinese and Japanese, light and dark.

## License

bowel is licensed under the [PolyForm Noncommercial License 1.0.0](LICENSE), which allows noncommercial use only. For a commercial license, [open an issue](https://github.com/21stware/bowel/issues).

The bundled mihomo core stays under GPL-3.0. The app carries its license in `Contents/Resources/licenses/mihomo/`, and each release attaches the matching source. The flag images in `assets/flags/` and the fonts in `resources/fonts/` are third-party files and keep their own licenses.
