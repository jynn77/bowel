import 'i18n.dart';
// What the pages draw. These start as the sample values the desktop's site
// shots use; once the core is attached, AppState replaces them with real ones.

class Node {
  const Node(this.name, this.flag, this.city, this.source, this.proto, this.provider, this.delay, this.history, {this.providerId = '', this.lat, this.lon});

  final String name, flag, city, source, proto, provider;

  /// Which Provider this is from. Two Providers can share a name.
  final String providerId;

  /// Where it exits, when that is known in degrees.
  final double? lat, lon;

  /// Milliseconds; 0 means the last test timed out, -1 that there was none.
  final int delay;
  final List<int> history;
}

/// The route of each of a traffic column's parts, in order.
var trafficRoutes = const <String>['default', 'ai', 'media', 'dev', 'direct'];

class Conn {
  const Conn({
    this.id = '',
    required this.host,
    required this.app,
    required this.route,
    required this.routeName,
    required this.node,
    required this.rate,
    required this.dur,
    required this.status,
    required this.rule,
    required this.ip,
    required this.net,
    required this.port,
    required this.total,
    this.err,
    this.speed = 0,
    this.bytes = 0,
    this.ruleType = '',
    this.payload = '',
  });

  final String id, host, app, route, routeName, node, rate, dur, status, rule, ip, net, total, ruleType, payload;
  final int port, speed, bytes;
  final String? err;
}

class ProviderInfo {
  const ProviderInfo({
    this.id = '',
    required this.name,
    required this.kind,
    this.long = false,
    this.nodes = 0,
    this.used,
    this.usage,
    this.expires,
    required this.updated,
    this.tones = const [],
    this.warn,
    this.enabled = true,
    this.autoUpdate = 0,
    this.source = '',
    this.usedBy = const [],
    this.subscription = false,
    this.shareable = true,
  });

  final String id, name, kind, updated, source;
  final bool long, enabled, subscription, shareable;
  final int nodes, autoUpdate;
  final List<String> usedBy;
  final int? used;
  final String? usage, expires, warn;
  final List<int> tones;
}

/// A region's name in the interface's language; `regions` stays the Chinese
/// the sample data is built from.
String regionName(String code) => switch (code) {
      'hk' => tr('香港', 'Hong Kong'),
      'jp' => tr('日本', 'Japan'),
      'us' => tr('美国', 'United States'),
      'sg' => tr('新加坡', 'Singapore'),
      'tw' => tr('台湾', 'Taiwan'),
      'kr' => tr('韩国', 'South Korea'),
      'gb' => tr('英国', 'United Kingdom'),
      'de' => tr('德国', 'Germany'),
      'fr' => tr('法国', 'France'),
      'ca' => tr('加拿大', 'Canada'),
      'au' => tr('澳大利亚', 'Australia'),
      'nl' => tr('荷兰', 'Netherlands'),
      '' => tr('未识别', 'Unidentified'),
      _ => code.toUpperCase(),
    };

const regions = {
  'hk': '香港',
  'jp': '日本',
  'us': '美国',
  'sg': '新加坡',
  'tw': '台湾',
  'kr': '韩国',
  'gb': '英国',
  'de': '德国',
  'fr': '法国',
  'ca': '加拿大',
  'au': '澳大利亚',
  'nl': '荷兰',
  '': '未识别',
};

List<int> _h(int v) => List.filled(10, v);

List<Node> nodes = _buildNodes();

