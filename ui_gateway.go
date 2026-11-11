package main

// Gateway mode in the interface: its switch, and the guide that tells the
// other devices what to fill in. Both windows show them: the settings'
// 入站 page under 局域网, and the home board's takeover.

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/i18n"
)

// gatewayUI is the switch's flick while the app applies it.
type gatewayUI struct {
	want     bool
	flipping bool
}

// gatewayShownOn is the switch's position: the user's flick while it is
// applied, the saved choice otherwise.
func (u *UI) gatewayShownOn() bool {
	if u.gw.flipping {
		return u.gw.want
	}
	return u.snap.Takeover.Gateway.Wanted
}

// setGateway flicks the switch; failures toast in the window it was
// flicked in.
func (u *UI) setGateway(on bool, toast func(string)) {
	u.gw.want, u.gw.flipping = on, true
	if u.app == nil { // a test's UI
		u.snap.Settings.Gateway, u.snap.Takeover.Gateway.Wanted = on, on
		u.gw.flipping = false
		return
	}
	go func() {
		err := u.app.setGateway(on)
		u.settingsUpdate(func() { u.gw.flipping = false })
		if err != nil {
			toast(err.Error())
		}
	}()
}

// gatewaySwitch is the switch, for a window whose toasts are toast.
func (u *UI) gatewaySwitch(c *ui.Context, key string, toast func(string)) {
	on := u.gatewayShownOn()
	if ui.Switch(c.Key(key), &on).Label(i18n.T("网关模式", "Gateway mode")).Disabled(u.gw.flipping).Changed() {
		u.setGateway(on, toast)
	}
}

// gatewayStatus is the gateway's state as a dot and a few words.
func (u *UI) gatewayStatus(p *pal) (ui.Color, string) {
	g := u.snap.Takeover.Gateway
	switch {
	case u.gw.flipping && u.gw.want && (!g.Ready || u.snap.Takeover.Tun.Status == "install"):
		return p.warn, i18n.T("请在系统弹窗中确认…", "Confirm in the system dialog…")
	case u.gw.flipping || u.pending("gateway"):
		return p.faint, i18n.T("正在应用…", "Applying…")
	case !g.Wanted && !g.Ready:
		return p.faint, i18n.T("未使用 · 首次开启需要授权一次", "Not in use · Needs authorization the first time")
	case !g.Wanted:
		return p.faint, i18n.T("未使用", "Not in use")
	case g.Status == "checking":
		return p.faint, i18n.T("正在检查…", "Checking…")
	case g.Status == "on" && g.Devices > 0:
		return p.good, fmt.Sprintf(i18n.T("生效中 · %d 台设备在用", "Active · %[1]d device(s) using it"), g.Devices)
	case g.Status == "on":
		return p.good, i18n.T("生效中 · 还没有设备在用", "Active · No devices using it yet")
	case g.Status == "error":
		return p.signal, g.Detail
	}
	return p.warn, g.Detail // paused, install
}

// gatewayDesc says what gateway mode is, beside its switch.
func gatewayDesc() string {
	return i18n.T("让手机、电视、游戏机把这台 Mac 当网关，按 bowel 的规则上网。会同时打开 TUN",
		"Phones, TVs and consoles use this Mac as their gateway and follow bowel's rules. Turns TUN on too")
}

// gatewayGuide is what to do on the other devices, how it stands and
// what to look out for. Shown while the gateway is wanted.
func (u *UI) gatewayGuide(c *ui.Context, p *pal, toast func(string)) {
	g := u.snap.Takeover.Gateway
	ui.Column(c.Key("gw-guide")).Gap(12).Padding(14, 16).Radius(p.r(10)).Background(p.card).Border(1, p.hair).Children(func() {
		dot, status := u.gatewayStatus(p)
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(7, 7).Radius(p.r(3.5)).Background(dot).Shrink(0)
			col := p.ink
			if dot == p.signal {
				col = p.signal
			}
			ui.Text(c, status).FontSize(12.5).FontWeight(600).TextColor(col).Grow(1).Shrink(1).MinWidth(0)
		})

		if g.IP == "" {
			ui.Text(c, i18n.T("这台 Mac 还没有局域网地址：先连上 Wi‑Fi 或网线。", "This Mac has no LAN address yet: connect it to Wi‑Fi or a cable first.")).FontSize(12).TextColor(p.mute)
		} else {
			ui.Text(c, i18n.T("在其他设备上填这两项：", "On the other devices, fill in these two:")).FontSize(12).TextColor(p.mute)
			ui.Column(c).Gap(6).Children(func() {
				u.gatewayAddr(c, p, "gw-copy-gateway", i18n.T("网关", "Gateway"), g.IP, toast)
				u.gatewayAddr(c, p, "gw-copy-dns", "DNS", g.IP, toast)
			})
			gatewayHowTo(c, p, g.IP, g.Router)
		}

		// What was measured.
		ui.Row(c).Wrap().GapX(14).GapY(4).Children(func() {
			for _, ck := range []struct {
				label string
				ok    bool
			}{
				{i18n.T("IP 转发", "IP forwarding"), g.Forwarding},
				{i18n.T("DNS 53 端口", "DNS on port 53"), g.DNS},
				{"TUN", g.Tun},
				{i18n.T("阻止休眠", "Kept awake"), g.Awake},
			} {
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					mark, col := "✓", p.good
					if !ck.ok {
						mark, col = "×", p.faint
						if g.Status == "error" || g.Status == "on" {
							col = p.signal
						}
					}
					ui.Text(c, mark).FontSize(12).FontWeight(700).TextColor(col)
					ui.Text(c, ck.label).FontSize(11.5).TextColor(p.ink2).NoWrap()
				})
			}
		})

		// What to look out for.
		ui.Column(c).Gap(5).Children(func() {
			if g.IP != "" && g.DHCP {
				gatewayTip(c, p, p.warn, i18n.T("这台 Mac 的地址是路由器自动分配的，可能会变：在路由器里给它绑定固定地址（DHCP 静态分配）。",
					"This Mac's address was assigned by the router and may change: reserve it for this Mac in the router (a DHCP reservation)."))
			}
			if g.WiFi {
				gatewayTip(c, p, p.warn, i18n.T("这台 Mac 走的是 Wi‑Fi：其他设备的流量都要经过它，改用网线会更快更稳。",
					"This Mac is on Wi‑Fi: every device's traffic goes through it, so a cable is faster and steadier."))
			}
			if g.Firewall {
				gatewayTip(c, p, p.warn, firewallTip())
			}
			gatewayTip(c, p, p.faint, i18n.T("网关开着时 bowel 会阻止 Mac 自动休眠，因为它一睡，这些设备就断网了。合上盖子仍会睡眠。",
				"While the gateway is on, bowel keeps the Mac from sleeping, since the devices lose the network when it sleeps. Closing the lid still puts it to sleep."))
			gatewayTip(c, p, p.faint, i18n.T("Mac 睡眠、关机或 bowel 退出后，这些设备就上不了网，要把网关和 DNS 改回去。",
				"When the Mac sleeps, shuts down or bowel quits, these devices lose the network until their gateway and DNS are set back."))
			gatewayTip(c, p, p.faint, i18n.T("最好在这些设备上关掉 IPv6：IPv6 流量不经过这台 Mac。",
				"Turn IPv6 off on these devices if you can: IPv6 traffic doesn't go through this Mac."))
		})
	})
}

