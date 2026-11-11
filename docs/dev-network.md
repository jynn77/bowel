# 开发者网络

> 目标：装了 bowel 之后，本地开发、局域网、容器、Tailscale 和公司 VPN 照常工作，用户不用手写规则。
> 实现在 `internal/devnet`，接入点是 `core.BuildConfig` 和 `app_devnet.go`，设置入口是 设置 > 开发者网络。

## 1. 问题的根源

这类流量出问题，很少是因为规则写少了。系统本来知道内网域名该问哪个 DNS、`100.x` 该走哪张网卡。代理接管之后，mihomo 用自己的 DNS 和默认网卡把这件事重做了一遍，系统的知识就丢了。

所以原则是三条：

1. **系统已有的路径优先。** 能不进 mihomo 就不进，进了也要走回系统原来的路径。
2. **从本机状态推导，不靠用户配置。** 静态目录只放“放之四海皆准”的部分，Tailscale、VPN、分流 DNS 从系统实时读取。
3. **不改用户保存的设置。** 叠加层在运行时合并进配置，所以老用户不需要迁移，关掉开关就完全回到原样。

## 2. 流量进入 mihomo 的四条路径

每个场景都要在这四条路径上成立，一条路径修好不算修好。

| 路径 | 谁会走 | 决定权在哪 | 典型的坑 |
|---|---|---|---|
| A 系统代理 | 浏览器、大部分 GUI 应用 | 系统的绕过列表，然后是 mihomo 规则 | 绕过列表的网段只匹配 IP 字面量，域名照样进 mihomo |
| B 显式代理 | `HTTP_PROXY`、`curl -x`、IDE 代理设置 | `NO_PROXY`，然后是 mihomo 规则 | 没有 `NO_PROXY` 时什么都进 mihomo |
| C TUN | 所有不走代理的进程 | 系统路由表，然后是 mihomo 规则 | DNS 被劫持，DIRECT 被绑到默认网卡 |
| D 全局/直连模式 | 上面任意路径叠加 | mihomo 不看规则 | 局域网和 Tailscale 也被送进节点 |

## 3. 场景枚举

`目录` 是静态表，`探测` 是运行时读取。每一项在 设置 > 开发者网络 里都能单独关闭。

| ID | 场景 | 内容 | 来源 |
|---|---|---|---|
| `loopback` | 本机回环 | `localhost`、`*.localhost`、`127.0.0.0/8`、`::1` | 目录 |
| `lan` | 局域网地址 | RFC1918、链路本地、组播、`fc00::/7`、`fe80::/10` | 目录 |
| `lan-names` | 局域网域名 | `.local`、`.lan`、`.home`、`.home.arpa`、`.localdomain`、`.intranet`、`.corp` | 目录 |
| `dev-tld` | 本地开发域名 | `.test`、`.example`、`.invalid`（Valet、puma-dev、dnsmasq） | 目录 |
| `containers` | 容器和虚拟机 | `.internal`（`host.docker.internal`）、`.svc`、`.cluster`、`.docker` | 目录 |
| `rebind` | 指向本机的公共域名 | `nip.io`、`sslip.io`、`lvh.me`、`localtest.me`、`traefik.me`、`vcap.me`、`localho.st` | 目录 |
| `mesh` | Tailscale 和组网 | `ts.net`、`beta.tailscale.net`、`100.64.0.0/10`、`fd7a:115c:a1e0::/48`，加第 4 节的探测结果 | 目录 + 探测 |
| `vpn` | 其他 VPN 和分流 DNS | 隧道网卡上的路由、`scutil --dns` 里的分流域名（含 `/etc/resolver/*`） | 探测 |

用户还可以加自己的内网后缀或网段（`devNetExtra`）。一条就同时生效于绕过列表、fake-ip 过滤、规则和 TUN 排除，不用分别配置四处。

### 特意不放进目录的

- **`.dev`、`.app`**：是真实的公共顶级域。
- **`tailscale.com`**：Tailscale 自己的 socket 绑物理网卡，不经过 bowel。浏览器打开登录页时，有的网络必须走代理才稳，强制直连会变差。
- **按进程分流**：macOS 27 隐藏了 socket 列表，`PROCESS-NAME` 不可靠，所以全部按地址和域名判断。

