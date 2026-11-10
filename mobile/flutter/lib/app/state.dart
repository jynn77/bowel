import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart' show Clipboard, MissingPluginException;
import 'package:flutter/widgets.dart' show BuildContext, Element, GlobalKey, NavigatorState, TextEditingController, Widget, WidgetsBinding;

import 'core.dart';
import 'i18n.dart';
import 'sample.dart';
import 'shell.dart';

class RouteCfg {
  RouteCfg(this.id, this.name, this.node, {this.auto = false}) : direct = node == null;

  final String id;
  String name;
  final bool direct;
  String? node;
  bool auto;
  String policy = 'auto';

  /// scope, order or fixed when the group has no way out; null when it has.
  String? broken;
}

/// What the 代理 page says is wrong with a running tunnel.
class Alert {
  const Alert(this.level, this.text, this.detail, this.action, this.label);

  final String level, text;
  final String? detail, action, label;
}

/// One morph's worth of change. `alias` names a shared element that should
/// start from another element's place, like a tapped node flying to the title.
typedef Morpher = void Function(VoidCallback change, {Map<String, String>? alias});

class AppState extends ChangeNotifier {
  // settings
  String skin = 'paper';
  int setup = 0;

  // navigation
  int tab = 0;

  // the board
  bool on = true, issue = false;
  String? issueText;
  String? x, sub, pending;
  String mode = 'rule', region = 'all', range = 'day';
  bool searching = false, testing = false;
  String nodeQuery = '';

  var routes = [
    RouteCfg('default', '默认', '香港 03 IPLC', auto: true),
    RouteCfg('ai', 'AI 服务', '洛杉矶 VPS'),
    RouteCfg('media', '流媒体', '新加坡 01', auto: true),
    RouteCfg('dev', '开发', '东京 01', auto: true),
    RouteCfg('direct', '直连', null),
  ];

  RouteCfg? reject;
  Alert? alert;

  /// Every route a rule can send traffic to.
  List<RouteCfg> get ruleTargets => [...routes, if (reject != null) reject!];

  // the sheet over the page, and the settings' own stack of pages
  Widget Function(BuildContext)? sheet;
  int sheetId = 0;
  final settingsNav = GlobalKey<NavigatorState>();
  void Function(String routeId, {String? service})? openGroupPage;
  VoidCallback? openRoutesPage;

  // routing and settings, while their pages are up
  Map? routing;

  /// Each route's part of today's traffic.
  Map<String, double> todayShare = const {};
  bool vpnOnDemand = false;
  String vpnExcluded = '';

  // activity
  bool showLog = false, paused = false;
  String filter = 'all', routeFilter = '', netFilter = '', sortBy = 'time', logFilter = 'all';
  int? openConn;
  String? toast;

  // providers
  int? openProvider;
  final openRegions = <String>{};
  bool providerTesting = false;
  String providerQuery = '';

  // first run
  String setupLink = '';
  final setupField = TextEditingController();
  final providerField = TextEditingController();
  bool addBusy = false;
  String? setupError;
  bool setupBusy = false;

  // from the core
  int userRules = 6;
  String coreText = 'mihomo · 运行中';
  String? _vpn;
  bool _seenView = false;

  /// A node's name in these pages → its name in the core.
  final _coreNames = <String, String>{};

  bool get _live => Core.instance.live;

  /// Set by the board while it's on screen.
  Morpher? morpher;

  RouteCfg route(String id) => routes.firstWhere((r) => r.id == id);
  List<RouteCfg> get proxied => routes.where((r) => !r.direct).toList();

  /// system, light or dark.
  String appearance = 'system';

  /// The theme color; '' keeps the skin's own.
  String accent = '';

  /// Whether the board's lines bend, as the logo's do.
  bool twisted = false;

  /// What each route carried in the last 7 and 30 days. Sample data has none.
  Map<String, int>? weekBytes, monthBytes;

  /// The totals of the chosen range; null for today, which comes with each tick.
  Map<String, int>? get rangeBytes => switch (range) { 'week' => weekBytes, 'month' => monthBytes, _ => null };

  /// Where the phone itself is on the map, once that has been measured.
  Place? you = sampleYou;

  static bool _placed(Map where) => ((where['country'] as String?) ?? '').isNotEmpty || ((where['city'] as String?) ?? '').isNotEmpty;