// gatewayAddr is one address to fill in, with its copy button.
func (u *UI) gatewayAddr(c *ui.Context, p *pal, key, label, addr string, toast func(string)) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(p.mute).Width(84).Shrink(0).NoWrap()
		ui.Text(c, addr).Font(actMono).FontSize(17).FontWeight(600).TextColor(p.ink).Selectable().Grow(1).MinWidth(0)
		btn(c.Key(key), p, i18n.T("复制", "Copy")).Shrink(0).OnClick(func() {
			c.WriteClipboard(addr)
			toast(fmt.Sprintf(i18n.T("已复制 %s", "Copied %s"), addr))
		})
	})
}

// gatewayHowTo is where the two addresses go on each kind of device.
func gatewayHowTo(c *ui.Context, p *pal, ip, router string) {
	routerAt := i18n.T("路由器的管理页", "the router's admin page")
	if router != "" {
		routerAt = fmt.Sprintf(i18n.T("路由器的管理页（%s）", "the router's admin page (%s)"), router)
	}
	rows := [][2]string{
		{i18n.T("路由器（推荐）", "Router (best)"), fmt.Sprintf(i18n.T("在%[1]s的 DHCP 设置里，把网关和 DNS 都改成 %[2]s。全家设备重连 Wi‑Fi 后生效；有的路由器不能改网关，就在设备上逐个改。",
			"In %[1]s, set both the gateway and DNS of its DHCP settings to %[2]s. Every device picks it up when it reconnects; if the router can't change the gateway, set each device instead."), routerAt, ip)},
		{"iPhone / iPad", fmt.Sprintf(i18n.T("设置 → 无线局域网 → 当前网络的 ⓘ → 配置 IP 选「手动」，IP 地址填同网段一个没人用的，子网掩码一般是 255.255.255.0，路由器填 %[1]s；配置 DNS 选「手动」，只留 %[1]s。",
			"Settings → Wi‑Fi → ⓘ of the network → Configure IP: Manual, with an unused address of the same network, subnet mask usually 255.255.255.0 and Router %[1]s; Configure DNS: Manual, with only %[1]s."), ip)},
		{"Android", fmt.Sprintf(i18n.T("设置 → WLAN → 当前网络 → 修改 → 高级选项 → IP 设置选「静态」，网关和 DNS 1 都填 %[1]s，IP 地址填同网段一个没人用的。",
			"Settings → Wi‑Fi → the network → Modify → Advanced → IP settings: Static, with Gateway and DNS 1 both %[1]s and an unused address of the same network."), ip)},
	}
	ui.Column(c).Gap(7).Padding(10, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.hair2).Children(func() {
		for _, r := range rows {
			ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
				ui.Text(c, r[0]).FontSize(11.5).FontWeight(600).TextColor(p.ink).Width(104).Shrink(0)
				ui.Text(c, r[1]).FontSize(11.5).LineHeight(1.45).TextColor(p.ink2).Grow(1).Basis(0).MinWidth(0)
			})
		}
	})
}

func gatewayTip(c *ui.Context, p *pal, dot ui.Color, text string) {
	ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Box(c).Size(5, 5).Radius(p.r(2.5)).Background(dot).Margin(6, 0, 0, 0).Shrink(0)
		ui.Text(c, text).FontSize(11.5).LineHeight(1.45).TextColor(p.mute).Grow(1).Basis(0).MinWidth(0)
	})
}
