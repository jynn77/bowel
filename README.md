<p align="center">
  <img src="site/icon.png" width="112" alt="bowel">
</p>

<h1 align="center">bowel</h1>

<p align="center">
bowel is a <a href="https://github.com/MetaCubeX/mihomo">mihomo</a> client for macOS, Windows, Linux and Android.
</p>

<p align="center">
  <a href="https://github.com/21stware/bowel/releases/latest"><b>Download</b></a> ·
  <a href="https://bowel.dev/en/">Website</a> ·
  <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="site/shots/home-en-dark.webp">
    <img src="site/shots/home-en.webp" width="880" alt="bowel main window">
  </picture>
</p>

The desktop interface is built in Go and rendered directly via the GPU, with no WebView. The Android app is built with Flutter and has the same cards and skins.

## What this fork changes

A fork of [21stware/bowel](https://github.com/21stware/bowel). It follows upstream, and keeps a few things of its own on top:

- **Name rules**: node names are shown without what a list of rules matches — plain text as itself, a rule between two slashes as a regular expression, as many rules as you like. Rules are read against the name as shown, so the zero-width characters subscriptions hide in names do not stand in the way of one. The core keeps the names it was given: choices, rules and the running config are unchanged.
- **The switch note**: a route moving off a failed node says `[Provider]Node is down, switched to [Provider]Node`, both names through the same rules, and goes after eight seconds — or as soon as the route is pinned, moves again, the failed node answers, or traffic stops being taken over.
- **System proxy on Windows**: `InternetSetOption` can be refused for the per-user LAN settings in an elevated process, which is how the shortcut runs, since TUN needs it. That write falls back to the registry, where Windows' own settings page reads the same values, and the change is announced the usual way. Taking over also clears a PAC script the machine had: a script comes before the fixed proxies, and one left in place made the app report its own takeover as someone else's.
- **Open at login on Windows**: the framework's login item goes through the Run key, whose entries start unelevated — dead for a tunnel. It is a scheduled task of the highest run level here, started with the administrator's token at logon and without a prompt; the uninstaller takes the task away.
- **Showing files**: 设置 > 关于's "Running config" selects the file in Explorer instead of calling the Finder's `open -R`, and says so.
- **Delay test on Windows**: an echo goes through IP Helper (`IcmpSendEcho`), which starts no process at all — a console used to flash for every node in a test round, even with `CREATE_NO_WINDOW`. `ping.exe` stays as the fallback, and as the way to leave by a given adapter (`-S`).
- **Provider addresses**: every provider's node list shows the server each node dials, read once per provider file rather than once per snapshot.
- **Exit lookup**: the exit probe takes a preset — Cloudflare, ip.sb, ipwho.is, ip-api or ipinfo.io — or any http(s) address that answers with an address and a country.
- **Connections**: the clear button sits by the app filter, and asks for a second click before it clears.

Upstream has the native TUN on Windows since 0.3: an elevated copy of the core under `%ProgramData%`, started by a scheduled task, and `nowindow` for every console child.

## Features

The screenshots are from macOS.

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
Toggle system proxy takeover, switch proxy modes, and pick the node for traffic no rule matches from the menu bar, or from the system tray on Windows and Linux.

<img src="site/shots/panel-en.webp" width="300" alt="Menu bar panel">

### Skins
Thirteen skins that change the look of the whole interface.

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
  <tr>
    <td><img src="site/shots/skin-shanshui-en.webp" alt="Shanshui"><br><sub>Shanshui</sub></td>
    <td><img src="site/shots/skin-celadon-en.webp" alt="Celadon"><br><sub>Celadon</sub></td>
    <td><img src="site/shots/skin-dunhuang-en.webp" alt="Dunhuang"><br><sub>Dunhuang</sub></td>
  </tr>
  <tr>
    <td><img src="site/shots/skin-dusk-en.webp" alt="Dusk"><br><sub>Dusk</sub></td>
    <td><img src="site/shots/skin-aurora-en.webp" alt="Aurora"><br><sub>Aurora</sub></td>
    <td><img src="site/shots/skin-obsidian-en.webp" alt="Obsidian"><br><sub>Obsidian</sub></td>
  </tr>
  <tr>
    <td><img src="site/shots/skin-riso-en.webp" alt="Riso"><br><sub>Riso</sub></td>
  </tr>
</table>

### Other
- **Custom rules**: Route specific domains, keywords, or IP ranges, and test where addresses go.
- **Measured exit locations**: Verify true exit locations through Cloudflare trace.
- **Gateway mode** (macOS, Linux): Turn this computer into a LAN gateway for devices like TVs and consoles.
- **TUN** (macOS, Windows, Linux): Intercept traffic from applications that ignore system proxy settings.
- **Developer tools** (macOS, Linux): Docker, the terminal and Git ignore the system proxy. bowel lists which ones aren't connected, writes their own config in one click, and removes it cleanly when you undo.
- **Download sources**: Measures the official source and each mirror for npm, pip, Go and Cargo, and switches in one click. When the current source stops answering, one click moves you to the fastest one that does, or sends it through a node again.
- **Private networks**: Addresses on your LAN, Tailscale and company VPN go direct automatically, in global mode too.
- **MCP**: Built-in MCP server for Claude Code and other agents to read status and troubleshoot connections. It asks you before changing settings by default. The command below is for macOS; on Windows and Linux, use the path of the bowel program.
  ```bash
  claude mcp add --scope user bowel -- /Applications/bowel.app/Contents/MacOS/bowel mcp
  ```
- **Settings and languages**: Searchable settings (⌘F); English, Simplified Chinese, Traditional Chinese and Japanese; light and dark.

## Install

macOS, with [Homebrew](https://brew.sh):

```bash
brew install --cask 21stware/tap/bowel
```

Windows, in PowerShell:

```powershell
irm https://github.com/21stware/bowel/releases/latest/download/install.ps1 | iex
```

Debian, Ubuntu and Fedora. This adds a signed package repository, so `apt` or `dnf` updates bowel from then on:

```bash
curl -fsSL https://21stware.github.io/packages/install.sh | sh
```

Or download the file for your platform from [Releases](https://github.com/21stware/bowel/releases/latest):

| Platform | File | Needs |
| --- | --- | --- |
| macOS | `bowel-<version>-arm64.dmg` | macOS 12 or later, Apple silicon |
| Windows | `bowel-setup-<version>-amd64.exe` or `-arm64.exe` | x64 or Arm64 |
| Linux | `bowel_<version>_<arch>.deb`, `bowel-<version>-1.<rpm arch>.rpm` or `bowel-<version>-linux-<arch>.tar.gz` | amd64 or arm64 |
| Android | `bowel-<version>-android-arm64.apk` | Android 8.0 or later, arm64 |

- **macOS**: Drag bowel into Applications. The app is signed with a Developer ID and notarized by Apple.
- **Windows**: Run the installer. It isn't code-signed, so Windows may warn you before it runs.
- **Linux**: Install the `.deb` or `.rpm`. For a user install in `~/.local`, download `install.sh` and the `.tar.gz` from the same release, then run `sh install.sh bowel-<version>-linux-<arch>.tar.gz` without unpacking the archive. This is separate from the package-repository script above.
- **Android**: Install the APK, allowing your browser to install apps if asked. Android asks for VPN permission the first time you connect.
- **iOS**: Not released yet.

Then add a provider (a subscription URL, node links or a QR code) and turn on takeover.

Some features depend on the platform:

| | macOS | Windows | Linux | Android |
| --- | --- | --- | --- | --- |
| Takeover | System proxy or TUN | System proxy or TUN | System proxy (GNOME, KDE) or TUN | System VPN |
| Gateway mode | Yes | No | Yes | No |
| Developer tools | Yes | No | Yes (zsh, bash) | No |
| Scan QR codes on the screen | Yes | Yes | Yes | No |
| Scan QR codes with the camera | Yes | No | No | Yes |
| MCP server | Yes | Yes | Yes | No |
| Command line and `bowel daemon` | Yes | Yes | Yes | No |

TUN asks for an administrator once, the first time you turn it on: macOS in its own dialog, Windows in a User Account Control prompt, Linux through polkit (`pkexec`). On Linux with systemd-resolved, that authorization also leaves a polkit rule letting your user point the resolver at the TUN device, which the core does every time TUN starts; without it (polkit older than 0.106, which reads no such rules) the desktop asks for a password three times at each start. On Linux the system proxy is GNOME's, which Cinnamon, MATE, Budgie and others share, or KDE's; on another desktop use TUN, or point your applications at the mixed port, 7899 by default. What each system has and lacks is listed in [docs/platform-parity.md](docs/platform-parity.md). On Android, you can choose apps that stay outside the VPN.

## Command line

The same program is a command line for the running bowel. On macOS it is `/Applications/bowel.app/Contents/MacOS/bowel`; link it into your `PATH` (`ln -s /Applications/bowel.app/Contents/MacOS/bowel /usr/local/bin/bowel`) or use the full path. Run without a command, it opens the app as before.

```bash
bowel status                      # the light, mode, takeover, each route's node, traffic
bowel on                          # take over (system proxy); bowel off puts it back
bowel mode global                 # rule | global | direct
bowel routes                      # routes and the node each uses
bowel route use ai auto           # choose a route's node, or auto
bowel nodes --test                # nodes with fresh delays
bowel providers add <url|file>    # also: list, update, remove, enable, disable
bowel rules add openai.com ai     # also: list, remove, test <address>
bowel conns --watch               # connections as they open and end
bowel logs -f --level warning     # follow the core's log
bowel settings set logLevel debug # also: list, get
bowel tun on                      # and: bowel gateway on
bowel tui                         # all of it on a full screen
```

`bowel tui` is the same on a whole terminal: pages for the overview, connections, logs, rules, Providers and settings, kept current. The overview has the switches (takeover, system proxy, TUN, gateway mode, allow LAN) and the routes: Enter or Space turns a switch, Enter on a route chooses its node (`t` tests the delays), `m` switches the mode and `a` adds a subscription. The settings page changes any setting by key. `/` filters connections and `?` lists the keys. A change asks y/n first. It works over SSH: `ssh server -t bowel tui`.

**A port per route group.** Two programs on this computer can reach the same site by different exits when each is given a proxy port of its own: `bowel settings set extraPorts '[{"port":7901,"route":"ai"},{"port":7902,"route":"media"}]'` opens mixed ports on `127.0.0.1` whose traffic all goes to that route group (the IDs `bowel routes` lists), whatever the rules and the mode say. They are never open to the LAN, are not part of an exported configuration, and have no place in the window; `'[]'` closes them.

Every command has `--help` with examples. Reading commands take `--json`, and `logs -f` and `conns --watch` print one JSON object per line with it. Colors and progress go away when the output is not a terminal or `NO_COLOR` is set. Exit codes are 0 for done, 1 when it failed (for example bowel isn't running) and 2 for a wrong command line. `bowel completion bash|zsh|fish|powershell` prints a completion script.

**Without a window.** `bowel daemon` runs bowel with no window, menu bar icon or Dock icon: the core, the monitors and the command line's socket only. It stays in the foreground and stops on SIGINT, SIGTERM or `bowel stop`, putting the system proxy back as the app does, so systemd, launchd or a service wrapper can keep it. `BOWEL_DATA_DIR` (or `--data-dir`) gives it a data folder of its own. The daemon does not update itself. Opening the app while the daemon runs does nothing: stop the daemon first.

```ini
# /etc/systemd/system/bowel.service
[Service]
ExecStart=/opt/bowel/bowel daemon
Environment=BOWEL_DATA_DIR=/var/lib/bowel
Restart=on-failure
```

**Agents.** Commands you type are not asked about. A command run by an agent (Claude Code, Codex, Cursor and others are recognised by their environment) follows the same policy as MCP: ask, allow or deny. The daemon has no window to ask in, so under "ask" it refuses an agent's change; allow them with `bowel settings set mcpPolicy allow`, which only you can run.

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

To build for Windows or Linux from any desktop system, fetch that platform's core first. The Windows installer also needs NSIS.

```bash
GOOS=linux GOARCH=amd64 ./scripts/fetch-core.sh
go tool mygo build -platform linux/amd64 .
```

The mobile apps are in `mobile/` and need Flutter and gomobile; Android also needs the Android SDK and NDK:

```bash
./mobile/android/build.sh       # Android: mihomo, the launcher and the bridge
./mobile/bridge/build.sh        # iOS: the bridge and the tunnel
cd mobile/flutter && flutter run
```

- How it works and where the code is: [docs/architecture.md](docs/architecture.md)
- The product's information architecture: [docs/product-ia.md](docs/product-ia.md)
- The website is static, in `site/`; open `site/index.html` to preview it. Changes to it go live when they reach `main`.
- To release, set the version in `mygo.json` and push a matching tag, such as `v0.2.0`. GitHub Actions builds macOS, Windows, Linux and Android and publishes them as one release. iOS builds only when the workflow is run by hand, and isn't published.

bowel was called Moho. The Go module is still `github.com/oboo/moho` and the bundle identifier `dev.moho.app`, which keeps macOS's camera and screen-recording permissions. On first launch the data in `~/Library/Application Support/Moho` moves to `…/bowel`.

## Credits

- [mihomo](https://github.com/MetaCubeX/mihomo): the proxy core, GPL-3.0. The released apps run it as a separate program and do not link it. iOS has to link it, which is why the iOS app isn't released.
- [MyGo](https://github.com/egoist/mygo): the native Go UI toolkit.
- [Flutter](https://flutter.dev): the mobile apps' interface.
- [OPPO Sans](resources/fonts/): the interface's typeface.

## License

bowel is licensed under the [PolyForm Noncommercial License 1.0.0](LICENSE), which allows noncommercial use only. For a commercial license, [open an issue](https://github.com/21stware/bowel/issues).

The bundled mihomo core stays under GPL-3.0. The desktop apps carry its license among their resources (`Contents/Resources/licenses/mihomo/` on macOS), the Android app names it in Settings, and each release attaches the matching source. The flag images in `assets/flags/` and the fonts in `resources/fonts/` are third-party files and keep their own licenses.
