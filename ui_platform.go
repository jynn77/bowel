package main

// What the interface says differently on each system. Most of it says
// the same everywhere, and i18n turns "Mac" into the computer; these are
// the few lines about something only one system has.

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/oboo/moho/internal/devtools"
	"github.com/oboo/moho/internal/gateway"
	"github.com/oboo/moho/internal/i18n"
)

// gatewaySupported says whether this system can be made a gateway, and
// so whether the interface offers it.
func gatewaySupported() bool { return gateway.Supported() }

// tunInstallHint is TUN's status before its one authorization, which
// each system asks for in its own dialog.
func tunInstallHint() string { return tunInstallHintOn(runtime.GOOS) }

func tunInstallHintOn(goos string) string {
	switch goos {
	case "darwin":
		return i18n.T("未使用 · 第一次开启时，macOS 会请求管理员授权", "Not in use · The first time it's turned on, macOS asks for an administrator's authorization")
	case "windows":
		return i18n.T("未使用 · 第一次开启时，Windows 会弹出用户账户控制，请求管理员授权", "Not in use · The first time it's turned on, Windows asks for an administrator's authorization in a User Account Control prompt")
	}
	return i18n.T("未使用 · 第一次开启时，系统会请求管理员授权", "Not in use · The first time it's turned on, the system asks for an administrator's authorization")
}

// firewallTip is the gateway's warning that the system's firewall is on.
func firewallTip() string { return firewallTipOn(runtime.GOOS) }

func firewallTipOn(goos string) string {
	if goos == "darwin" {
		return i18n.T("macOS 防火墙开着：如果其他设备查不到域名，在「系统设置 → 网络 → 防火墙」里允许 mihomo 接收连接。",
			"The macOS firewall is on: if other devices can't look up names, allow mihomo incoming connections in System Settings → Network → Firewall.")
	}
	return i18n.T("防火墙（ufw 或 firewalld）开着：如果其他设备查不到域名或上不了网，放行 53 端口，并允许转发来自局域网的流量。",
		"A firewall (ufw or firewalld) is on: if other devices can't look up names or reach the network, open port 53 and allow traffic from the LAN to be forwarded.")
}

// toolSubOf is the line under a tool's name. The terminal's is its
// shell, which is the one whose file bowel reads.
func toolSubOf(t devtools.Tool) string {
	if t.ID != devtools.Shell {
		return toolSub(t.ID)
	}
	switch filepath.Base(strings.TrimPrefix(t.File, "~/")) {
	case ".bashrc":
		return "bash"
	case ".zshenv":
		return "zsh"
	}
	return ""
}