  Future<void> load() async {
    final prefs = await Shell.instance.loadPrefs();
    skin = prefs['skin'] ?? skin;
    appearance = prefs['appearance'] ?? appearance;
    accent = prefs['accent'] ?? accent;
    twisted = prefs['borders'] == 'twisted';
    vpnOnDemand = prefs['vpnOnDemand'] == '1';
    vpnExcluded = prefs['vpnExcluded'] ?? '';
    setup = prefs['setupDone'] == '1' ? 0 : 1;
    notifyListeners();
  }

  void _morph(VoidCallback change, {Map<String, String>? alias}) {
    final m = morpher;
    if (m != null) {
      m(() {
        change();
        notifyListeners();
      }, alias: alias);
    } else {
      change();
      notifyListeners();
    }
  }

  void set(VoidCallback change) {
    change();
    notifyListeners();
  }

  void _later(int ms, VoidCallback fn) => Timer(Duration(milliseconds: ms), fn);

  void selectTab(int i, {bool fromShell = false}) {
    if (!fromShell) Shell.instance.setTab(i);
    if (tab == i) {
      // the tab you're already on: back to the board as it first opens
      if (x != null) collapse();
      return;
    }
    set(() {
      tab = i;
      x = null;
      sub = null;
    });
  }

  void open(String id) {
    if (pending != null) return;
    _morph(() {
      x = id;
      sub = null;
      region = 'all';
      searching = false;
      nodeQuery = '';
    });
  }

  void collapse() => _morph(() {
        x = null;
        sub = null;
      });

  void openSub(String routeId) {
    if (pending != null) return;
    _morph(() {
      x = 'routes';
      sub = routeId;
      region = 'all';
      searching = false;
      nodeQuery = '';
    });
  }

  void subBack() => _morph(() => sub = null);

  void toggleTake() {
    if (pending != null) return;
    if (_live) {
      _setVPN(!on);
      return;
    }
    set(() => pending = 'take');
    _later(850, () {
      _morph(() {
        on = !on;
        issue = false;
        pending = null;
      });
    });
  }

  void retake() {
    if (pending != null) return;
    if (_live) {
      _setVPN(true);
      return;
    }
    set(() => pending = 'take');
    _later(850, () {
      _morph(() {
        on = true;
        issue = false;
        pending = null;
      });
    });
  }

  Future<void> _setVPN(bool want) async {
    set(() => pending = 'take');
    try {
      await Core.instance.setVPN(want);
    } catch (e) {
      set(() => pending = null);
      _toast(Core.message(e));
    }
  }

  void setMode(String m) {
    if (mode == m || pending != null) return;
    _morph(() => mode = m);
    _send('setMode', {'mode': m});
  }

  void pickNode(String routeId, String name) {
    final r = route(routeId);
    if (r.node == name || pending != null) return;
    _morph(() {
      r.node = name;
      r.auto = false;
      r.policy = 'fixed';
      pending = 'node:$routeId';
    }, alias: {'exit-$routeId': 'pick-$name'});
    if (_live) _send('select', {'route': routeId, 'node': _coreNames[name] ?? name});
    _later(800, () => set(() => pending = null));
  }

  /// How the group picks its node: auto, order or fixed.
  void setStrategy(String routeId, String mode) {
    final r = route(routeId);
    if (r.policy == mode) return;
    set(() {
      r.policy = mode;
      r.auto = mode != 'fixed';
    });
    if (_live) routeOp('setStrategy', {'id': routeId, 'mode': mode});
  }

  void toggleAuto(String routeId) => setStrategy(routeId, route(routeId).auto ? 'fixed' : 'auto');

  void test() async {
    set(() => testing = true);
    if (_live) {
      await _send('testRoute', {'route': sub ?? 'default'});
      set(() => testing = false);
      return;
    }
    _later(1000, () => set(() => testing = false));
  }

  void testProvider() async {
    set(() => providerTesting = true);
    final i = openProvider;
    if (_live && i != null && i < providers.length) {
      await _send('testProvider', {'id': providers[i].id});
      set(() => providerTesting = false);
      return;
    }
    _later(1100, () => set(() => providerTesting = false));
  }

  void showSheet(Widget Function(BuildContext) builder) => set(() {
        sheet = builder;
        sheetId++;
      });

