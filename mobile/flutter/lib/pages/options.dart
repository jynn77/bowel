import 'dart:io' show Platform;

import 'package:flutter/widgets.dart';

import '../app/i18n.dart';
import '../app/shell.dart';
import '../app/state.dart';
import '../ui/atoms.dart';
import '../ui/skin.dart';
import 'rows.dart';
import 'sheet.dart';

/// One setting of the core, by its key in the settings file.
class _F {
  const _F.flag(this.key, this.title, {this.sub}) : kind = 'bool', choices = const [], unit = '';
  const _F.num(this.key, this.title, this.unit, {this.sub}) : kind = 'int', choices = const [];
  const _F.pick(this.key, this.title, this.choices) : sub = null, kind = 'pick', unit = '';
  const _F.text(this.key, this.title) : sub = null, kind = 'text', choices = const [], unit = '';
  const _F.lines(this.key, this.title, {this.sub}) : kind = 'list', choices = const [], unit = '';

  final String key, title, kind, unit;
  final String? sub;
  final List<(String, String)> choices;
}

List<_F> get _basic => [
  _F.pick('logLevel', tr('日志级别', 'Log level'), [('debug', 'debug'), ('info', 'info'), ('warning', 'warning'), ('error', 'error'), ('silent', 'silent')]),
  _F.text('testUrl', tr('默认测速地址', 'Default test URL')),
  _F.flag('ipv6', 'IPv6'),
  _F.flag('unifiedDelay', tr('统一延迟', 'Unified delay'), sub: tr('只算节点到目标的时间，不算握手', 'Count only node-to-target time, not the handshake')),
  _F.flag('tcpConcurrent', tr('TCP 并发', 'TCP concurrency'), sub: tr('同时连接域名的多个地址，用最快的', 'Connect to a domain\'s addresses at once and use the fastest')),
  _F.flag('disableKeepAlive', tr('关闭 TCP Keep-Alive', 'Disable TCP Keep-Alive')),
  _F.num('keepAliveInterval', tr('Keep-Alive 间隔', 'Keep-Alive interval'), tr('秒', 's')),
  _F.pick('fingerprint', tr('TLS 指纹', 'TLS fingerprint'), [('', tr('不使用', 'None')), ('chrome', 'Chrome'), ('firefox', 'Firefox'), ('safari', 'Safari'), ('ios', 'iOS'), ('android', 'Android'), ('edge', 'Edge'), ('random', tr('随机', 'Random'))]),
];

List<_F> get _auto => [
  _F.num('urlTestInterval', tr('测速间隔', 'Test interval'), tr('秒', 's'), sub: tr('自动选择节点多久测一次', 'How often automatic selection tests nodes')),
  _F.num('urlTestTolerance', tr('切换容差', 'Switch tolerance'), 'ms', sub: tr('另一个节点要快这么多才切换', 'Another node must be this much faster to switch')),
  _F.num('healthInterval', tr('Provider 检查间隔', 'Provider check interval'), tr('秒', 's')),
];

List<_F> get _dns => [
  _F.pick('dnsMode', tr('DNS 模式', 'DNS mode'), [('fake-ip', 'Fake IP'), ('redir-host', 'Redir Host')]),
  _F.lines('nameservers', tr('DNS 服务器', 'DNS servers')),
  _F.lines('defaultNameservers', tr('解析 DNS 服务器的 DNS', 'DNS for resolving DNS servers')),
  _F.lines('fallback', tr('备用 DNS', 'Fallback DNS')),
  _F.lines('proxyNameservers', tr('解析节点地址的 DNS', 'DNS for node addresses')),
  _F.lines('nameserverPolicy', tr('按域名指定 DNS', 'DNS by domain'), sub: tr('每行一条：域名=服务器', 'One per line: domain=server')),
  _F.text('fakeIpRange', tr('Fake IP 地址段', 'Fake IP range')),
  _F.lines('fakeIpFilter', tr('Fake IP 例外', 'Fake IP exceptions')),
  _F.lines('hosts', 'Hosts', sub: tr('每行一条：域名=IP', 'One per line: domain=IP')),
  _F.flag('respectRules', tr('DNS 请求也按规则走', 'Route DNS requests by rules')),
  _F.flag('useHosts', tr('使用 Hosts', 'Use Hosts')),
  _F.flag('useSystemHosts', tr('使用系统 Hosts', 'Use system Hosts')),
  _F.flag('preferH3', tr('优先 DoH/3', 'Prefer DoH/3')),
];

