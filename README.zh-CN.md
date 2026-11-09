<p align="center">
  <img src="site/icon.png" width="112" alt="bowel">
</p>

<h1 align="center">bowel</h1>

<p align="center">
bowel 是 <a href="https://github.com/MetaCubeX/mihomo">mihomo</a> 的客户端。上游只发布 macOS 版，
<b>本分支补上了 Windows</b>，并在这之上改了一些东西。
</p>

<p align="center">
  <a href="README.md">English</a>
</p>

## 本分支改了什么

### Windows

上游没有 Windows 版，这里是移植，不是套壳。

- **测速**：`ping.exe` 以 `CREATE_NO_WINDOW` 运行，延迟从输出里按正则取，不受系统语言影响。以前每测一个节点就闪一个命令行窗口，一轮测速能弹几十个。
- **TUN**：内核副本和 `wintun.dll` 放到 `%APPDATA%\bowel\bin`，只在 bowel 自身提权时开网卡；没提权时开关会说明原因，而不是直接失败。
- **关 TUN**：先让内核把隧道拆掉、再停内核；崩溃或强杀留下的路由，下次启动时收回。Windows 没有 SIGTERM，以前停内核就是 `Process.Kill()`——适配器和 `198.18.0.2` 的默认路由（metric 0，压过 WLAN 的 35）会留在系统里，所有包进一条没有会话的隧道，表现就是「关掉 TUN 就没网」。
- **解析器**：无论开不开 TUN，内核始终带自己的 `dns:` 段。它不只是给流量用的：节点域名要解析，VLESS 的 ECH 还要在 TLS 握手前查 DNS 记录。TUN 关、内置 DNS 也关时，正好这批节点全部测速失败，别的订阅却正常。
- **系统代理**：`InternetSetOption` 写 per-user 的 LAN 设置时，在提权进程里可能被 Windows 拒绝——而快捷方式正是以管理员运行的（TUN 需要）。现在这种写入会改走注册表，Windows 自己的设置页读的就是同一处，写完照常广播变更。接管时还会清掉机器上遗留的 PAC 脚本：脚本优先于固定代理，留着它会让 app 把自己刚做的接管读成「别人的代理」。
- **内核日志**：Windows 版隐藏内核的控制台，日志页直接读内核自己的输出流。
- **打开文件**：设置 > 关于的「运行配置」原来调 Finder 的 `open -R`，Windows 上并不存在。现在会在资源管理器里选中该文件，按钮文字也照实写；MCP 桥接里「打开 app」改为直接启动 exe，不再因找不到 `.app` 报错。

### 其他

- **出口地址质量**：用设置里指定的一个地址服务（默认 ip-api.com，也可换 ipwho.is / ip.sb / ipapi.co）判断出口是机房还是家宽、有没有被标记为代理，再结合节点延迟给出「优秀 / 良好 / 一般 / 较差」。默认关闭——开启后会把出口 IP 发到外面。
- **更多预设**：测速地址、出口测试地址、质量检测地址三者都是「常用地址下拉 + 文本框」，也可以自己填。
- **订阅地址列**：提供商页多一列 `host:port`，跟订阅文件里一致（Clash YAML、纯链接列表、整体 base64 都支持），按文件大小与修改时间缓存。
- **清理列表**：标签上方的橡皮擦清空当前页列出的内容。已经结束的连接一次点击即清；活跃和全部需要再点一次确认，两者都不会断开连接。
- **我的位置**：隧道稳定后会重测一次，之后每 10 分钟校正，开 TUN 后不再把节点所在国家当成你的位置。
- **地区识别**：匹配前剔除零宽字符，`H​K`、`日​本` 这类名字也能认出来。
- **节点名屏蔽规则**：名字里不显示什么由你决定——一个规则库，每条删掉节点名里匹配的片段。普通文本按字面量，两端是 / 的按正则（如 `/\[.*\]/` 删掉 `[Vless]` 这类前缀），可以加任意多条。默认一条删零宽字符（订阅往名字里塞它防复制，看不见却读出空隙）。内核拿到的名字不变，选择、规则和运行中的配置都不受影响。

## 安装

1. 从 [Releases](https://github.com/jynn77/bowel/releases) 下载 `bowel-setup-<版本>-amd64.exe`，运行安装。
2. 打开后添加一个 Provider（订阅链接、节点链接或扫码），再打开接管。

需要 Windows 10 或更新版本（amd64）。安装位置可以自己选，快捷方式带管理员权限，TUN 才能打开虚拟网卡。免安装版是同一目录下的 `bowel.exe`。

## 从源码构建

需要 Go 1.27.3+（`GOTOOLCHAIN=auto` 会自动下载），不需要 Node 或 Bun。

```bash
GOOS=windows GOARCH=amd64 ./scripts/fetch-core.sh   # 内核取到 resources/windows-amd64/bin
# 再把 wintun.dll（0.14.1 amd64，取自 wintun.net）放到同一目录 —— TUN 需要它
go tool mygo build -platform windows/amd64 -o build .
# → build/windows-amd64/bowel-setup-<版本>-amd64.exe   （NSIS 安装包）
# → build/windows-amd64/bowel.exe                      （免安装版）
```

- 工作原理与代码结构：[docs/architecture.md](docs/architecture.md)
- 设置里按 `Ctrl+F` 可以搜索；界面支持简体中文、繁體中文、English 与日本語，浅色与深色。

## 许可证

bowel 采用 [PolyForm Noncommercial License 1.0.0](LICENSE) 授权，仅限非商业用途。如需商业授权，请[提交 issue](https://github.com/21stware/bowel/issues)。

随应用打包的 mihomo 内核仍适用 GPL-3.0。应用内的 `Contents/Resources/licenses/mihomo/` 附有它的许可证，每个 Release 都附带对应版本的源码。`assets/flags/` 中的旗帜图片和 `resources/fonts/` 中的字体属于第三方文件，仍适用各自的许可证。
