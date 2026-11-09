<p align="center">
  <img src="site/icon.png" width="112" alt="bowel">
</p>

<h1 align="center">bowel</h1>

<p align="center">
bowel is a <a href="https://github.com/MetaCubeX/mihomo">mihomo</a> client for macOS.
</p>

<p align="center">
  <a href="https://github.com/21stware/bowel/releases/latest"><b>Download for macOS</b></a> ·
  <a href="https://21stware.github.io/bowel/">Website</a> ·
  <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="site/shots/home-en-dark.webp">
    <img src="site/shots/home-en.webp" width="880" alt="bowel main window">
  </picture>
</p>

The interface is built in Go and rendered directly via the GPU, with no WebView.

> **This fork also builds for Windows.** The installer and everything else that
> changed on the way there — TUN included — is under
> [Windows](#windows-this-fork).

## Features

### Routes
Route traffic by purpose, and select exit nodes manually or automatically by speed.

<img src="site/shots/routes-en.webp" width="880" alt="Routes">

### Connections
Inspect the app, destination, rule, exit, and speed for each connection, or reroute domains on the fly.

<img src="site/shots/activity-en.webp" width="880" alt="Connections log">

### Providers
Import subscription links, node URLs, YAML configs, or QR codes, with data usage and expiry shown for each source.

<img src="site/shots/providers-en.webp" width="880" alt="Providers">

### Menu bar
Toggle system proxy takeover, switch proxy modes, and select default nodes from the menu bar.

<img src="site/shots/panel-en.webp" width="300" alt="Menu bar panel">

### Skins
Six skins that change the look of the whole interface.

<table>
  <tr>
    <td><img src="site/shots/skin-broadsheet-en.webp" alt="Broadsheet"><br><sub>Broadsheet</sub></td>
    <td><img src="site/shots/skin-phosphor-en.webp" alt="Phosphor"><br><sub>Phosphor</sub></td>
    <td><img src="site/shots/skin-neon-en.webp" alt="Neon"><br><sub>Neon</sub></td>
  </tr>
  <tr>
    <td><img src="site/shots/skin-blueprint-en.webp" alt="Blueprint"><br><sub>Blueprint</sub></td>
    <td><img src="site/shots/skin-sakura-en.webp" alt="Sakura"><br><sub>Sakura</sub></td>
    <td><img src="site/shots/skin-paper-en.webp" alt="Paper"><br><sub>Paper (default)</sub></td>
  </tr>
</table>

### Other
- **Custom rules**: Route specific domains, keywords, or IP ranges, and test where addresses go.
- **Measured exit locations**: Verify true exit locations through Cloudflare trace.
- **Gateway mode**: Turn this Mac into a LAN gateway for devices like TVs and consoles.
- **TUN**: Intercept traffic from applications that ignore system proxy settings.
- **MCP**: Built-in MCP server for Claude Code and other agents to read status and troubleshoot connections. It asks you before changing settings by default.
  ```bash
  claude mcp add --scope user bowel -- /Applications/bowel.app/Contents/MacOS/bowel mcp
  ```
- **Settings and languages**: Searchable settings (⌘F); English, Simplified Chinese, Traditional Chinese and Japanese; light and dark.

## Install

1. Download `bowel-<version>-arm64.dmg` from [Releases](https://github.com/21stware/bowel/releases/latest) and drag bowel into Applications.
2. Add a provider (a subscription URL, node links or a QR code) and turn on takeover.

Needs macOS 12 or later. Released builds are for Apple silicon, signed with a Developer ID and notarized by Apple.

## Windows (this fork)

There is no upstream Windows build; this tree makes one.

```bash
GOOS=windows GOARCH=amd64 ./scripts/fetch-core.sh   # mihomo.exe into resources/windows-amd64/bin
# put wintun.dll (0.14.1 amd64, wintun.net) beside it — TUN needs it
go tool mygo build -platform windows/amd64 -o build .
# → build/windows-amd64/bowel-setup-<version>-amd64.exe   (NSIS installer)
# → build/windows-amd64/bowel.exe                          (portable)
```

Needs Windows 10 or later, amd64. The installer puts bowel in `%LOCALAPPDATA%`
or the directory you pick, and runs the shortcut as administrator so TUN can
open a device.

### What the port needed

- **Delay test**: `ping.exe` runs with `CREATE_NO_WINDOW` and its delay is read
  out of the output whatever language the line is in. A console per node used
  to flash onto the screen for every test round.
- **TUN**: the core's own copy and `wintun.dll` are staged in
  `%APPDATA%\bowel\bin`, and a device is opened only when bowel itself is
  elevated. Without elevation the switch explains why rather than failing.
- **TUN, on the way out**: the core is asked to drop the tunnel *before* it is
  stopped, and the routes a killed core leaves behind are taken back on the
  next start. Windows has no SIGTERM, so stopping the core used to be
  `Process.Kill()`: the adapter and a default route through `198.18.0.2` (metric
  0, ahead of the LAN's 35) stayed behind, and every packet went into a tunnel
  with no session — which reads as "turning TUN off breaks the network".
- **A resolver, TUN or no TUN**: the core always gets a `dns:` section of its
  own. It needs one for the nodes' names, not only for traffic: a node whose
  server is a domain cannot be dialled without it, and VLESS' ECH asks DNS for
  its config before the TLS handshake starts. With TUN off and the built-in DNS
  switch off, exactly those nodes failed to test while every other provider was
  fine.
- **Kernel logs**: the Windows build hides the console it runs the core in, and
  the log page reads the core's own stream instead.

### What else is here on top of 0.2.0

- **Exit address quality**: the manifest asks one address service the settings
  name (ip-api.com by default; ipwho.is, ip.sb and ipapi.co also work) whether
  the exit is a data centre or a home line and whether it is listed as a proxy,
  then grades it — excellent, good, fair, poor — together with the node's
  delay. Off by default, since it sends the exit address outside.
- **More presets**: the test URL, the exit lookup URL and the quality lookup
  URL each have a picker of common addresses beside the field, and any other
  address can still be typed.
- **Provider addresses**: a column showing `host:port` exactly as the
  subscription has it, read from the file (Clash YAML, link lists, or a whole
  base64 blob) and cached until the file changes.
- **Clearing the lists**: the eraser above the tabs empties what the page
  shows. What has ended goes on the first click; live connections and "all"
  ask for a second one, and neither disconnects anything.
- **Your own place**: it is re-measured once the tunnel has settled and every
  ten minutes after that, so switching TUN on no longer leaves the map showing
  the node's country instead of yours.
- **Region names**: zero-width characters are dropped before matching, so a
  node named `H​K` or `日​本` is still recognised.

## Develop

Needs Go 1.27.3+ (`GOTOOLCHAIN=auto` fetches it). No Node or Bun.

```bash
./scripts/fetch-core.sh     # mihomo + GeoSite/GeoIP data into resources/
go tool mygo dev            # the app, rebuilt on every change
go tool mygo build          # bowel.app and a .dmg, signed but not notarized
./scripts/release.sh        # build, notarize and staple: use this for releases
CGO_ENABLED=0 go test ./... # includes tests that start the real core
./scripts/site-shots.sh     # re-render the README's and the website's screenshots (needs cwebp)
```

- How it works and where the code is: [docs/architecture.md](docs/architecture.md)
- The product's information architecture: [docs/product-ia.md](docs/product-ia.md)
- The website is static, in `site/`; open `site/index.html` to preview it. Changes to it go live when they reach `main`.
- To release, set the version in `mygo.json` and push a matching tag, such as `v0.1.0`. GitHub Actions builds, signs and notarizes the disk image and attaches it to a new release.

bowel was called Moho. The Go module is still `github.com/oboo/moho` and the bundle identifier `dev.moho.app`, which keeps macOS's camera and screen-recording permissions. On first launch the data in `~/Library/Application Support/Moho` moves to `…/bowel`.

## Credits

- [mihomo](https://github.com/MetaCubeX/mihomo): the proxy core, GPL-3.0. bowel runs it as a separate program and does not link it.
- [MyGo](https://github.com/egoist/mygo): the native Go UI toolkit.
- [OPPO Sans](resources/fonts/): the interface's typeface.

## License

bowel is licensed under the [PolyForm Noncommercial License 1.0.0](LICENSE), which allows noncommercial use only. For a commercial license, [open an issue](https://github.com/21stware/bowel/issues).

The bundled mihomo core stays under GPL-3.0. The app carries its license in `Contents/Resources/licenses/mihomo/`, and each release attaches the matching source. The flag images in `assets/flags/` and the fonts in `resources/fonts/` are third-party files and keep their own licenses.