  void closeSheet() {
    if (sheet != null) set(() => sheet = null);
  }

  /// Runs a core call, showing a failure; null then, and for "null" answers.
  Future<Object?> ask(String method, [Map<String, Object?> args = const {}]) async {
    try {
      return await Core.instance.call(method, args);
    } catch (e) {
      _toast(Core.message(e), ms: 4000);
      return null;
    }
  }

  Future<bool> send(String method, [Map<String, Object?> args = const {}]) => _send(method, args);

  void say(String text, {int ms = 1600}) => _toast(text, ms: ms);

  void _syncTabTitles() => Shell.instance.setTabTitles([tr('代理', 'Proxy'),tr('活动', 'Activity'), 'Provider', tr('设置', 'Settings')]);

  /// Switches the interface. Every element rebuilds, because a word on
  /// screen can come from anywhere, and what the core wrote is asked for again.
  Future<void> setLanguage(String code) async {
    if (code == language) return;
    final unconnected = tr('未连接', 'Not connected');
    language = code;
    for (final r in routes) {
      if (r.node == unconnected) r.node = tr('未连接', 'Not connected');
    }
    _syncTabTitles();
    void mark(Element e) {
      e.markNeedsBuild();
      e.visitChildren(mark);
    }

    WidgetsBinding.instance.rootElement?.visitChildren(mark);
    if (!Core.instance.live) return;
    await _send('setLanguage', {'name': code});
    final view = await Core.instance.call('view');
    if (view is Map) _applyView(view);
    await refreshRouting();
    notifyListeners();
  }

  Future<void> refreshRouting() async {
    final r = await ask('routing');
    if (r is Map) set(() => routing = r);
  }

  Future<bool> routeOp(String op, Map<String, Object?> args) async {
    final ok = await _send('routeOp', {'op': op, ...args});
    await refreshRouting();
    return ok;
  }

  /// Adds a rule. A failure comes back for the sheet to show: the service
  /// that already holds the rule, or what's wrong with it.
  Future<({String? conflict, String? error})> addServiceRule(Map<String, Object?> args) async {
    try {
      await Core.instance.call('routeOp', {'op': 'addRule', ...args});
    } catch (e) {
      final m = Core.message(e);
      return m.startsWith('conflict:') ? (conflict: m.substring(9), error: null) : (conflict: null, error: m);
    }
    await refreshRouting();
    return (conflict: null, error: null);
  }

  /// What a rule being typed would become. Null when it can't be asked.
  Future<Map?> previewRule(String kind, String text) async {
    try {
      final out = await Core.instance.call('parseRule', {'kind': kind, 'text': text});
      return out is Map ? out : null;
    } catch (_) {
      return null;
    }
  }

  /// Where a connection's rule lives. Null when it's in no service.
  Future<({String group, String service})?> ruleHome(String ruleType, String payload) async {
    try {
      final out = await Core.instance.call('ruleHome', {'kind': ruleType, 'text': payload});
      if (out is Map && out['group'] != null) return (group: out['group'] as String, service: out['service'] as String? ?? '');
    } catch (_) {
      // no link then; the rule's words are still on the row
    }
    return null;
  }

  /// Opens a service in 路由组, from any tab.
  void showService(String group, String service) {
    selectTab(3);
    openGroupPage?.call(group, service: service);
  }

  /// Opens 路由组 from any tab.
  void showRoutes() {
    selectTab(3);
    openRoutesPage?.call();
  }

  /// Opens a route group's services and rules, from any tab.
  void showGroup(String id) {
    selectTab(3);
    openGroupPage?.call(id);
  }

  void setOnDemand(bool want) {
    Shell.instance.setPref('vpnOnDemand', want ? '1' : '0');
    set(() => vpnOnDemand = want);
  }

  void setExcluded(Set<String> ids) {
    final text = ids.join(',');
    Shell.instance.setPref('vpnExcluded', text);
    set(() => vpnExcluded = text);
  }

  /// Takes the user to whatever fixes the issue on 代理.
  void followAlert() {
    final action = alert?.action;
    if (action == null) return;
    if (action == 'providers') {
      selectTab(2);
    } else if (action == 'activity') {
      selectTab(1);
    } else if (action.startsWith('fix:')) {
      selectTab(3);
      openGroupPage?.call(action.substring(4));
    }
  }