List<Node> _buildNodes() {
  final out = <Node>[
    const Node('香港 03 IPLC', 'hk', '香港', '实测', 'Shadowsocks', '机场 A', 41, [9, 10, 9, 11, 10, 9, 10, 9, 10, 10]),
    const Node('香港 02', 'hk', '香港', '实测', 'Trojan', '机场 A', 64, [5, 6, 4, 5, 3, 6, 5, 5, 6, 5]),
    const Node('香港 01', 'hk', '香港', '按名称', 'Trojan', '机场 B', 72, [3, 4, 3, 4, 3, 3, 4, 3, 4, 4]),
    Node('东京 01', 'jp', '东京', '实测', 'VMess', '机场 A', 118, _h(3)),
    Node('新加坡 01', 'sg', '新加坡', '实测', 'Shadowsocks', '机场 A', 143, _h(3)),
    Node('洛杉矶 VPS', 'us', '洛杉矶', '实测', 'Hysteria2', '自建', 168, _h(3)),
    Node('Relay-7 专线', '', '', '未知', 'VLESS', '朋友分享的专线', 212, _h(3)),
    Node('东京 02', 'jp', '东京', '按名称', 'VMess', '机场 B', 0, _h(2)),
  ];

  var seed = 20261109;
  int rnd(int a, int b) {
    seed = (seed * 1664525 + 1013904223) & 0xFFFFFFFF;
    return a + ((seed >>> 8) % (b - a + 1));
  }

  String nn(int i) => i.toString().padLeft(2, '0');
  void add(String p, String f, String name, String proto, int base, int spread) {
    final d = rnd(1, 100) <= 13 ? 0 : base + rnd(0, spread);
    final source = rnd(1, 10) > 7 ? '按名称' : '实测';
    out.add(Node(name, f, regions[f]!, source, proto, p, d, List.generate(10, (_) => rnd(2, 9))));
  }

  const protos = ['Shadowsocks', 'Trojan', 'VMess', 'Hysteria2'];
  const c = [
    ('hk', 64, ['IEPL', 'IPLC', 'BGP', 'HKT 家宽'], 30, 90),
    ('us', 52, ['洛杉矶 CN2', '圣何塞 BGP', '西雅图', '纽约'], 150, 170),
    ('jp', 48, ['东京 IEPL', '东京 BGP', '大阪 BGP'], 70, 110),
    ('sg', 36, ['IEPL', 'BGP'], 85, 110),
    ('tw', 28, ['Hinet', 'IEPL'], 60, 90),
    ('kr', 18, ['首尔 BGP'], 75, 80),
    ('gb', 10, ['伦敦'], 220, 90),
    ('de', 10, ['法兰克福'], 210, 90),
    ('fr', 6, ['巴黎'], 230, 80),
    ('ca', 6, ['多伦多'], 190, 80),
    ('au', 4, ['悉尼'], 180, 90),
    ('nl', 4, ['阿姆斯特丹'], 225, 80),
  ];
  for (final (f, count, lines, base, spread) in c) {
    for (var i = 1; i <= count; i++) {
      final mark = i % 5 == 0 ? ' x1.5' : (i % 11 == 0 ? ' x2' : '');
      add('机场 C', f, '${regions[f]} ${lines[i % lines.length]} ${nn(i)}$mark', protos[i % protos.length], base, spread);
    }
  }
  const d = [
    ('hk', 46, 'HK-HKT', 30, 100),
    ('jp', 32, 'JP-Tokyo-NTT', 75, 110),
    ('us', 30, 'US-LosAngeles', 155, 170),
    ('sg', 18, 'SG-Equinix', 90, 110),
    ('tw', 10, 'TW-Hinet', 60, 90),
    ('kr', 6, 'KR-Seoul', 80, 80),
    ('gb', 2, 'UK-London', 225, 80),
    ('de', 2, 'DE-Frankfurt', 215, 80),
  ];
  for (final (f, count, stem, base, spread) in d) {
    for (var i = 1; i <= count; i++) {
      add('机场 D', f, '$stem-${nn(i)}', 'Trojan', base, spread);
    }
  }
  // subscriptions often smuggle their account info in as fake nodes
  for (final n in const ['剩余流量：110.0 GB', '套餐到期：2027-01-12']) {
    out.add(Node(n, '', '', '未知', 'Shadowsocks', '机场 D', 0, _h(2)));
  }
  return out;
}

Node node(String name) => nodes.firstWhere((n) => n.name == name, orElse: () => Node(name, '', '', '未知', '', '', -1, const []));
int byLatency(Node a, Node b) => (a.delay <= 0 ? 100000 : a.delay).compareTo(b.delay <= 0 ? 100000 : b.delay);
/// -1 untested, 0 timed out, then fast to slow.
int latTone(int d) => d < 0 ? -1 : (d == 0 ? 0 : (d < 130 ? 1 : (d < 220 ? 2 : 3)));
String latText(int d) => d < 0 ? '—' : (d == 0 ? tr('超时', 'Timeout') : '$d ms');