List<_F> get _sniff => [
  _F.flag('sniff', tr('域名嗅探', 'Domain sniffing'), sub: tr('从流量本身读出域名', 'Read the domain from the traffic itself')),
  _F.lines('sniffHttp', tr('HTTP 端口', 'HTTP ports')),
  _F.lines('sniffTls', tr('TLS 端口', 'TLS ports')),
  _F.lines('sniffQuic', tr('QUIC 端口', 'QUIC ports')),
  _F.flag('sniffOverride', tr('用嗅探结果覆盖目标', 'Override the target with the sniffed domain')),
  _F.flag('sniffForceDnsMapping', tr('嗅探 DNS 映射的连接', 'Sniff DNS-mapped connections')),
  _F.flag('sniffParsePureIp', tr('嗅探纯 IP 连接', 'Sniff IP-only connections')),
  _F.lines('sniffSkip', tr('跳过这些域名', 'Skip these domains')),
];

List<_F> get _geo => [
  _F.flag('geoAutoUpdate', tr('自动更新', 'Auto update')),
  _F.num('geoUpdateInterval', tr('更新间隔', 'Update interval'), tr('小时', 'h')),
  _F.text('geoipUrl', tr('GeoIP 地址', 'GeoIP URL')),
  _F.text('geositeUrl', tr('GeoSite 地址', 'GeoSite URL')),
  _F.text('mmdbUrl', tr('MMDB 地址', 'MMDB URL')),
];

class OptionsPage extends StatefulWidget {
  const OptionsPage({super.key, required this.app});
  final AppState app;

  @override
  State<OptionsPage> createState() => _OptionsPageState();
}

class _OptionsPageState extends State<OptionsPage> {
  Map? _s;
  bool _geoBusy = false;

  AppState get app => widget.app;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final s = await app.ask('settings');
    if (s is Map && mounted) setState(() => _s = s);
  }

  Future<void> _set(String key, Object? value) async {
    await app.send('setSettings', {'patch': {key: value}});
    await _load(); // what was saved, even when the core said no
  }

  Widget _field(Look l, _F f) {
    final s = _s!;
    final v = s[f.key];
    switch (f.kind) {
      case 'bool':
        return toggleRow(l, f.title, v == true, () => _set(f.key, v != true), sub: f.sub);
      case 'int':
        return navRow(l, f.title, '$v ${f.unit}', sub: f.sub, onTap: () => inputSheet(app, f.title, (t) {
              final n = int.tryParse(t);
              if (n != null) _set(f.key, n);
            }, initial: '$v', keyboard: TextInputType.number));
      case 'pick':
        final shown = f.choices.where((c) => c.$1 == v);
        return navRow(l, f.title, shown.isEmpty ? '$v' : shown.first.$2, sub: f.sub, onTap: () => pickSheet<String>(app, f.title, [for (final c in f.choices) PickOption(c.$1, c.$2)], (x) => _set(f.key, x), value: v as String?));
      case 'list':
        final list = ((v as List?) ?? const []).cast<String>();
        return navRow(l, f.title, list.isEmpty ? tr('无', 'None') : tr('{0} 条', '{0}', [list.length]), sub: f.sub, onTap: () => inputSheet(app, f.title, (t) => _set(f.key, [for (final x in t.split('\n')) if (x.trim().isNotEmpty) x.trim()]), initial: list.join('\n'), maxLines: 8, note: f.sub ?? tr('每行一条', 'One per line')));
      default:
        return navRow(l, f.title, '${v ?? ''}', sub: f.sub, onTap: () => inputSheet(app, f.title, (t) => _set(f.key, t), initial: '${v ?? ''}', keyboard: TextInputType.url));
    }
  }

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    if (_s == null) return SubPage(title: tr('内核选项', 'Core options'), children: []);
    final canGeo = _s!['canUpdateGeo'] == true;
    return SubPage(
      title: tr('内核选项', 'Core options'),
      actions: [
        Btn(tr('恢复默认', 'Reset'), minH: 44, kind: BtnKind.plain, onTap: () => confirmSheet(app, tr('把内核选项恢复默认', 'Reset core options'), tr('恢复', 'Reset'), () async {
              await app.send('resetCore');
              await _load();
            }, body: tr('路由组和规则不受影响。', 'Route groups and rules stay as they are.'))),
      ],
      children: [
        section(l, tr('基本', 'Basic'), top: 4, [for (final f in _basic) _field(l, f)]),
        section(l, tr('自动选择', 'Auto select'), [for (final f in _auto) _field(l, f)]),
        section(l, 'DNS', [
          for (final f in _dns) _field(l, f),
          navRow(l, tr('清除 DNS 缓存', 'Flush DNS cache'), '', sub: tr('VPN 开启时可用', 'Available while VPN is on'), onTap: () async {
            if (await app.send('flushDNS')) app.say(tr('已清除', 'Cleared'));
          }),
        ]),
        section(l, tr('域名嗅探', 'Domain sniffing'), [for (final f in _sniff) _field(l, f)]),
        if (canGeo)
          section(l, tr('Geo 数据', 'Geo data'), [
            // iOS has no core running to refresh or read MMDB: only the two lists.
            for (final f in Platform.isIOS ? _geo.where((f) => f.key == 'geoipUrl' || f.key == 'geositeUrl') : _geo) _field(l, f),
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Btn(tr('立即更新', 'Update now'), minH: 40, onTap: _geoBusy ? null : () async {
                setState(() => _geoBusy = true);
                app.say(tr('正在更新', 'Updating'), ms: 0);
                if (await app.send('updateGeo')) app.say(Platform.isIOS ? tr('已更新，下次打开 VPN 时生效', 'Updated. Takes effect the next time VPN turns on') : tr('已更新', 'Updated'));
                if (mounted) setState(() => _geoBusy = false);
              }),
            ),
          ]),
        const SizedBox(height: 18),
      ],
    );
  }
}