  void addRule(RouteCfg r) async {
    final i = openConn;
    final c = i != null && i < conns.length ? conns[i] : null;
    set(() => openConn = null);
    if (_live && c != null && !await _send('addRule', {'host': c.host, 'route': r.id})) return;
    _toast(tr('已添加规则，走 {0}', 'Rule added, via {0}', [r.name]));
  }

  Future<void> closeMany(List<Conn> list) => _send('closeConns', {'ids': [for (final c in list) c.id]});

  void closeConn(Conn c) {
    set(() => openConn = null);
    if (_live) _send('closeConn', {'id': c.id});
  }

  int _toastId = 0;

  /// `ms` 0 keeps it up until the next toast replaces it.
  void _toast(String text, {int ms = 1600}) {
    final id = ++_toastId;
    set(() => toast = text);
    if (ms > 0) {
      _later(ms, () {
        if (id == _toastId) set(() => toast = null);
      });
    }
  }

  /// Runs a core call, showing a failure instead of throwing.
  Future<bool> _send(String method, Map<String, Object?> args) async {
    try {
      await Core.instance.call(method, args);
      return true;
    } catch (e) {
      _toast(Core.message(e));
      return false;
    }
  }

  // ---- Providers ----

  Future<bool> addProvider(String input) async {
    final text = input.trim();
    if (text.isEmpty || addBusy) return false;
    if (!_live) {
      _toast(tr('内核没有启动，暂时不能添加', 'The core isn\'t running, so this can\'t be added yet'), ms: 5000);
      return false;
    }
    addBusy = true;
    _toast(tr('正在添加', 'Adding'), ms: 0);
    try {
      await Core.instance.call('addProvider', {'input': text});
      providerField.clear();
      _toast(tr('已添加', 'Added'));
      return true;
    } catch (e) {
      // The link stays in the field so it can be read and retried.
      _toast(Core.message(e), ms: 6000);
      return false;
    } finally {
      addBusy = false;
    }
  }

  void updateProvider(ProviderInfo p) async {
    if (!_live) return;
    _toast(tr('正在更新', 'Updating'), ms: 0);
    if (await _send('updateProvider', {'id': p.id})) _toast(tr('已更新', 'Updated'));
  }

  void updateAllProviders() async {
    _toast(tr('正在更新', 'Updating'), ms: 0);
    final failed = await ask('updateAll');
    if (failed == 0) {
      _toast(tr('已全部更新', 'All updated'));
    } else if (failed is int) {
      _toast(tr('{0} 个更新失败', '{0} failed to update', [failed]), ms: 4000);
    }
  }

  void deleteProvider(ProviderInfo p) {
    if (!_live) return;
    set(() => openProvider = null);
    _send('deleteProvider', {'id': p.id});
  }

  void setProviderEnabled(ProviderInfo p, bool on) {
    _send('setProviderEnabled', {'id': p.id, 'on': on});
  }

  void setProviderAuto(ProviderInfo p, int hours) {
    _send('setAutoUpdate', {'id': p.id, 'hours': hours});
  }

  Future<bool> renameProvider(ProviderInfo p, String name) => _send('renameProvider', {'id': p.id, 'name': name});

  /// Adds the YAML file the user picks.
  Future<void> addFile() async {
    final path = await _pick(Shell.instance.pickFile);
    if (path == null) return;
    if (setup == 1) {
      set(() => setupBusy = true);
      final ok = await _send('addFile', {'path': path});
      set(() {
        setupBusy = false;
        if (ok) setup = 2;
      });
      return;
    }
    _toast(tr('正在添加', 'Adding'), ms: 0);
    if (await _send('addFile', {'path': path})) _toast(tr('已添加', 'Added'));
  }

  /// Adds what the QR code in a picture says.
  Future<void> addFromImage() async {
    final path = await _pick(Shell.instance.pickImage);
    if (path == null) return;
    final out = await ask('decodeQR', {'path': path});
    if (out is! Map) return;
    if (out['error'] != null) {
      _toast(out['error'] as String, ms: 4000);
      return;
    }
    await addProvider(out['text'] as String);
  }

  Future<void> addFromClipboard() async {
    final text = (await Clipboard.getData('text/plain'))?.text?.trim() ?? '';
    if (text.isEmpty) {
      _toast(tr('剪贴板是空的', 'The clipboard is empty'));
      return;
    }
    providerField.text = text;
    await addProvider(text);
  }