class Stats {
  const Stats({this.down = 0, this.up = 0, this.today = 0, this.month = 0, this.active = 0});
  final int down, up, today, month, active;
}

Stats stats = const Stats(down: 2663383, up: 217088, today: 2426656522, month: 51539607552, active: 58);

/// Three significant figures, as the speed readouts have always shown them.
(String, String) sizeParts(int bytes) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  var v = bytes.toDouble();
  var i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  if (i == 0) return ('$bytes', units[0]);
  return (v >= 100 ? v.toStringAsFixed(0) : (v >= 10 ? v.toStringAsFixed(1) : v.toStringAsFixed(2)), units[i]);
}

String sizeText(int bytes) {
  final (n, u) = sizeParts(bytes);
  return '$n $u';
}

class Place {
  const Place(this.lat, this.lon, this.city);
  final double lat, lon;
  final String city;
}

const places = {
  'hk': Place(22.3, 114.2, '香港'),
  'jp': Place(35.7, 139.7, '东京'),
  'sg': Place(1.35, 103.8, '新加坡'),
  'us': Place(34.05, -118.24, '洛杉矶'),
  'tw': Place(25, 121.5, '台北'),
  'kr': Place(37.5, 127, '首尔'),
  'ca': Place(43.65, -79.38, '多伦多'),
  'au': Place(-33.87, 151.21, '悉尼'),
  'gb': Place(51.5, -.13, '伦敦'),
  'de': Place(50.11, 8.68, '法兰克福'),
  'fr': Place(48.86, 2.35, '巴黎'),
  'nl': Place(52.37, 4.9, '阿姆斯特丹'),
};

const sampleYou = Place(31.2, 121.5, '上海');

/// Where a node exits: its measured or guessed spot, else its region's city.
Place? placeOf(Node n) => n.lat != null && n.lon != null ? Place(n.lat!, n.lon!, n.city) : places[n.flag];

List<Conn> conns = const [
  Conn(host: 'googlevideo.com', app: 'YouTube', route: 'media', routeName: '流媒体', node: '新加坡 01', rate: '1.15 MB/s', dur: '2:14', status: 'active', rule: 'GeoSite youtube', ip: '142.250.66.14', net: 'TCP', port: 443, total: '↓ 84.2 MB  ↑ 1.4 MB'),
  Conn(host: 'api.anthropic.com', app: 'Claude', route: 'ai', routeName: 'AI 服务', node: '洛杉矶 VPS', rate: '82.0 KB/s', dur: '0:41', status: 'active', rule: '域名后缀 anthropic.com', ip: '160.79.104.10', net: 'TCP', port: 443, total: '↓ 2.1 MB  ↑ 340 KB'),
  Conn(host: 'github.com', app: 'GitHub', route: 'dev', routeName: '开发', node: '东京 01', rate: '205 KB/s', dur: '0:12', status: 'active', rule: 'GeoSite github', ip: '20.205.243.166', net: 'TCP', port: 443, total: '↓ 3.8 MB  ↑ 96 KB'),
  Conn(host: 'x.com', app: 'X', route: 'default', routeName: '默认', node: '香港 03 IPLC', rate: '失败', dur: '', status: 'failed', rule: '未命中，走默认', ip: '—', net: 'TCP', port: 443, total: '连接超时', err: '连接节点超时'),
  Conn(host: 'slack.com', app: 'Slack', route: 'default', routeName: '默认', node: '香港 03 IPLC', rate: '11.7 KB/s', dur: '8:03', status: 'active', rule: '未命中，走默认', ip: '3.4.12.8', net: 'TCP', port: 443, total: '↓ 1.2 MB  ↑ 210 KB'),
  Conn(host: 'weixin.qq.com', app: '微信', route: 'direct', routeName: '直连', node: '', rate: '3.2 KB/s', dur: '14:52', status: 'active', rule: 'GeoSite cn', ip: '183.3.226.35', net: 'TCP', port: 443, total: '↓ 640 KB  ↑ 88 KB'),
  Conn(host: 'cdn.jsdelivr.net', app: 'Safari', route: 'default', routeName: '默认', node: '香港 03 IPLC', rate: '已关闭', dur: '0:03', status: 'closed', rule: '未命中，走默认', ip: '104.16.85.20', net: 'TCP', port: 443, total: '↓ 412 KB  ↑ 12 KB'),
  Conn(host: 'stun.l.google.com', app: 'Meet', route: 'default', routeName: '默认', node: '香港 03 IPLC', rate: '已关闭', dur: '0:21', status: 'closed', rule: '未命中，走默认', ip: '74.125.250.129', net: 'UDP', port: 19302, total: '↓ 2 KB  ↑ 2 KB'),
];