class AboutPage extends StatelessWidget {
  const AboutPage({super.key, required this.app});
  final AppState app;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final p = l.text(13.5, color: l.ink2, lh: 20);
    return SubPage(title: tr('关于', 'About'), children: [
      Padding(
        padding: const EdgeInsets.fromLTRB(4, 8, 4, 18),
        child: Row(children: [
          const LogoTile(size: 60),
          const SizedBox(width: 14),
          Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text('bowel', style: l.text(23, weight: 700, display: true, lh: 29)),
            Text('0.1.0', style: l.text(13.5, color: l.mute, lh: 19)),
          ]),
        ]),
      ),
      section(l, tr('版本', 'Version'), top: 4, [
        navRow(l, tr('内核', 'Core'), app.coreText),
      ]),
      section(l, tr('开源许可', 'Open source licenses'), [
        const SizedBox(height: 8),
        Text(tr('bowel 按 PolyForm Noncommercial 1.0.0 授权，可用于非商业用途。', 'bowel is licensed under PolyForm Noncommercial 1.0.0 for noncommercial use.'), style: p),
        const SizedBox(height: 8),
        Text(tr('内核 mihomo 按 GNU GPL 3.0 授权，源代码见 github.com/MetaCubeX/mihomo。', 'The mihomo core is licensed under GNU GPL 3.0. Source: github.com/MetaCubeX/mihomo.'), style: p),
        const SizedBox(height: 8),
        Text(tr('界面字体 OPPO Sans 4.0 · © OPPO 广东移动通信有限公司，依 OPPO Sans 字体许可使用，许可证随应用附带。', 'Interface font OPPO Sans 4.0 · © Guangdong OPPO Mobile Telecommunications Corp., Ltd., used under the OPPO Sans font license included with the app.'), style: p),
      ]),
    ]);
  }
}

/// Which apps stay outside the VPN, on Android.
class AppsPage extends StatefulWidget {
  const AppsPage({super.key, required this.app});
  final AppState app;

  @override
  State<AppsPage> createState() => _AppsPageState();
}

class _AppsPageState extends State<AppsPage> {
  List<({String id, String name})>? _apps;
  String _q = '';

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final list = await Shell.instance.listApps();
      if (mounted) setState(() => _apps = list);
    } catch (_) {
      if (mounted) setState(() => _apps = const []);
    }
  }

  @override
  Widget build(BuildContext context) {
    final app = widget.app;
    return ListenableBuilder(
      listenable: app,
      builder: (context, _) {
        final l = Skin.of(context);
        final out = {for (final s in app.vpnExcluded.split(',')) if (s.isNotEmpty) s};
        final q = _q.trim().toLowerCase();
        final rows = [for (final a in _apps ?? const <({String id, String name})>[]) if (q.isEmpty || a.name.toLowerCase().contains(q) || a.id.contains(q)) a];
        return SubPage(
          title: tr('分应用代理', 'Per-app proxy'),
          dockChild: SearchField(hint: tr('搜索 App', 'Search apps'), label: tr('搜索 App', 'Search apps'), onChanged: (v) => setState(() => _q = v)),
          children: [
            Padding(padding: const EdgeInsets.fromLTRB(16, 4, 16, 6), child: Text(tr('关掉的 App 不走 bowel，直接联网。下次开启 VPN 时生效。', 'Apps turned off skip bowel and connect directly. Applies the next time VPN turns on.'), style: l.text(13, color: l.mute, lh: 18))),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Column(children: [
                for (final a in rows)
                  toggleRow(l, a.name, !out.contains(a.id), () {
                    final next = {...out};
                    next.contains(a.id) ? next.remove(a.id) : next.add(a.id);
                    app.setExcluded(next);
                  }, sub: a.id),
              ]),
            ),
          ],
        );
      },
    );
  }
}