  Future<String?> _pick(Future<String?> Function() pick) async {
    try {
      return await pick();
    } on MissingPluginException {
      _toast(tr('这台设备暂时不能选择文件', 'This device can\'t choose files yet'));
      return null;
    } catch (e) {
      _toast(Core.message(e), ms: 4000);
      return null;
    }
  }

  /// Reads a QR code and adds it the same way a pasted link is added.
  Future<void> scanLink() async {
    final String? code;
    try {
      code = await Shell.instance.scanQR();
    } on MissingPluginException {
      if (setup == 1) {
        set(() => setupError = tr('这台设备暂时不能扫码，请粘贴链接', 'This device can\'t scan yet. Paste the link instead.'));
      } else {
        _toast(tr('这台设备暂时不能扫码', 'This device can\'t scan yet'));
      }
      return;
    }
    if (code == null) return;
    if (setup == 1) {
      setupField.text = code;
      setupLink = code;
      await submitSetupLink();
    } else {
      providerField.text = code;
      await addProvider(code);
    }
  }

  Future<void> submitSetupLink() async {
    if (setupBusy) return;
    final text = setupLink.trim();
    if (text.isEmpty || !_live) {
      set(() {
        setupError = null;
        setup = 2;
      });
      return;
    }
    set(() {
      setupBusy = true;
      setupError = null;
    });
    try {
      await Core.instance.call('addProvider', {'input': text});
      set(() {
        setupBusy = false;
        setup = 2;
      });
    } catch (e) {
      set(() {
        setupBusy = false;
        setupError = Core.message(e);
      });
    }
  }

  // ---- What the core pushes ----

  /// Connects to the native host and starts from its current state.
  Future<void> attachCore() async {
    final core = Core.instance;
    core.onEmit = (name, value) {
      switch (name) {
        case 'view':
          _applyView(value as Map);
        case 'tick':
          _applyTick(value as Map);
        case 'conns':
          _applyConns(value as List);
      }
    };
    await core.start();
    if (!core.live) return;
    _clearSamples();
    await core.call('conns', {'on': true});
    final view = await core.call('view');
    if (view is Map) {
      language = view['language'] as String? ?? 'zh';
      _syncTabTitles();
      _applyView(view);
    }
  }

  void _clearSamples() {
    nodes = const [];
    conns = const [];
    logs = const [];
    providers = const [];
    topApps = const [];
    trafficStack = const [];
    stats = const Stats();
    userRules = 0;
    coreText = '';
    share = {for (final r in routes) r.id: 0.0};
    trafficRoutes = [for (final r in routes) r.id];
    on = false;
    openProvider = null;
  }

  void _applyView(Map v) {
    final vpn = v['vpn'] as String;
    final wasOn = on;
    final nowOn = vpn == 'on';

    mode = v['mode'] as String? ?? mode;
    final raised = v['issue'] as Map?;
    alert = raised == null ? null : Alert(raised['level'] as String? ?? 'warn', raised['text'] as String? ?? '', raised['detail'] as String?, raised['action'] as String?, raised['label'] as String?);
    userRules = v['userRules'] as int? ?? 0;
    final at = v['you'] as Map?;
    you = at == null ? null : Place((at['lat'] as num).toDouble(), (at['lon'] as num).toDouble(), at['city'] as String? ?? '');
    coreText = v['core'] as String? ?? '';
    _applyNodes(v['nodes'] as List? ?? const []);
    _applyProviders(v['providers'] as List? ?? const []);
    _applyRoutes(v['routes'] as List? ?? const []);
    final count = v['ruleCount'] as int? ?? 0;
    modes = [
      ('rule', tr('规则', 'Rules'), tr('按规则把流量分到不同路由。日常就用这个。', 'Splits traffic across routes by rules. The everyday choice.'), tr('{0} 条规则 · {1} 条路由', '{0} rules · {1} routes', [count, routes.length])),
      ('global', tr('全局', 'Global'), tr('所有流量都走默认路由的出口，规则暂停。', 'All traffic uses the Default route exit. Rules pause.'), tr('所有流量走默认出口', 'All traffic uses the default exit')),
      ('direct', tr('直连', 'Direct'), tr('所有流量直接连接，不经过任何节点。', 'All traffic connects directly, through no node.'), tr('不经过任何节点', 'Through no node')),
    ];

    void status() {
      on = nowOn;
      issue = vpn == 'error';
      issueText = issue ? v['vpnError'] as String? : null;
      if (vpn == 'connecting' || vpn == 'disconnecting') {
        pending = 'take';
      } else if (_vpn != vpn && pending == 'take') {
        pending = null;
      }
      // the opened hero stays: its big button is how you turn it back on
      if (wasOn && !nowOn && x != 'hero') {
        x = null;
        sub = null;
      }
      _vpn = vpn;
    }

    if (wasOn != nowOn) {
      _morph(status);
    } else {
      status();
      notifyListeners();
    }
    _refreshLogs();
  }