## 4. Tailscale 全覆盖

| 情况 | 探测来源 | 处理 |
|---|---|---|
| 未安装、已停止 | `tailscale status --json` | 只留目录项，`ts.net` 走 DIRECT |
| 运行中 | 状态 + 接口地址匹配出 `utunN` | 生成绑定该网卡的直连出站 `Tailscale-net` |
| 对端 IP（`100.x`、`fd7a:`） | 目录 | 规则走 `Tailscale-net`，TUN 排除 |
| MagicDNS 全名 | 状态里的 suffix | fake-ip 过滤，DNS 分流到 `100.100.100.100#Tailscale-net`，规则走 `Tailscale-net` |
| 短主机名（`ssh mybox`、`http://mybox/`） | 状态里的 peers | 写入 hosts（有 IPv4 就只写 IPv4），加 `DOMAIN` 规则和绕过项 |
| Headscale 或自定义域名 | 状态里的 suffix | 同 MagicDNS，不写死 `ts.net` |
| Split DNS（管理员配的内网域名） | `scutil --dns` | 对应域名分流，出站绑 Tailscale |
| 子网路由（subnet router） | 路由表上 Tailscale 网卡的非主机路由 | `IP-CIDR` 走 `Tailscale-net`。只认路由表，没开 accept-routes 时不会误判 |
| 出口节点（exit node） | 状态 | 和 TUN 抢默认路由，不做兼容，在设置页明确警告 |
| `100.64/10` 的系统路由被别的工具占走 | 路由表上非 scoped 的路由不在 Tailscale 网卡 | 名字和 IPv4 段不进绕过列表，也不排除出 TUN，全部交给 `Tailscale-net` |
| 网卡号变化、重启、切网络 | 每 20 秒探测 | 结果变了才重载配置 |
| CLI 不可用 | `utun` 上有 CGNAT 地址，且 DNS 指向 `100.100.100.100` | 按运行中处理，只是没有 peers |

**为什么需要绑定网卡的出站。** TUN 打开后，mihomo 的 DIRECT 只在目标落在某张网卡的网段内时才选那张网卡，否则绑默认网卡。Tailscale 的地址是 /32，不包含对端，于是 DIRECT 会从 en0 出去。这只影响已经进了 mihomo 的流量，比如 TUN 和系统代理同时开、或者 `curl -x`。

**hosts 只写一个地址族。** mihomo 对 hosts 里的多个地址随机选一个，选中后不会再试另一个。对端同时有 IPv4 和 IPv6 时，只监听 IPv4 的服务会有一半概率失败。所以 hosts 优先写 IPv4。MagicDNS 打开时全名不写 hosts，由 `100.100.100.100` 答两个地址族，mihomo 会两个都试，刚加入的机器也能解析。

**路由被占走的情况。** 另一个代理工具可能把 `100.64/10` 指向物理网关，这时 Tailscale 的路由只剩 scoped 版本，不绑网卡的连接会从 en0 出去然后超时。`Tailscale-net` 绑了网卡，不受影响，所以这种情况下要让流量进 mihomo，而不是绕过它。

**TUN 路径本身。** 把 CGNAT 段排除在 TUN 路由之外后，`100.x` 完全由 Tailscale 自己的路由接管。DNS 劫持也碰不到 `100.100.100.100`。

## 5. TUN 排除

下列网段不进 TUN：`10/8`、`172.16/12`、`192.168/16`、`169.254/16`、`100.64/10`、组播、`fc00::/7`、`fe80::/10`、`fd7a:115c:a1e0::/48`。

好处是这些流量全程不经过 mihomo，于是 `dig @10.0.0.1` 不会被 DNS 劫持骗，`ping`、mDNS、打印机都恢复正常。

有一个例外：如果用户自己写了一条规则，把这些网段里的地址送到非直连路由（比如通过节点访问公司网段），对应网段就不排除，用户的意图优先。

## 6. 全局和直连模式

mihomo 在 global 和 direct 模式下不看规则。开发者网络开启时，bowel 给内核的实际模式是 `rule`，规则只有叠加层加一条 `MATCH,GLOBAL`（或 `MATCH,DIRECT`）。界面上仍然是全局模式，语义不变，只是局域网和 Tailscale 不再被送进节点。

