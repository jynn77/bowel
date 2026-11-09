<p align="center">
  <img src="site/icon.png" width="112" alt="bowel">
</p>

<h1 align="center">bowel</h1>

<p align="center">
bowel 是 macOS 上的 <a href="https://github.com/MetaCubeX/mihomo">mihomo</a> 客户端。
</p>

<p align="center">
  <a href="https://github.com/21stware/bowel/releases/latest"><b>下载 macOS 版</b></a> ·
  <a href="https://21stware.github.io/bowel/">官网</a> ·
  <a href="README.md">English</a>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="site/shots/home-zh-dark.webp">
    <img src="site/shots/home-zh.webp" width="880" alt="bowel 主窗口">
  </picture>
</p>

界面使用 Go 开发并通过 GPU 渲染，不使用 WebView。

> **本分支同时构建 Windows 版**，TUN 一并支持。安装包和移植过程中的其他改动见
> [Windows（本分支）](#windows本分支)。

## 界面与功能

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
支持在菜单栏开关接管状态、切换运行模式和默认出口节点。

<img src="site/shots/panel-zh.webp" width="300" alt="菜单栏面板">

### 主题皮肤
六种皮肤，换的是整个界面的样子。

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
</table>

### 其他
- **自定义规则**：支持按域名、关键词或 IP 段添加分流规则并测试匹配结果。
- **出口位置实测**：通过 Cloudflare 请求实测节点真实的出口所在地。
- **网关模式**：支持作为局域网网关为电视、游戏机和手机提供代理服务。
- **TUN**：接管不遵循系统代理设置的应用程序流量。
- **MCP**：内置 MCP 服务器，供 Claude Code 等 Agent 读取状态、排查网络问题，改设置前默认先问你。
  ```bash
  claude mcp add --scope user bowel -- /Applications/bowel.app/Contents/MacOS/bowel mcp
  ```
- **设置与语言**：设置项支持搜索（⌘F），支持简体中文、繁體中文、English 与日本語，支持浅色与深色。

## 安装

1. 从 [Releases](https://github.com/21stware/bowel/releases/latest) 下载 `bowel-<版本>-arm64.dmg`，把 bowel 拖进「应用程序」。
2. 打开后添加一个 Provider（订阅链接、节点链接或扫码），打开接管。

需要 macOS 12 或更新版本，目前发布的是 Apple 芯片版本。安装包用 Developer ID 签名，并经过 Apple 公证。

## Windows（本分支）

上游没有 Windows 版，本分支补上了。

```bash
GOOS=windows GOARCH=amd64 ./scripts/fetch-core.sh   # 内核取到 resources/windows-amd64/bin
# 再把 wintun.dll（0.14.1 amd64，取自 wintun.net）放到同一目录 —— TUN 需要它
go tool mygo build -platform windows/amd64 -o build .
# → build/windows-amd64/bowel-setup-<版本>-amd64.exe   （NSIS 安装包）
# → build/windows-amd64/bowel.exe                      （免安装版）
```

需要 Windows 10 或更新版本（amd64）。安装包默认装在 `%LOCALAPPDATA%` 或你选的目录，快捷方式带管理员权限，TUN 才能打开虚拟网卡。

### 移植时解决了什么

- **测速**：`ping.exe` 以 `CREATE_NO_WINDOW` 运行，延迟从输出里按正则取，不受系统语言影响。以前每测一个节点就闪一个命令行窗口。
- **TUN**：把内核副本和 `wintun.dll` 放到 `%APPDATA%\bowel\bin`，只有 bowel 自身提权时才开网卡。没提权时开关会说明原因，而不是直接失败。
- **关 TUN**：先让内核自己把隧道拆掉，再停内核；崩溃或强杀留下的路由，下次启动时收回。Windows 没有 SIGTERM，以前停内核就是 `Process.Kill()`——适配器和 `198.18.0.2` 的默认路由（metric 0，压过 WLAN 的 35）留在系统里，所有包进了一条没有会话的隧道，表现就是「关掉 TUN 就没网」。
- **解析器**：无论开不开 TUN，内核始终带自己的 `dns:` 段。它不只是给流量用的——节点域名要解析，VLESS 的 ECH 还要在 TLS 握手前先查 DNS 记录。以前 TUN 关、内置 DNS 也关时，恰好这批节点测速全挂，别的订阅却正常。
- **内核日志**：Windows 版把内核的控制台隐藏，日志页直接读内核自己的输出流。

### 相对 0.2.0 的其他改动

- **出口地址质量**：用设置里指定的一个地址服务（默认 ip-api.com，也可换 ipwho.is / ip.sb / ipapi.co）判断出口是机房还是家宽、有没有被标记为代理，再结合节点延迟给出「优秀 / 良好 / 一般 / 较差」。默认关闭——开启后会把出口 IP 发到外面。
- **更多预设**：测速地址、出口测试地址、质量检测地址三者都是「常用地址下拉 + 文本框」，也可以自己填。
- **订阅地址列**：提供商页多一列 `host:port`，跟订阅文件里一致（Clash YAML、纯链接列表、整体 base64 都支持），按文件大小与修改时间缓存。
- **清理列表**：标签上方的橡皮擦清空当前页列出的内容。已经结束的连接一次点击即清；活跃和全部需要再点一次确认，两者都不会断开连接。
- **我的位置**：隧道稳定后会重测一次，之后每 10 分钟校正，开 TUN 后不再把节点所在国家当成你的位置。
- **地区识别**：匹配前剔除零宽字符，`H​K`、`日​本` 这类名字也能认出来。

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

- 工作原理与代码结构：[docs/architecture.md](docs/architecture.md)
- 产品信息架构：[docs/product-ia.md](docs/product-ia.md)
- 官网是 `site/` 下的静态页面，直接打开 `site/index.html` 即可预览。合入 `main` 后自动上线。
- 发版时先改 `mygo.json` 里的版本号，再推送对应的标签（如 `v0.1.0`）。GitHub Actions 会打包、签名、公证，并把磁盘映像附到新的 Release 上。

bowel 原名 Moho。Go 模块仍是 `github.com/oboo/moho`，Bundle ID 仍是 `dev.moho.app`（保留 macOS 已授予的摄像头和录屏权限）。首次启动时，`~/Library/Application Support/Moho` 里的数据会迁移到 `…/bowel`。

## 致谢

- [mihomo](https://github.com/MetaCubeX/mihomo)：代理内核，GPL-3.0。bowel 把它作为独立进程运行，不链接它的代码。
- [MyGo](https://github.com/egoist/mygo)：原生 Go UI 框架。
- [OPPO Sans](resources/fonts/)：界面字体。

## 许可证

bowel 采用 [PolyForm Noncommercial License 1.0.0](LICENSE) 授权，仅限非商业用途。如需商业授权，请[提交 issue](https://github.com/21stware/bowel/issues)。

随应用打包的 mihomo 内核仍适用 GPL-3.0。应用内的 `Contents/Resources/licenses/mihomo/` 附有它的许可证，每个 Release 都附带对应版本的源码。`assets/flags/` 中的旗帜图片和 `resources/fonts/` 中的字体属于第三方文件，仍适用各自的许可证。