  /// A page name for a node whose own is taken. A pasted link's node already
  /// goes by its Provider's name, so naming the Provider again says nothing.
  String _distinctName(String name, String provider) {
    final out = name == provider ? name : '$name · $provider';
    if (!_coreNames.containsKey(out)) return out;
    var i = 2;
    while (_coreNames.containsKey('$out $i')) {
      i++;
    }
    return '$out $i';
  }

  void _applyNodes(List list) {
    _coreNames.clear();
    final names = <String, String>{}; // the core's name → the page's
    final out = <Node>[];
    for (final raw in list) {
      final n = raw as Map;
      final core = n['name'] as String;
      final providerName = n['providerName'] as String? ?? '';
      var name = n['display'] as String? ?? core;
      if (_coreNames.containsKey(name)) name = _distinctName(name, providerName);
      _coreNames[name] = core;
      names[core] = name;
      final where = n['where'] as Map? ?? const {};
      final bars = (n['h'] as List? ?? const []).cast<int>();
      out.add(Node(
        name,
        (where['country'] as String? ?? '').toLowerCase(),
        where['city'] as String? ?? '',
        n['source'] as String? ?? '未知',
        n['type'] as String? ?? '',
        providerName,
        bars.isEmpty ? -1 : n['delay'] as int? ?? 0,
        [...List.filled(10 - bars.length.clamp(0, 10), 2), ...bars],
        providerId: n['provider'] as String? ?? '',
        lat: _placed(where) ? (where['lat'] as num?)?.toDouble() : null,
        lon: _placed(where) ? (where['lon'] as num?)?.toDouble() : null,
      ));
    }
    nodes = out;
    _nodeNames = names;
  }

  Map<String, String> _nodeNames = const {};

  void _applyProviders(List list) {
    providers = [
      for (final raw in list) _providerOf(raw as Map),
    ];
    final open = openProvider;
    if (open != null && open >= providers.length) openProvider = null;
  }

  ProviderInfo _providerOf(Map p) {
    final name = p['name'] as String;
    final kind = switch (p['kind']) { 'subscription' => tr('订阅', 'Subscription'), 'link' => tr('节点链接', 'Node link'), _ => tr('文件', 'File') };
    final count = p['nodes'] as int? ?? 0;
    final used = p['used'] as int? ?? 0, total = p['total'] as int? ?? 0, expire = p['expire'] as int? ?? 0;
    final enabled = p['enabled'] as bool? ?? true;
    final warnKind = switch (p['warnKind']) { 'quota' => tr('流量快用完了', 'Almost out of data'), 'expire' => tr('快到期了', 'Expires soon'), _ => null };
    final own = nodes.where((n) => n.providerId == p['id']);
    return ProviderInfo(
      id: p['id'] as String,
      name: name,
      kind: kind,
      long: count > 20 && enabled,
      nodes: count,
      used: total > 0 ? (used * 100 / total).round().clamp(0, 100) : null,
      usage: total > 0 ? tr('已用 {0} / {1}', '{0} of {1} used', [sizeText(used), sizeText(total)]) : null,
      expires: expire > 0 ? tr('{0} 到期', 'Expires {0}', [DateTime.fromMillisecondsSinceEpoch(expire * 1000).toString().substring(0, 10)]) : null,
      updated: _updatedText(p['updated'] as int? ?? 0),
      tones: [for (final n in own) latTone(n.delay)],
      warn: (p['error'] as String?) ?? warnKind,
      enabled: enabled,
      autoUpdate: p['autoUpdate'] as int? ?? 0,
      source: p['source'] as String? ?? '',
      usedBy: (p['usedBy'] as List? ?? const []).cast<String>(),
      subscription: p['kind'] == 'subscription',
      shareable: p['kind'] != 'file',
    );
  }