List<(String, String, String)> logs = const [
  ('38:27', 'warn', '连接 x.com 失败：节点超时'),
  ('38:21', 'info', '流媒体 自动切换到 新加坡 01'),
  ('38:02', 'info', 'Provider 机场 A 已更新，18 个节点'),
  ('37:48', 'warn', '东京 02 测速超时'),
  ('36:15', 'info', '配置已重新加载'),
  ('36:14', 'info', '内核已启动'),
];

List<ProviderInfo> providers = const [
  ProviderInfo(name: '机场 C', kind: '订阅', long: true, used: 41, usage: '已用 412 GB / 1000 GB', expires: '2027-06-30 到期', updated: '刚刚更新'),
  ProviderInfo(name: '机场 D', kind: '订阅', long: true, used: 78, usage: '已用 390 GB / 500 GB', expires: '2027-01-12 到期', updated: '3 小时前更新'),
  ProviderInfo(name: '机场 A', kind: '订阅', nodes: 18, used: 62, usage: '已用 186 GB / 300 GB', expires: '2027-03-01 到期', updated: '2 小时前更新', tones: [1, 1, 1, 1, 1, 1, 2, 2, 2, 3]),
  ProviderInfo(name: '机场 B', kind: '订阅', nodes: 12, used: 94, usage: '已用 94 GB / 100 GB', expires: '4 天后到期', updated: '昨天更新', tones: [1, 1, 2, 2, 2, 3, 3, 0], warn: '流量快用完，4 天后到期'),
  ProviderInfo(name: '自建 洛杉矶 VPS', kind: '节点链接', nodes: 1, updated: '本机添加', tones: [2]),
  ProviderInfo(name: '朋友分享的专线', kind: '节点链接', nodes: 1, updated: '本机添加', tones: [3]),
];

List<Node> providerNodes(ProviderInfo p) =>
    nodes.where((n) => p.id.isNotEmpty ? n.providerId == p.id : n.provider == p.name || (p.name.startsWith('自建') && n.provider == '自建')).toList();

Map<String, double> share = const {'default': .31, 'ai': .1, 'media': .45, 'dev': .06, 'direct': .08};
Map<String, String> rulesOf = const {
  'default': '没命中其他规则的流量',
  'ai': '12 条规则 · openai.com、anthropic.com',
  'media': '18 条规则 · youtube.com、netflix.com',
  'dev': '9 条规则 · github.com、docker.io',
  'direct': '10 条规则 · 国内、局域网',
};
List<(String, String, String, String)> topApps = const [
  ('YouTube', 'googlevideo.com', 'media', '1.15 MB/s'),
  ('GitHub', 'github.com', 'dev', '205 KB/s'),
  ('Claude', 'api.anthropic.com', 'ai', '82.0 KB/s'),
  ('Slack', 'slack.com', 'default', '11.7 KB/s'),
  ('微信', 'weixin.qq.com', 'direct', '3.2 KB/s'),
];
List<(String, String, String, String)> modes = const [
  ('rule', '规则', '按规则把流量分到不同路由。日常就用这个。', '49 条规则 · 5 条路由'),
  ('global', '全局', '所有流量都走默认路由的出口，规则暂停。', '所有流量走默认出口'),
  ('direct', '直连', '所有流量直接连接，不经过任何节点。', '不经过任何节点'),
];

/// Traffic over time: one column per step, split by route.
List<List<double>> trafficStack = () {
  var seed = 7;
  double rnd() {
    seed = (seed * 16807) % 2147483647;
    return seed / 2147483647;
  }

  return List.generate(46, (i) => [.3 + rnd() * .35, .05 + rnd() * .08, .34 + rnd() * .3 + (i % 19 < 3 ? -.2 : 0), .04]);
}();
