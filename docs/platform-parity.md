# Platform parity

What the desktop app does on each system, taking macOS as the baseline: every behaviour a `_darwin.go` file gives has a row. A row says either how the other systems do the same thing, why they have no need of it, or that it is left for later and why.

How far each implementation has been tried is its own column, because "it compiles" and "it has run" are different claims:

- **macOS**: developed and run on.
- **Linux**: the package tests run on Ubuntu 24.04 (arm64, kernel 6.8), unprivileged and with no desktop session. Anything that needs a desktop (polkit's dialog, GNOME's or KDE's settings, the tray, a screenshot tool) or root on a real network (the TUN device itself, forwarding) has **not** been run.
- **Windows**: cross-compiled and vetted, with the logic that needs no Windows API tested on other systems. **Nothing has run on Windows on our side**; what is known from a real machine comes from users' reports, noted where it applies.

## The table

| Feature | macOS | Linux | Windows |
| --- | --- | --- | --- |
| TUN: one authorisation | `osascript` with administrator privileges | `pkexec` (polkit) | UAC prompt (`ShellExecuteEx`, `runas`) |
| TUN: what it grants | setuid-root copy of the core in the data directory | root-owned copy with file capabilities `cap_net_admin,cap_net_bind_service,cap_net_raw`; setuid root where `setcap` is missing. With systemd-resolved, also a polkit rule for the user: the four `org.freedesktop.resolve1` actions behind the core's `resolvectl` calls | copy under `%ProgramData%\bowel\tun\<SID>\` writable by administrators only, and two scheduled tasks that run it elevated: one in no session of the user's (S4U), one in the session they are logged on in, for accounts the first cannot be started for |
| TUN: running the core | child process | child process | the first task that starts it, started and ended with `schtasks`: the S4U one, else the logged-on session's (a domain account away from its domain controller), where a copy of bowel's own program beside the core (`bowel-tun.exe`) starts it without a window, in a job that ends the core when the task is ended; log read from the controller |
| TUN: files the core's config names (providers, rule sets) | beside the core's directory, allowed through `SAFE_PATHS` in the child's environment | same | inside the core's directory (`core\providers`, `core\rulesets`): the task's core has no environment of the app's |
| TUN: its core does not start | the bundled core runs as a child in its place; the TUN entry says why, and turning TUN on again tries again | same | same; a task that was deleted counts as not installed, and is asked for again, and so does an installation from before there were two tasks whose one task starts nothing |
| TUN: stale copy after an update | offered, one authorisation | same; an installation without the polkit rule, or whose rule names another TUN device than the settings, or whose rule file was deleted or changed (where the user may read polkit's directory), is authorised once more when TUN is next turned on | same |
| System proxy: set, restore | `networksetup`, every service | GNOME (`gsettings`, shared by Cinnamon, MATE, Budgie…) and KDE (`kwriteconfig`, `kioslaverc`) | WinINet LAN settings |
| System proxy: measured every 2 s | `scutil --proxy` | the session's own desktop's settings | WinINet |
| System proxy: other desktops | — | not available; the app says so and points at TUN | — |
| Developer networks: routes, tunnels | `netstat -rn`, interface names | `ip route show table all`, sysfs for what an interface is | `GetIpForwardTable2`, adapter types |
| Developer networks: split DNS | `scutil --dns` | `resolvectl` (systemd-resolved) | name resolution policy table, tunnel adapters' suffixes |
| Developer networks: Tailscale | CLI, or interface and resolver | same | same |
| Bind past TUN (IPv6 probe) | `IPV6_BOUND_IF` | `SO_BINDTODEVICE` | `IPV6_UNICAST_IF` / `IP_UNICAST_IF` |
| Ping a node's server | `/sbin/ping -b iface` | `ping -I iface` | `PING.EXE -S address` |
| Primary interface | `scutil` | `/proc/net/route` | route table |
| Gateway mode | sudoers rule for two `sysctl` commands, `caffeinate` | sudoers rule for two `sysctl` commands, `systemd-inhibit` | **later** |
| Gateway: network facts (Wi-Fi, DHCP, firewall) | `networksetup`, `ipconfig`, socketfilterfw | sysfs, `ip addr`, ufw / firewalld | adapters (Wi-Fi, DHCP); no firewall check |
| Which app owns a connection | bowel reads the kernel's socket list | **not needed** | **not needed** |
| Developer tools: terminal | zsh (`.zshenv`) | zsh or bash (`.bashrc`), by `$SHELL` | **later** |
| Developer tools: Git HTTPS, Git SSH | yes | yes; SSH with OpenBSD `nc` or `ncat` | **later** |
| Developer tools: Docker daemon | Colima | Colima only; the system's dockerd **later** | **later** |
| Download sources (npm, pip, Go, Cargo) | yes | yes | yes |
| QR: picture file | yes (HEIC and others through `sips`) | formats Go decodes | formats Go decodes |
| QR: screen | `screencapture`, per display | first of grim, spectacle, gnome-screenshot, maim, scrot, ImageMagick | PowerShell, .NET `CopyFromScreen` |
| QR: camera | AVFoundation | **later** | **later** |
| Dock icon, flag emoji drawing, quit pump | yes | **not applicable** | **not applicable** |
| Tray | menu bar | system tray | system tray |

## Not needed, and why

- **Which app owns a connection** (`internal/procinfo`). mihomo names the process behind a connection itself, on every system. `procinfo` exists only because macOS 27 hides the kernel's socket list from a process whose parent is an ad-hoc signed app, which the core is; bowel, opened like any app, reads the list for it. Linux (`sock_diag` and `/proc`) and Windows (`GetExtendedTcpTable`) put no such limit on the core, so the monitor's fallback is never asked.
- **Dock icon** (`syncDock`), **flag drawing** (`flagdraw_darwin.go`), **quit pump** (`quit_pump_darwin.go`). The first is the Dock's, which the other systems do not have. The second works around how macOS's text system colours emoji flags; elsewhere the plain drawing is right. The third keeps AppKit's run loop turning during a quit.
- **Screen Recording permission**. Only macOS keeps the screen behind a permission of its own. Elsewhere a capture that fails says so.

## Left for later, and why

- **Gateway mode on Windows.** Three things are missing, and each needs trying on Windows before it is written. Windows forwards per adapter: both the LAN's and TUN's must be switched, and TUN's is made anew each time. Nothing like a sudoers rule lets a user's process do that unelevated, so it would take a second task or a service. And the firewall drops what arrives to be forwarded, and the resolver's port 53, until rules are added. The interface does not offer the gateway on Windows.
- **Developer tools on Windows.** The model is another one: terminals are PowerShell and cmd, whose environment is the user's registry (`HKCU\Environment`) and a profile script; OpenSSH for Windows has no `nc` to call. Nothing is read or written there, and the section is not shown. Download sources work.
- **The system's Docker daemon on Linux.** Its proxy is `/etc/docker/daemon.json` or a systemd drop-in, both root's, and takes a restart of the daemon. Colima's, which bowel does connect, is a file of the user's.
- **Camera on Linux and Windows.** Video4Linux and Media Foundation are each a capture pipeline of their own; the screen and picture files cover the same use meanwhile.
- **System proxy on other Linux desktops** (sway, Hyprland, Xfce with no GSettings schema). There is no setting to write; programs there follow environment variables, which the terminal's connection under Developer tools sets.
- **A polkit action of bowel's own.** `pkexec` shows its generic text ("run /bin/sh as the super user") since bowel installs no policy file. Packaging one in the `.deb` and `.rpm` would let the dialog say what it is for.

## Privileged paths

The rules are the same on every system: the authorisation is asked once, it grants the narrowest thing that works, and what bowel changed is put back, also after a crash.

| | What root (or elevation) does, once | What it leaves | Undo |
| --- | --- | --- | --- |
| macOS TUN | `chown root:admin`, `chmod 4755`, `mv` | setuid core in the data directory | delete the file |
| Linux TUN | `chown root:root`, `chmod 0755`, `setcap`, `mv`; with systemd-resolved, `install` of the rule | core with three network capabilities, running as the user; `/etc/polkit-1/rules.d/49-bowel-tun-<uid>.rules` | delete the two files and `bin/tun-dns-rule` beside the copy; removing the package (`.deb`, `.rpm`, AUR) deletes every user's rule |
| Windows TUN | `mkdir`, `icacls`, `copy`, `schtasks /End` and `/Create`, ending a core still running from the copy | protected copy and `bowel-tun.exe` beside it, tasks `bowel-tun-<SID>` and `bowel-tun-<SID>-interactive` | delete the tasks and the directory |
| macOS, Linux gateway | `install`, `visudo -c`, `mv` | `/etc/sudoers.d/bowel-gateway-<uid>`: two `sysctl` commands | delete the file |

On Windows the mark beside the copy is what `Installed` reads, so deleting the task alone leaves it: the app finds the task missing the next time it starts the core (`schtasks /Query`), runs the bundled core, and asks for the authorisation again when TUN is next turned on. Each task's description names all three things to delete.

The polkit rule on Linux exists because the core, with `auto-route`, points systemd-resolved at its resolver for the TUN device each time the device comes up (`resolvectl domain`, `default-route`, `dns`) and takes that back when it goes (`revert`). Those are polkit actions that ask for an administrator, and file capabilities mean nothing to polkit, so a core running as the user brought three password dialogs at every start (issue 71). Skipping the calls (`dns-mode: disabled`) is not the way out: resolved would go on asking the network's own servers over sockets bound to the physical link, past TUN. The rule names the user and, where polkit is told the link (systemd 256 and later), the TUN device; with an older systemd it allows that user the four actions on any link. That adds little to what the copy already gives, since whoever may run it can route every packet of the machine. Its text is kept beside the copy (`bin/tun-dns-rule`), the mark that the rule was written; where polkit's directory refused the rule, the mark ends in a line saying so, and that machine is not asked again. Before TUN starts bowel also reads the rule itself and compares it with what it would write, so a rule deleted or edited by hand is written again under one authorization. That works only where the user may read `/etc/polkit-1/rules.d`: where the directory is closed to users (the report on issue 71 needed `sudo` to list Fedora 45's, which suggests it is; not confirmed), the mark is taken at its word, and a rule deleted there goes unnoticed until the mark is deleted too. Removing the package deletes the rules of every user (`scripts/linux-postrm.sh`), and leaves the copy and the mark in each home, as it leaves the settings. Nothing is written where the core runs as root, where `resolvectl` is missing, or where polkit reads no `rules.d` (0.105 and its `.pkla` files): there the dialogs stay.

The second task exists because an S4U logon of a domain account asks the domain controller, and fails when it cannot be reached (issue 66: Task Scheduler event 104, `LogonUserS4U`, `0x8007051F`). It is not the only task because it runs only while the user is logged on at the desktop: `bowel daemon` over SSH on a machine nobody is logged on to has TUN through the first task alone. Which task worked is kept in `tun-task` in the data folder, so that the wait for a task that starts nothing is paid once.

At run time: forwarding as it was is kept in `gateway-before.json` and put back when the gateway goes off or on the next start; the system proxy as it was is kept in `sysproxy-before.json` and restored on quit; TUN's routes go with the core's process. On Windows a core that a crash of the app left running is ended (`schtasks /End`) before the next one starts.

## Not yet run on a real machine

Linux:

1. The polkit dialog and the whole `pkexec` installation, on a desktop session. (The commands' quoting ran in `sh`, and reading capabilities was checked against a file `setcap` really marked.)
2. The core opening a TUN device with file capabilities alone, with `auto-route`, on a real network; and the same on a home directory mounted `nosuid`, which should be refused. A user's report on Fedora 45 with KDE (issue 71, bowel 0.3.0) covers the first half: the `pkexec` installation ran as written and the device came up, with three polkit dialogs from the core's `resolvectl` calls. The rule written since has **not** been read by a real polkit: its text was only run in another JavaScript engine against a stand-in for polkit's objects. Open: that polkitd accepts it, that systemd 256 and later pass the link as `interface`, that `revert` at TUN off is covered, and what re-created the device in that report after a dismissed dialog (the core ignores `resolvectl`'s result, so it was not a retry of it). Also not run on a desktop: `Uninstall` removing the rule through `pkexec`, the second authorization after a rule deleted by hand, and the removal script in a real `.rpm` or the AUR package (the `.deb`'s ran in the VM, in a stand-in package: rules kept on upgrade, deleted on removal).
3. System proxy on real GNOME and KDE sessions (tested against stand-ins for `gsettings` and `kwriteconfig6`), including whether running programs pick the change up.
4. Gateway mode end to end: the sudoers rule through real `sudo` (the rule passes real `visudo`), forwarding with TUN's rules in place, and the resolver on `0.0.0.0:53` beside systemd-resolved's stub on `127.0.0.53`.
5. Split DNS from `resolvectl` on a machine that runs systemd-resolved, and Tailscale's routes in table 52 (parsed from samples).
6. Screen scanning with each screenshot tool, on Wayland and X11; the tray.
7. `ping` (the test machine has none installed).

Windows, all of it on our side. A user's report on Windows 11 (issue 59, bowel 0.3.0) covers the first three for TUN: the UAC installation, the copy, its mark and the task were made as written; with the core able to read its files, the task started from the unelevated `schtasks /Run`, the adapter and routes came up, and `/End` took them down. That report is also where the fixes after 0.3.0 come from (files inside the core's directory, the bundled core as a fallback), and those have **not** been run on Windows. In the order they would fail:

1. The UAC prompt and the elevated command: `ShellExecuteExW`'s structure, cmd's quoting, `icacls` and `schtasks /Create /XML` with a UTF-16 definition.
2. Whether a task with an S4U principal and `HighestAvailable` starts from the user's unelevated `schtasks /Run`, runs elevated with no window, and ends with `/End`.
3. The core finding Wintun and opening the adapter from session 0.
4. The second task, none of which has run anywhere: that a task with an `InteractiveToken` principal and `HighestAvailable` starts from the unelevated `schtasks /Run` where the S4U one does not (a domain account with its controller out of reach); that its action, `bowel-tun.exe tun-core …` (a copy of the app's program, a GUI program), starts the core with no window; that `schtasks /End`, which ends that launcher alone, ends the core through the launcher's kill-on-close job; that the installation's `bowel-tun.exe tun-core --kill` ends a core left running; and that an installation made before is asked for again. A user's report on issue 66 (a domain-joined Windows 11 machine) covers the first point, and is why the action is no longer `conhost.exe --headless`: that showed no window, but `/End` ended the console host and left the elevated core running.
5. Waiting on the elevated process (`OpenProcess(SYNCHRONIZE)`, else the process list) and the log through the controller.
6. Routes, adapters and the policy table read through the IP helper API and the registry; `IPV6_UNICAST_IF`; `PING.EXE -S`.
7. Screen capture through PowerShell, at display scales other than 100%.
8. A user who is not an administrator: the task's `HighestAvailable` is then no elevation, and TUN will not open.