  String _updatedText(int ms) {
    if (ms <= 0) return '';
    final ago = DateTime.now().difference(DateTime.fromMillisecondsSinceEpoch(ms));
    if (ago.inMinutes < 1) return tr('刚刚更新', 'Updated just now');
    if (ago.inHours < 1) return tr('{0} 分钟前更新', 'Updated {0} min ago', [ago.inMinutes]);
    if (ago.inDays < 1) return tr('{0} 小时前更新', 'Updated {0} h ago', [ago.inHours]);
    return ago.inDays == 1 ? tr('昨天更新', 'Updated yesterday') : tr('{0} 天前更新', 'Updated {0} days ago', [ago.inDays]);
  }

  void _applyRoutes(List list) {
    if (list.isEmpty) return;
    final rules = <String, String>{};
    final next = <RouteCfg>[];
    RouteCfg? blocked;
    for (final raw in list) {
      final v = raw as Map;
      final id = v['id'] as String;
      final detail = v['detail'] as String? ?? '';
      final count = v['rules'] as int? ?? 0;
      final ruleWord = tr('{0} 条规则', count == 1 ? '{0} rule' : '{0} rules', [count]);
      rules[id] = count > 0 ? (detail.isEmpty ? ruleWord : '$ruleWord · $detail') : detail;
      final known = routes.where((r) => r.id == id);
      final isDirect = v['direct'] as bool? ?? false;
      final r = known.isNotEmpty ? known.first : RouteCfg(id, v['name'] as String? ?? id, isDirect ? null : tr('未连接', 'Not connected'), auto: true);
      r.name = v['name'] as String? ?? r.name;
      final policy = v['policy'] as String? ?? '';
      r.policy = policy.isEmpty ? 'auto' : policy;
      r.broken = v['broken'] as String?;
      if (v['reject'] == true) {
        blocked = r;
        continue;
      }
      next.add(r);
      if (r.direct) continue;
      final core = v['node'] as String? ?? '';
      if (core.isNotEmpty) {
        r.node = _nodeNames[core] ?? core;
        r.auto = v['auto'] as bool? ?? r.auto;
      } else if (!_seenView) {
        r.node = tr('未连接', 'Not connected');
        r.auto = true;
      }
    }
    final same = next.length == routes.length && next.indexed.every((e) => routes[e.$1].id == e.$2.id);
    routes = next;
    reject = blocked;
    if (!same) {
      trafficStack = const [];
      trafficRoutes = [for (final r in routes) r.id];
      if (sub != null && !routes.any((r) => r.id == sub)) sub = null;
    }
    share = {for (final r in routes) r.id: share[r.id] ?? 0};
    rulesOf = {for (final r in routes) r.id: rules[r.id] ?? ''};
    _seenView = true;
  }

  void _applyTick(Map t) {
    final byRoute = (t['byRoute'] as Map? ?? const {}).cast<String, Object?>();
    final rates = [for (final r in routes) (byRoute[r.id] as int?) ?? 0];
    final total = rates.fold(0, (a, b) => a + b);
    if (total > 0) {
      share = {for (final (i, r) in routes.indexed) r.id: rates[i] / total};
    }
    trafficStack = [
      ...trafficStack.skip(trafficStack.length >= 46 ? 1 : 0),
      [for (final b in rates) b.toDouble()],
    ];
    final ranges = t['ranges'] as Map?;
    if (ranges != null) {
      weekBytes = (ranges['week'] as Map).cast<String, int>();
      monthBytes = (ranges['month'] as Map).cast<String, int>();
    }
    final today = t['today'] as Map?;
    if (today != null) todayShare = {for (final e in today.entries) e.key as String: (e.value as num).toDouble()};
    stats = Stats(
      down: t['down'] as int? ?? 0,
      up: t['up'] as int? ?? 0,
      today: (t['todayDown'] as int? ?? 0) + (t['todayUp'] as int? ?? 0),
      month: t['monthDown'] as int? ?? 0,
      active: t['active'] as int? ?? 0,
    );
    notifyListeners();
  }