切换模式时原来会断开全部连接。现在叠加层命中的连接会保留，因为切换前后它们的路径没变，SSH、WebSocket 和数据库连接不用重连。

## 7. 优先级

1. 用户规则里落在叠加层范围内的（比如把 `x.foo.ts.net` 指给节点）
2. 叠加层：完整域名，后缀从长到短，网段从长到短
3. 其余用户规则和模板规则，按原来的排序

DNS 分流、hosts、fake-ip 过滤都是用户自己写的优先。

## 8. DNS 的两种状态

- **TUN 或用户开了 DNS**：分流合并进现有的 DNS 配置。`.lan`、`.local`、`.corp`、`.test`、`.internal` 这类后缀改问系统的 DNS，否则公共 DoH 会把 `nas.lan` 答成不存在。
- **只有系统代理，DNS 关闭**：mihomo 原本用系统解析，但它是纯 Go 解析器，不认 macOS 的分流配置。只要探测到分流域名，就打开一个最小 DNS 块，默认仍是 `system`，只多了分流。没有分流时不打开，行为和以前完全一样。

## 9. 自动探测的边界

- 其他代理软件的 TUN（路由里有默认路由或 `128.0/1`）不算 VPN，不绑定，设置页会提示有冲突。
- 只有 IPv6 链路本地地址的 `utun`（iCloud 中继、接力）会被过滤掉。
- 探测失败或生成的配置被 mihomo 拒绝时，退回只用静态目录，不会把网络弄断。
- Windows 上目录和 Tailscale 探测可用，系统路由和分流 DNS 探测只实现了 macOS。
- 移动端不走这套配置。

## 10. 验证状态

单元测试覆盖目录、解析器（用真实格式的样本）和配置生成，集成测试用真实 mihomo 校验生成的配置。

绑定网卡的出站、经它的 DNS 分流、短主机名 hosts、`MATCH,GLOBAL` 已经用打包的 mihomo 实测。

2026-11-09 用真实的 Tailscale 1.104.5（macsys）实测，当时另一个代理工具占着默认路由和 `100.64/10`：

- 探测结果正确，约 300 毫秒。Tailscale 关闭再打开，探测能跟上。
- 经真实 mihomo 的系统代理路径，rule、global、direct 三种模式下，对端的 IPv4、IPv6、MagicDNS 全名、短主机名和探测之后才出现的机器都能访问，覆盖局域网直连和中继两种连接，每个目标 8 次全部成功，都走 `Tailscale-net`。局域网地址走 DIRECT。
- 带真实探测结果的配置通过 `mihomo -t`，包含 TUN 配置。
- 另一个 TUN 占着默认路由时，Tailscale 上报的公网端点是物理网络的地址，而不是该 TUN 出口的地址，说明它自身的 WireGuard 流量没有进那个 TUN。

同一天退出那个工具后，在开发版里打开 bowel 自己的 TUN（rule 模式）再测一遍：

- `route-exclude-address` 按预期生效：`100.64/10` 仍在 Tailscale 网卡上，局域网走 en0，公网进 TUN。
- 不经代理的程序访问对端，IPv4、IPv6、MagicDNS 全名、短主机名每种 8 次全部成功，这些连接都没有进 mihomo，`ping` 也正常。经 17899 显式代理的同样全部成功，都走 `Tailscale-net`。
- Tailscale 自身的流量不进 TUN。到公网对端（`118.89.152.130:41641`）能建立直连，内核日志里没有 41641、STUN 和对端中继地址的连接。
- `dig @8.8.8.8` 被劫持后仍能解析 MagicDNS 全名和短主机名，公网访问正常。
- Tailscale 在运行中关闭、打开，配置在 20 秒内跟着变化，mihomo 进程和 TUN 都不重建。

没有单独在 TUN 下测试 global 和 direct 模式，这两种模式的规则已在系统代理路径上验证。

Tailscale 会把 TUN 的 `198.18.0.1:41641` 也列为自己的端点，实测没有影响连接。

## 11. 开发工具

上面各节解决“进了 bowel 的流量别被弄坏”。开发工具解决反过来的问题：很多工具不读系统代理，流量根本没进 bowel。实现在 `internal/devtools`，入口在 模式 卡片。

