<p align="center">
  <img src="site/icon.png" width="112" alt="bowel">
</p>

<h1 align="center">bowel</h1>

<p align="center">
bowel 是 macOS、Windows、Linux 和 Android 上的 <a href="https://github.com/MetaCubeX/mihomo">mihomo</a> 客户端。
</p>

<p align="center">
  <a href="https://github.com/21stware/bowel/releases/latest"><b>下载</b></a> ·
  <a href="https://bowel.dev/">官网</a> ·
  <a href="README.md">English</a>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="site/shots/home-zh-dark.webp">
    <img src="site/shots/home-zh.webp" width="880" alt="bowel 主窗口">
  </picture>
</p>

桌面版界面使用 Go 开发并通过 GPU 渲染，不使用 WebView。Android 版使用 Flutter 开发，卡片和皮肤与桌面版相同。

## 本分支改了什么

本仓库是 [21stware/bowel](https://github.com/21stware/bowel) 的分支：跟随上游更新，在上面保留几处自己的改动。

- **节点名屏蔽规则**：显示名字前，按规则列表删掉匹配的片段——普通文本按字面量，两端带 `/` 的按正则，条数不限。规则按「显示出来的名字」匹配，订阅塞进名字里的零宽字符不会挡路。内核拿到的名字不变：选择、规则和运行中的配置都不受影响。
- **切换提示**：路由从掉线的节点上挪走时会说「[提供商]节点不可用，已经切换到[提供商]节点」，两边的名字都过同一套规则，八秒后消失——固定节点、再次切换、失败节点恢复或停止接管时立即消失。
- **Windows 系统代理**：`InternetSetOption` 写 per-user 的 LAN 设置时，在提权进程里可能被 Windows 拒绝——而快捷方式正是以管理员运行的（TUN 需要）。这种写入改走注册表，Windows 自己的设置页读的就是同一处，写完照常广播变更。接管时还会清掉机器上遗留的 PAC 脚本：脚本优先于固定代理，留着它会让 app 把自己刚做的接管读成「别人的代理」。
- **Windows 开机启动**：框架的开机启动项写在注册表 Run 键里，那里的条目是未提权启动的——对隧道来说等于白启动。这里改成「最高权限」的计划任务：登录时以管理员令牌启动，没有 UAC 弹窗；卸载时一并删除。
- **打开文件**：设置 > 关于的「运行配置」会在资源管理器中选中该文件，而不再是 Finder 的 `open -R`，按钮文字也照实写。
- **Windows 测速**：echo 走 IP Helper（`IcmpSendEcho`），完全不启动进程——以前测一轮，每个节点都会闪一个命令行窗口，`CREATE_NO_WINDOW` 也挡不住。`ping.exe` 只留作兜底，以及需要绑定某块网卡外出（`-S`）的时候。
- **提供商地址**：每个提供商的节点列表都显示节点拨号用的服务器地址，按提供商文件缓存，不必每次快照重读。
- **出口检测**：出口探测可以用预设（Cloudflare、ip.sb、ipwho.is、ip-api、ipinfo.io），也可以填任何返回地址和国家的 http(s) 地址。
- **连接页**：清理按钮挪到应用筛选旁边，并需要再点一次才真正清理。

上游 0.3 起自带 Windows TUN：内核的提权副本放在 `%ProgramData%`，由计划任务启动；控制台子进程统一走 `nowindow`。

## 界面与功能

以下截图来自 macOS 版。

### 路由
按用途分配不同路由，可手动指定出口节点或由自动测速选择。

<img src="site/shots/routes-zh.webp" width="880" alt="路由">

### 连接
查看每个连接的应用、目标、规则、出口和速率，也可以直接修改指定域名的路由。

<img src="site/shots/activity-zh.webp" width="880" alt="连接日志">

### 提供商
支持添加订阅链接、节点链接、YAML 配置或扫码导入，并显示流量与到期时间。

<img src="site/shots/providers-zh.webp" width="880" alt="提供商">

### 菜单栏
支持在菜单栏（Windows 和 Linux 上是系统托盘）开关接管状态、切换运行模式和「其他流量」的出口节点。

<img src="site/shots/panel-zh.webp" width="300" alt="菜单栏面板">

### 主题皮肤
十三种皮肤，换的是整个界面的样子。

<table>
  <tr>
    <td><img src="site/shots/skin-broadsheet-zh.webp" alt="报刊"><br><sub>报刊</sub></td>
    <td><img src="site/shots/skin-phosphor-zh.webp" alt="荧光屏"><br><sub>荧光屏</sub></td>
    <td><img src="site/shots/skin-neon-zh.webp" alt="霓虹"><br><sub>霓虹</sub></td>
  </tr>
  <tr>
    <td><img src="site/shots/skin-blueprint-zh.webp" alt="蓝图"><br><sub>蓝图</sub></td>
    <td><img src="site/shots/skin-sakura-zh.webp" alt="樱"><br><sub>樱</sub></td>
    <td><img src="site/shots/skin-paper-zh.webp" alt="纸墨"><br><sub>纸墨（默认）</sub></td>
  </tr>
  <tr>
    <td><img src="site/shots/skin-shanshui-zh.webp" alt="山水"><br><sub>山水</sub></td>
    <td><img src="site/shots/skin-celadon-zh.webp" alt="青瓷"><br><sub>青瓷</sub></td>
    <td><img src="site/shots/skin-dunhuang-zh.webp" alt="敦煌"><br><sub>敦煌</sub></td>
  </tr>
  <tr>
    <td><img src="site/shots/skin-dusk-zh.webp" alt="暮色"><br><sub>暮色</sub></td>
    <td><img src="site/shots/skin-aurora-zh.webp" alt="极光"><br><sub>极光</sub></td>
    <td><img src="site/shots/skin-obsidian-zh.webp" alt="黑曜"><br><sub>黑曜</sub></td>
  </tr>
  <tr>
    <td><img src="site/shots/skin-riso-zh.webp" alt="孔版"><br><sub>孔版</sub></td>
  </tr>
</table>

### 其他
- **自定义规则**：支持按域名、关键词或 IP 段添加分流规则并测试匹配结果。
- **出口位置实测**：通过 Cloudflare 请求实测节点真实的出口所在地。
- **网关模式**（macOS、Linux）：支持作为局域网网关为电视、游戏机和手机提供代理服务。
- **TUN**（macOS、Windows、Linux）：接管不遵循系统代理设置的应用程序流量。
- **开发工具**（macOS、Linux）：Docker、终端和 Git 不读系统代理，bowel 列出哪些没有接入，点一下写好它们自己的配置，撤销时原样删除。
- **下载源**：实测 npm、pip、Go 和 Cargo 的官方源与各个镜像，点一下换源；当前的源连不上时，一键换到最快的那个，或让它重新走节点。
- **内网直连**：局域网、Tailscale 和公司 VPN 的地址自动直连，全局模式下也一样。
- **MCP**：内置 MCP 服务器，供 Claude Code 等 Agent 读取状态、排查网络问题，改设置前默认先问你。下面是 macOS 的命令，Windows 和 Linux 上请换成 bowel 程序所在的路径。
  ```bash
  claude mcp add --scope user bowel -- /Applications/bowel.app/Contents/MacOS/bowel mcp
  ```
- **设置与语言**：设置项支持搜索（⌘F），支持简体中文、繁體中文、English 与日本語，支持浅色与深色。

## 安装

macOS，使用 [Homebrew](https://brew.sh)：

```bash
brew install --cask 21stware/tap/bowel
```

Windows，在 PowerShell 中运行：

```powershell
irm https://github.com/21stware/bowel/releases/latest/download/install.ps1 | iex
```

Debian、Ubuntu 和 Fedora。这条命令会添加一个已签名的软件源，之后由 `apt` 或 `dnf` 更新 bowel：

```bash
curl -fsSL https://21stware.github.io/packages/install.sh | sh
```

也可以从 [Releases](https://github.com/21stware/bowel/releases/latest) 下载对应平台的安装包：

| 平台 | 文件 | 要求 |
| --- | --- | --- |
| macOS | `bowel-<版本>-arm64.dmg` | macOS 12 或更新版本，Apple 芯片 |
| Windows | `bowel-setup-<版本>-amd64.exe` 或 `-arm64.exe` | x64 或 Arm64 |
| Linux | `bowel_<版本>_<架构>.deb`、`bowel-<版本>-1.<rpm 架构>.rpm` 或 `bowel-<版本>-linux-<架构>.tar.gz` | amd64 或 arm64 |
| Android | `bowel-<版本>-android-arm64.apk` | Android 8.0 或更新版本，arm64 |

- **macOS**：把 bowel 拖进「应用程序」。安装包用 Developer ID 签名，并经过 Apple 公证。
- **Windows**：运行安装程序。安装包没有代码签名，运行前 Windows 可能会弹出提示。
- **Linux**：安装 `.deb` 或 `.rpm`。要安装到当前用户的 `~/.local`，从同一个 Release 下载 `install.sh` 和 `.tar.gz`，不用解压，运行 `sh install.sh bowel-<版本>-linux-<架构>.tar.gz`。这个脚本与上面添加软件源的脚本不同。
- **Android**：安装 APK，如有提示请允许浏览器安装应用。首次连接时系统会请求 VPN 权限。
- **iOS**：暂未发布。

打开后添加一个 Provider（订阅链接、节点链接或扫码），打开接管。

部分功能因平台而异：

| | macOS | Windows | Linux | Android |
| --- | --- | --- | --- | --- |
| 接管方式 | 系统代理或 TUN | 系统代理或 TUN | 系统代理（GNOME、KDE）或 TUN | 系统 VPN |
| 网关模式 | 支持 | 不支持 | 支持 | 不支持 |
| 开发工具 | 支持 | 不支持 | 支持（zsh、bash） | 不支持 |
| 扫描屏幕上的二维码 | 支持 | 支持 | 支持 | 不支持 |
| 用摄像头扫描二维码 | 支持 | 不支持 | 不支持 | 支持 |
| MCP 服务器 | 支持 | 支持 | 支持 | 不支持 |
| 命令行和 `bowel daemon` | 支持 | 支持 | 支持 | 不支持 |

第一次开启 TUN 时需要管理员授权一次：macOS 是系统自己的对话框，Windows 是用户账户控制（UAC），Linux 通过 polkit（`pkexec`）。在使用 systemd-resolved 的 Linux 上，这次授权还会留下一条 polkit 规则，允许当前用户把解析器指向 TUN 网卡（内核每次启动 TUN 都要这么做）；没有这条规则时（polkit 早于 0.106，不读取这类规则），每次启动 TUN 桌面都会连问三次密码。Linux 上的系统代理是 GNOME 的（Cinnamon、MATE、Budgie 等桌面共用它）或 KDE 的；其他桌面请使用 TUN，或让应用自行连接混合端口，默认是 7899。各系统具体有什么、缺什么，见 [docs/platform-parity.md](docs/platform-parity.md)。Android 版可以选择不经过 VPN 的应用。

## 命令行

同一个程序也是命令行，用来操作运行中的 bowel。macOS 上它在 `/Applications/bowel.app/Contents/MacOS/bowel`，可以链接到 `PATH` 里（`ln -s /Applications/bowel.app/Contents/MacOS/bowel /usr/local/bin/bowel`），或直接写完整路径。不带命令运行时，和以前一样打开应用。

```bash
bowel status                      # 状态灯、模式、接管、各路由的节点、流量
bowel on                          # 开始接管（系统代理）；bowel off 恢复原样
bowel mode global                 # rule | global | direct
bowel routes                      # 路由和各自使用的节点
bowel route use ai auto           # 给路由选节点，或 auto 自动
bowel nodes --test                # 重新测速后列出节点
bowel providers add <地址|文件>    # 还有 list、update、remove、enable、disable
bowel rules add openai.com ai     # 还有 list、remove、test <地址>
bowel conns --watch               # 持续显示新连接和结束的连接
bowel logs -f --level warning     # 持续输出内核日志
bowel settings set logLevel debug # 还有 list、get
bowel tun on                      # 以及 bowel gateway on
bowel tui                         # 全屏界面
```

`bowel tui` 是同一套功能的全屏界面：概览、连接、日志、规则、Provider 和设置六个页面，实时刷新。概览页有一组开关（接管、系统代理、TUN、网关模式、允许局域网）和路由列表：在开关上按回车或空格切换，在路由上按回车选节点（`t` 测速），`m` 切换模式，`a` 添加订阅。设置页可以修改任意一项设置。`/` 过滤连接，`?` 查看全部按键。会改变网络的操作先问 y/n。通过 SSH 也能用：`ssh server -t bowel tui`。

**每个路由组一个端口。** 想让本机的两个程序用不同的出口访问同一个网站，可以给它们各配一个代理端口：`bowel settings set extraPorts '[{"port":7901,"route":"ai"},{"port":7902,"route":"media"}]'` 会在 `127.0.0.1` 上再开几个混合端口，每个端口的流量固定走对应的路由组（用 `bowel routes` 里的 ID），不看规则，也不受全局、直连模式影响。这些端口不对局域网开放，不会写进导出的配置，界面里也没有对应的设置；设成 `'[]'` 即关闭。

每个命令都有 `--help` 和例子。读取类命令支持 `--json`，`logs -f` 和 `conns --watch` 加上它后每行输出一个 JSON 对象。输出不是终端或设置了 `NO_COLOR` 时，不显示颜色和进度动画。退出码：0 成功，1 执行失败（例如 bowel 没在运行），2 命令写错了。`bowel completion bash|zsh|fish|powershell` 输出补全脚本。

**无窗口运行。** `bowel daemon` 不开窗口、菜单栏图标和 Dock 图标，只运行内核、监控和命令行通道。它在前台运行，收到 SIGINT、SIGTERM 或 `bowel stop` 后退出，并像应用退出时一样恢复系统代理，适合交给 systemd、launchd 或服务包装器托管。用 `BOWEL_DATA_DIR`（或 `--data-dir`）可以给它单独的数据目录。daemon 不会自动升级。daemon 运行时再打开应用不会有反应，需要先停掉 daemon。

```ini
# /etc/systemd/system/bowel.service
[Service]
ExecStart=/opt/bowel/bowel daemon
Environment=BOWEL_DATA_DIR=/var/lib/bowel
Restart=on-failure
```

**Agent。** 你自己敲的命令不会被询问。由 agent 运行的命令（Claude Code、Codex、Cursor 等，按它们的环境变量识别）和 MCP 用同一个策略：询问、允许或拒绝。daemon 没有窗口可以弹框，策略为“询问”时会拒绝 agent 的变更；可以用 `bowel settings set mcpPolicy allow` 放行，这条命令只有你自己能执行。

## 开发

需要 Go 1.27.3+（`GOTOOLCHAIN=auto` 会自动下载）。不需要 Node 或 Bun。

```bash
./scripts/fetch-core.sh     # 下载 mihomo 内核和 GeoSite/GeoIP 数据到 resources/
go tool mygo dev            # 运行，改代码自动重建
go tool mygo build          # 打包 bowel.app 和 .dmg（已签名，未公证）
./scripts/release.sh        # 打包、公证并装订，发版用这个
CGO_ENABLED=0 go test ./... # 包含会启动真实内核的测试
./scripts/site-shots.sh     # 重新生成 README 和官网的截图（需要 cwebp）
```

在任意桌面系统上都可以构建 Windows 或 Linux 版，先下载对应平台的内核。Windows 安装程序还需要 NSIS。

```bash
GOOS=linux GOARCH=amd64 ./scripts/fetch-core.sh
go tool mygo build -platform linux/amd64 .
```

移动端代码在 `mobile/`，需要 Flutter 和 gomobile，Android 还需要 Android SDK 和 NDK：

```bash
./mobile/android/build.sh       # Android：mihomo、启动器和桥接层
./mobile/bridge/build.sh        # iOS：桥接层和隧道
cd mobile/flutter && flutter run
```

- 工作原理与代码结构：[docs/architecture.md](docs/architecture.md)
- 产品信息架构：[docs/product-ia.md](docs/product-ia.md)
- 官网是 `site/` 下的静态页面，直接打开 `site/index.html` 即可预览。合入 `main` 后自动上线。
- 发版时先改 `mygo.json` 里的版本号，再推送对应的标签（如 `v0.2.0`）。GitHub Actions 会构建 macOS、Windows、Linux 和 Android 版，并发布到同一个 Release。iOS 版只在手动运行工作流时构建，不会发布。

bowel 原名 Moho。Go 模块仍是 `github.com/oboo/moho`，Bundle ID 仍是 `dev.moho.app`（保留 macOS 已授予的摄像头和录屏权限）。首次启动时，`~/Library/Application Support/Moho` 里的数据会迁移到 `…/bowel`。

## 致谢

- [mihomo](https://github.com/MetaCubeX/mihomo)：代理内核，GPL-3.0。已发布的版本把它作为独立进程运行，不链接它的代码。iOS 必须链接它，因此 iOS 版暂未发布。
- [MyGo](https://github.com/egoist/mygo)：原生 Go UI 框架。
- [Flutter](https://flutter.dev)：移动端界面框架。
- [OPPO Sans](resources/fonts/)：界面字体。

## 许可证

bowel 采用 [PolyForm Noncommercial License 1.0.0](LICENSE) 授权，仅限非商业用途。如需商业授权，请[提交 issue](https://github.com/21stware/bowel/issues)。

随应用打包的 mihomo 内核仍适用 GPL-3.0。桌面版的资源目录里附有它的许可证（macOS 上是 `Contents/Resources/licenses/mihomo/`），Android 版在设置中注明，每个 Release 都附带对应版本的源码。`assets/flags/` 中的旗帜图片和 `resources/fonts/` 中的字体属于第三方文件，仍适用各自的许可证。