  void _applyConns(List list) {
    if (openConn != null || paused) return; // an open row doesn't move under the finger
    conns = [for (final raw in list) _connOf(raw as Map)];
    final byRate = conns.where((c) => c.status == 'active').toList();
    final rates = {for (final raw in list) (raw as Map)['id'] as String: (raw['downRate'] as int? ?? 0) + (raw['upRate'] as int? ?? 0)};
    byRate.sort((a, b) => rates[b.id]!.compareTo(rates[a.id]!));
    topApps = [for (final c in byRate.take(5)) (_site(c.host), c.host, c.route, c.rate)];
  }

  String _site(String host) {
    final parts = host.split('.');
    return parts.length > 1 ? parts.sublist(parts.length - 2).join('.') : host;
  }

  Conn _connOf(Map c) {
    final routeId = c['route'] as String? ?? '';
    final known = ruleTargets.where((r) => r.id == routeId);
    final status = c['status'] as String? ?? 'closed';
    final down = c['downRate'] as int? ?? 0, up = c['upRate'] as int? ?? 0;
    final start = c['start'] as int? ?? 0, end = c['end'] as int? ?? 0;
    final host = c['host'] as String? ?? '';
    final span = Duration(milliseconds: (end > 0 ? end : DateTime.now().millisecondsSinceEpoch) - start);
    final node = c['node'] as String? ?? '';
    return Conn(
      id: c['id'] as String? ?? '',
      host: host,
      app: _site(host),
      route: known.isEmpty ? 'direct' : routeId,
      routeName: known.isEmpty ? tr('直连', 'Direct') : known.first.name,
      node: node == 'DIRECT' ? '' : node,
      rate: switch (status) { 'failed' => tr('失败', 'Failed'), 'closed' => tr('已关闭', 'Closed'), 'blocked' => tr('已拦截', 'Blocked'), _ => '${sizeText(down + up)}/s' },
      dur: status == 'failed' || status == 'blocked' ? '' : '${span.inMinutes}:${(span.inSeconds % 60).toString().padLeft(2, '0')}',
      status: status,
      rule: c['rule'] as String? ?? '',
      ruleType: c['ruleType'] as String? ?? '',
      payload: c['payload'] as String? ?? '',
      ip: c['ip'] as String? ?? '',
      net: c['net'] as String? ?? '',
      port: c['port'] as int? ?? 0,
      total: status == 'blocked' ? '—' : '↓ ${sizeText(c['down'] as int? ?? 0)} · ↑ ${sizeText(c['up'] as int? ?? 0)}',
      err: c['err'] as String?,
      speed: down + up,
      bytes: (c['down'] as int? ?? 0) + (c['up'] as int? ?? 0),
    );
  }

  Future<void> _refreshLogs() async {
    if (paused) return;
    try {
      final list = await Core.instance.call('logs');
      if (list is! List) return;
      logs = [
        for (final raw in list)
          (() {
            final m = raw as Map;
            final t = DateTime.fromMillisecondsSinceEpoch(m['time'] as int);
            final stamp = '${t.minute.toString().padLeft(2, '0')}:${t.second.toString().padLeft(2, '0')}';
            return (stamp, m['level'] as String, m['text'] as String);
          })(),
      ];
    } catch (_) {
      // the log list simply stays as it was
    }
  }

  /// A skin brings its own theme color, as it does on the desktop.
  void setSkin(String id) {
    Shell.instance.setPref('skin', id);
    Shell.instance.setPref('accent', '');
    set(() {
      skin = id;
      accent = '';
    });
  }

  void setAppearance(String v) {
    Shell.instance.setPref('appearance', v);
    set(() => appearance = v);
  }

  void setAccent(String v) {
    Shell.instance.setPref('accent', v);
    set(() => accent = v);
  }

  void setTwisted(bool v) {
    Shell.instance.setPref('borders', v ? 'twisted' : 'straight');
    set(() => twisted = v);
  }

  /// Whether to draw in dark, given what the system is set to.
  bool wantsDark(Brightness system) => appearance == 'system' ? system == Brightness.dark : appearance == 'dark';

  void finishSetup({required bool take}) {
    Shell.instance.setPref('setupDone', '1');
    if (_live) {
      set(() => setup = 0);
      if (take) _setVPN(true);
      return;
    }
    set(() {
      setup = 0;
      on = take;
    });
  }
}