| 工具 | 写到哪里 | bowel 不运行时 |
|---|---|---|
| Colima 的 dockerd | `colima.yaml` 的 `docker.proxies`，要重启 Colima | `docker pull` 失败 |
| 容器和 `docker build` | `~/.docker/config.json` 的 `proxies` | 新容器和构建失败 |
| zsh | `~/.zshenv`，先检测端口 | 新开的终端不设代理 |
| Git SSH | `~/.ssh/config` 顶部，`Match exec` 检测端口 | 直连 |
| Git HTTPS（改直连） | `~/.gitconfig` | 不受影响 |

**撤销就是删除。** 写入的内容夹在两行注释之间，撤销时整段删除，不备份原值。JSON 没有注释，Colima 启动时可能重写自己的文件，这两项按键名删除，`written.json` 记录它们是 bowel 写的，用户自己配的不碰。

**退出。** 面板的“退出”和面板里的 ⌘Q 会列出会连不上的工具。系统发起的退出（比如注销）不能拦，不提醒。bowel 打不开时用 `bowel devtools off` 撤销全部。

**实测。** 2026-11-09 在真实 Colima 0.10.3 上跑过：写入并重启后，`docker pull`、容器内的 `apt-get` 和 `docker build` 都经过 bowel；撤销并重启后代理消失。Colima 启动时会删掉 `colima.yaml` 里的注释，这就是按键名删除的原因。

`brew services restart` 对 Colima 不可靠：它只结束外层进程，VM 可能留下，下一个进程发现“已在运行”就退出，配置不会重新读取。所以 bowel 按顺序停服务、`colima stop`、再启动服务，然后等 daemon 的代理和文件一致。

## 12. 下载源

开发工具一节管“流量进不进 bowel”。下载源管另一件事：包从哪里下。npm、PyPI、Go、Rust 本来就是 开发 路由组里的内置服务，走哪个节点由路由决定；规则说不了的只有“改用镜像”，那是工具自己的一行配置。实现在 `internal/devtools/sources.go`，入口在 模式 卡片展开后的 依赖源，排在 开发工具 下面。

| 工具 | 改哪一行 | 镜像 |
|---|---|---|
| npm | `~/.npmrc` 的 `registry=` | npmmirror、腾讯云、华为云 |
| pip | `pip.conf` 的 `index-url`（放进已有的 `[global]`） | 清华、阿里云、中科大 |
| Go | Go 的 `env` 文件里的 `GOPROXY=<镜像>,direct`，也就是 `go env -w` 写的那个 | goproxy.cn、goproxy.io、阿里云 |
| Cargo | `~/.cargo/config.toml` 的 `[source.crates-io]` 和一个 `bowel-mirror` 源 | rsproxy、中科大、清华 |

**只改一行。** 点 用这个 就地改那一行，文件里别的内容不动；用户自己已经写了这一项的，在原来的位置、按原来的名字改。换回官方源是删掉这一行，文件因此空了就删掉文件。只有上表列出的镜像地址能写进去。

**实测。** 镜像直接连；官方源经 bowel 自己的端口连，所以测到的就是它的服务此刻走的那条路。内核没运行时不测官方源。每种走法最多试三次，有一次通就算通：同一个镜像连续请求，偶尔会丢一次。npm 请求的是一个小包的页面，不用 `/-/ping`，npmmirror 不应答那个地址。

**修复。** 正在用的那种走法三次都不通时，模式 卡片上出现一行，并给出一个修复，按这个顺序选：

1. 官方源不通，而且它的服务被指到了 直连：把这个服务放回默认的路由组。
2. 换成列表里能通的最快的镜像。
3. 正在用镜像而官方源能通：换回官方源。

都不通时不给修复。用户自己的私有源连不上时，只会建议换到列表里的镜像，不会建议别的地址。

**实测记录。** 2026-11-10 在一台 Mac 上对真实的 Go 和 pip 配置换到镜像再换回：`go env GOPROXY` 和 `pip3 config get global.index-url` 读到的值都跟着变，结束后两个文件都不存在，和开始前一样。npm 和 Cargo 那台机器上没有安装，只有文件读写的测试。Windows 和 Linux 上的路径按各自的位置写了，没有实机验证。
