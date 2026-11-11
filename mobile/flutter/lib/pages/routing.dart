import 'package:flutter/widgets.dart';

import '../app/i18n.dart';
import '../app/sample.dart';
import '../app/state.dart';
import '../ui/atoms.dart';
import '../ui/skin.dart';
import 'frame.dart';
import 'rows.dart';
import 'sheet.dart';

List<Map> _groups(AppState app) => ((app.routing?['groups'] as List?) ?? const []).cast<Map>();
List<Map> _list(Map? m, String key) => ((m?[key] as List?) ?? const []).cast<Map>();
List<String> _strs(Map m, String key) => ((m[key] as List?) ?? const []).cast<String>();

RouteCfg? _cfgOf(AppState app, String id) => app.ruleTargets.where((r) => r.id == id).firstOrNull;

String _modeOf(Map g) {
  final m = g['mode'] as String? ?? '';
  return m.isEmpty ? 'auto' : m;
}

String _modeName(String mode) => switch (mode) { 'fixed' => tr('固定节点', 'Fixed node'), 'order' => tr('顺序备用', 'Backup order'), _ => tr('自动最快', 'Fastest') };

String _noNodeWord(Map g) => g['reject'] == true ? tr('已拦截', 'Blocked') : tr('直接连接', 'Direct connection');

String _nodeDisplay(AppState app, String full) {
  for (final n in _list(app.routing, 'nodes')) {
    if (n['name'] == full) return n['display'] as String;
  }
  return tr('已不存在的节点', 'Node no longer exists');
}

/// A strategy in a few words: 自动最快 · 香港.
String _policyWord(AppState app, Map g, RouteCfg? rc) {
  if (g['direct'] == true) return _noNodeWord(g);
  final mode = _modeOf(g);
  if (mode == 'order' && rc?.broken == null) {
    final at = _strs(g, 'order').indexWhere((n) => _nodeDisplay(app, n) == rc?.node);
    if (at >= 0) return tr('顺序备用 · 第 {0} 位', 'Backup order · No. {0}', [at + 1]);
  }
  if (mode == 'auto') {
    final names = {for (final r in _list(app.routing, 'regions')) r['code']: r['name'] as String};
    final providers = {for (final p in _list(app.routing, 'providers')) p['id']: p['name'] as String};
    final scope = [for (final r in _strs(g, 'regions')) names[r] ?? r, for (final p in _strs(g, 'providers')) providers[p] ?? p];
    if (scope.isNotEmpty) return '${_modeName(mode)} · ${scope.join(tr('、', ', '))}';
  }
  return _modeName(mode);
}

String _exitWord(AppState app, Map g, RouteCfg? rc) {
  if (g['direct'] == true) return _noNodeWord(g);
  return switch (rc?.broken) {
    'fixed' => tr('{0} 不通', '{0} is down', [_nodeDisplay(app, g['fixed'] as String? ?? '')]),
    null => rc?.node ?? '—',
    _ => tr('没有可用节点', 'No usable node'),
  };
}

String _delayWord(RouteCfg? rc) => rc == null || rc.direct || rc.broken != null || rc.node == null ? '—' : latText(node(rc.node!).delay);

/// What a service still sends: nothing while it is off.
List<Map> _live(Map s) => s['off'] == true ? const [] : [for (final r in _list(s, 'rules')) if (r['off'] != true) r];

/// 路由组: which groups there are, and where an address would go.
class RoutesPage extends StatefulWidget {
  const RoutesPage({super.key, required this.app});
  final AppState app;

  @override
  State<RoutesPage> createState() => _RoutesPageState();
}

class _RoutesPageState extends State<RoutesPage> {
  Map? _result;
  String? _host;
  Object? _routing;

  @override
  void initState() {
    super.initState();
    _routing = widget.app.routing;
    widget.app.addListener(_routingChanged);
    widget.app.refreshRouting();
  }

  @override
  void dispose() {
    widget.app.removeListener(_routingChanged);
    super.dispose();
  }

  /// A rule or group that changed can send the tested address elsewhere.
  void _routingChanged() {
    if (identical(widget.app.routing, _routing)) return;
    _routing = widget.app.routing;
    if (_host != null) _explain(_host!);
  }

  Future<void> _explain(String host) async {
    _host = host;
    final out = await widget.app.ask('explain', {'host': host});
    if (out is Map && mounted) setState(() => _result = out.isEmpty ? null : out);
  }

  void _newGroup() {
    final app = widget.app;
    final nav = Navigator.of(context);
    inputSheet(app, tr('新建路由组', 'New route group'), (name) async {
      if (name.isEmpty || !await app.routeOp('newGroup', {'name': name})) return;
      final made = _groups(app).where((g) => g['name'] == name).firstOrNull;
      if (made != null) nav.push(routeOf(GroupPage(app: app, id: made['id'] as String)));
    }, hint: tr('名称', 'Name'), action: tr('新建', 'Create'));
  }

  Widget _explained(Look l) {
    final r = _result!;
    final err = r['error'] as String?, note = r['note'] as String?;
    final route = r['route'] as String? ?? '', service = r['serviceId'] as String?;
    return Container(
      margin: const EdgeInsets.fromLTRB(16, 8, 16, 0),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(color: l.paper2, borderRadius: l.br(12)),
      child: err != null || note != null
          ? Text(err ?? note!, style: l.text(13.5, color: err != null ? l.signalT : l.ink, lh: 19))
          : Row(children: [
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Row(children: [
                    Square(l.route(route), size: 10),
                    const SizedBox(width: 6),
                    Expanded(child: Line1(tr('走 {0}', 'Via {0}', ['${r['name'] ?? ''}${(r['exit'] as String? ?? '').isEmpty ? '' : ' → ${r['exit']}'}']), l.text(15, weight: 600, lh: 21))),
                  ]),
                  Text('${r['service'] != null ? '${r['service']} · ' : ''}${r['rule'] ?? ''}', style: l.text(12.5, color: l.mute, lh: 17)),
                ]),
              ),
              if (service != null && route.isNotEmpty) ...[
                const SizedBox(width: 8),
                Btn(tr('查看', 'View'), minH: 36, padH: 10, fontSize: 13, onTap: () => pushPage(context, GroupPage(app: widget.app, id: route, service: service))),
              ],
            ]),
    );
  }

  @override
  Widget build(BuildContext context) {
    final app = widget.app;
    return ListenableBuilder(
      listenable: app,
      builder: (context, _) {
        final l = Skin.of(context);
        final all = _groups(app);
        var services = 0, rules = 0;
        for (final g in all) {
          for (final s in _list(g, 'services')) {
            if (s['mine'] != true && s['off'] != true) services++;
            rules += _live(s).length;
          }
        }
        return SubPage(
          title: tr('路由组', 'Route groups'),
          dockChild: Row(children: [
            Expanded(child: SearchField(hint: tr('测试一个地址', 'Test an address'), label: tr('测试一个地址', 'Test an address'), keyboardType: TextInputType.url, onSubmitted: _explain)),
            const SizedBox(width: 8),
            IconBox('plus', label: tr('新建路由组', 'New route group'), onTap: _newGroup),
          ]),
          children: [
            if (all.isNotEmpty)
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 2, 16, 0),
                child: Text(tr('{0} 个路由组 · {1} 个服务 · {2} 条规则，写得越具体越优先', '{0} route groups · {1} services · {2} rules, the more specific first', [all.length, services, rules]), style: l.text(13, color: l.mute, lh: 18)),
              ),
            if (_result != null) _explained(l),
            const SizedBox(height: 6),
            for (final g in all)
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: _groupRow(context, l, app, g),
              ),
          ],
        );
      },
    );
  }

  Widget _groupRow(BuildContext context, Look l, AppState app, Map g) {
    final id = g['id'] as String;
    final rc = _cfgOf(app, id);
    final direct = g['direct'] == true;
    final count = _list(g, 'services').where((s) => s['mine'] != true && s['off'] != true).length;
    final small = l.text(12.5, color: l.mute, lh: 17);
    return Tap(
      onTap: () => pushPage(context, GroupPage(app: app, id: id)),
      child: hairBottom(
        l,
        Container(
          constraints: const BoxConstraints(minHeight: 56),
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Row(children: [
            Square(l.route(id), size: 12),
            const SizedBox(width: 10),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Line1(g['name'] as String? ?? id, l.text(15, lh: 21)),
                if (direct)
                  Line1('${_noNodeWord(g)} · ${tr('{0} 个服务', count == 1 ? '{0} service' : '{0} services', [count])}', small)
                else
                  Row(children: [
                    Flexible(child: Line1(_policyWord(app, g, rc), small)),
                    Text(' → ', style: l.text(12.5, color: l.faint, lh: 17)),
                    Flexible(child: Line1(_exitWord(app, g, rc), l.text(12.5, color: rc?.broken != null ? l.signalT : l.ink2, lh: 17))),
                  ]),
              ]),
            ),
            if (!direct) ...[const SizedBox(width: 10), Text(_delayWord(rc), maxLines: 1, style: l.text(12.5, color: l.mute, lh: 17))],
            const SizedBox(width: 4),
            Ic('chev', size: 15, color: l.mute),
          ]),
        ),
      ),
    );
  }
}

/// One group: its strategy, services and test sites. `service` opens that
/// service and brings it into view.
class GroupPage extends StatefulWidget {
  const GroupPage({super.key, required this.app, required this.id, this.service});
  final AppState app;
  final String id;
  final String? service;

  @override
  State<GroupPage> createState() => _GroupPageState();
}

class _GroupPageState extends State<GroupPage> {
  final _openServices = <String>{}; // several at once: opening one never moves another
  final _serviceKeys = <String, GlobalKey>{};
  String? _scrollTo;
  bool _sitesBusy = false;

  AppState get app => widget.app;
  String get id => widget.id;

  @override
  void initState() {
    super.initState();
    final s = widget.service;
    if (s != null && s.isNotEmpty) _show(s);
    app.refreshRouting();
  }

  void _show(String service) {
    _openServices.add(service);
    _scrollTo = service;
  }

  /// Once the asked-for service is on the page, scrolls to it.
  void _scrollWhenReady(Map g) {
    final target = _scrollTo;
    if (target == null || !_list(g, 'services').any((s) => s['id'] == target)) return;
    _scrollTo = null;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      final c = _serviceKeys[target]?.currentContext;
      if (c != null && c.mounted) Scrollable.ensureVisible(c, duration: const Duration(milliseconds: 320), curve: easeCard, alignment: .1);
    });
  }

  Map? get _g {
    for (final g in _groups(app)) {
      if (g['id'] == id) return g;
    }
    return null;
  }

  List<Map> get _nodes => _list(app.routing, 'nodes');

  PickOption<String> _nodeOption(Map n) {
    final d = n['delay'] as int? ?? -1;
    return PickOption(n['name'] as String, n['display'] as String, sub: '${n['provider']}${d > 0 ? ' · $d ms' : (d == 0 ? ' · ${tr('超时', 'Timeout')}' : '')}', leading: FlagDot(n['flag'] as String? ?? '', 18));
  }

  void _pickFixed(Map g) {
    pickSheet<String>(app, tr('固定到节点', 'Fix to node'), [for (final n in _nodes) _nodeOption(n)], (v) => app.routeOp('setFixed', {'id': id, 'node': v}), value: g['fixed'] as String?, search: _nodes.length > 8);
  }

  void _addOrder(Map g) {
    final have = _strs(g, 'order');
    pickSheet<String>(app, tr('加入备用列表', 'Add to fallback list'), [for (final n in _nodes) if (!have.contains(n['name'])) _nodeOption(n)], (v) => app.routeOp('setOrder', {'id': id, 'order': [...have, v]}), search: _nodes.length > 8);
  }

  void _moveOrder(Map g, int i, int by) {
    final list = [..._strs(g, 'order')];
    final v = list.removeAt(i);
    list.insert(i + by, v);
    app.routeOp('setOrder', {'id': id, 'order': list});
  }

  void _scope(Map g, {List<String>? regions, List<String>? providers}) {
    app.routeOp('setScope', {'id': id, 'regions': regions ?? _strs(g, 'regions'), 'providers': providers ?? _strs(g, 'providers')});
  }

  Widget _chips(List<Widget> kids) => Padding(padding: const EdgeInsets.only(top: 6, bottom: 4), child: Wrap(spacing: 4, runSpacing: 4, children: kids));

  /// Where the group is going now, and why not when it can't.
  Widget _status(Look l, Map g, RouteCfg? rc) {
    final broken = rc?.broken;
    final style = l.text(14, color: l.ink2, lh: 20);
    return Padding(
      padding: const EdgeInsets.only(top: 6),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Expanded(
            child: Row(children: [
              Flexible(child: Line1(_policyWord(app, g, rc), style)),
              Text(' → ', style: l.text(14, color: l.faint, lh: 20)),
              Flexible(child: Line1(_exitWord(app, g, rc), l.text(14, weight: 600, color: broken != null ? l.signalT : l.ink, lh: 20))),
            ]),
          ),
          const SizedBox(width: 8),
          Text(_delayWord(rc), maxLines: 1, style: l.text(13, color: l.mute, lh: 20)),
        ]),
        if (broken != null)
          Padding(
            padding: const EdgeInsets.only(top: 2),
            child: Text(
              switch (broken) {
                'fixed' => tr('固定节点不会自动切换，这个路由组现在连不上。', "A fixed node doesn't switch by itself, so this route group can't connect now."),
                'scope' => tr('范围不会被悄悄放宽，这个路由组现在连不上。', "A range is never widened on its own, so this route group can't connect now."),
                _ => tr('这个路由组现在连不上。', "This route group can't connect now."),
              },
              style: l.text(13, color: l.signalT, lh: 18),
            ),
          ),
      ]),
    );
  }

  Widget _strategy(Look l, Map g, RouteCfg? rc) {
    final mode = _modeOf(g);
    final regions = _strs(g, 'regions'), providers = _strs(g, 'providers'), order = _strs(g, 'order');
    final allRegions = _list(app.routing, 'regions'), allProviders = _list(app.routing, 'providers');
    // A chip counts what's left of the other filter; one chosen without nodes still shows.
    int regionCount(String code) => _nodes.where((n) => n['country'] == code && (providers.isEmpty || providers.contains(n['providerId']))).length;
    int providerCount(String p) => _nodes.where((n) => n['providerId'] == p && (regions.isEmpty || regions.contains(n['country']))).length;
    final known = {for (final r in allRegions) r['code'] as String};
    final regionChips = [
      for (final r in allRegions) (r['code'] as String, r['name'] as String),
      for (final r in regions) if (!known.contains(r)) (r, r),
    ];
    final knownProviders = {for (final p in allProviders) p['id'] as String};
    final providerChips = [
      for (final p in allProviders) (p['id'] as String, p['name'] as String),
      for (final p in providers) if (!knownProviders.contains(p)) (p, p),
    ];
    final kids = <Widget>[
      _status(l, g, rc),
      Padding(
        padding: const EdgeInsets.only(top: 10, bottom: 4),
        child: FittedBox(
          fit: BoxFit.scaleDown,
          alignment: Alignment.centerLeft,
          child: Seg<String>(
            options: [('auto', _modeName('auto')), ('order', _modeName('order')), ('fixed', _modeName('fixed'))],
            value: mode,
            padH: 12,
            onChanged: (v) => app.routeOp('setStrategy', {'id': id, 'mode': v}),
          ),
        ),
      ),
    ];
    if (mode == 'auto') {
      if (regionChips.isNotEmpty) {
        kids.addAll([
          Padding(padding: const EdgeInsets.only(top: 8), child: Text(tr('地区', 'Regions'), style: l.text(13, color: l.mute, lh: 18))),
          _chips([
            Chip(text: tr('全部', 'All'), on: regions.isEmpty, onTap: () => _scope(g, regions: const [])),
            for (final (code, name) in regionChips)
              Chip(
                text: name,
                count: regionCount(code),
                leading: FlagDot(code.toLowerCase(), 16),
                on: regions.contains(code),
                onTap: () => _scope(g, regions: regions.contains(code) ? ([...regions]..remove(code)) : [...regions, code]),
              ),
          ]),
        ]);
      }
      if (allProviders.length > 1 || providers.isNotEmpty) {
        kids.addAll([
          Padding(padding: const EdgeInsets.only(top: 4), child: Text('Provider', style: l.text(13, color: l.mute, lh: 18))),
          _chips([
            Chip(text: tr('全部', 'All'), on: providers.isEmpty, onTap: () => _scope(g, providers: const [])),
            for (final (pid, name) in providerChips)
              Chip(
                text: name,
                count: providerCount(pid),
                on: providers.contains(pid),
                onTap: () => _scope(g, providers: providers.contains(pid) ? ([...providers]..remove(pid)) : [...providers, pid]),
              ),
          ]),
        ]);
      }
    } else if (mode == 'fixed') {
      final fixed = g['fixed'] as String? ?? '';
      kids.add(navRow(l, tr('节点', 'Node'), fixed.isEmpty ? tr('未选择', 'None chosen') : _nodeDisplay(app, fixed), onTap: () => _pickFixed(g)));
    } else {
      for (final (i, n) in order.indexed) {
        kids.add(hairBottom(
          l,
          Container(
            constraints: const BoxConstraints(minHeight: 48),
            child: Row(children: [
              SizedBox(width: 22, child: Text('${i + 1}', style: l.text(13, color: l.mute, lh: 18))),
              Expanded(child: Line1(_nodeDisplay(app, n), l.text(14.5, lh: 20))),
              _small('up', tr('上移', 'Move up'), i == 0 ? null : () => _moveOrder(g, i, -1)),
              _small('down', tr('下移', 'Move down'), i == order.length - 1 ? null : () => _moveOrder(g, i, 1)),
              _small('x', tr('移除', 'Remove'), () => app.routeOp('setOrder', {'id': id, 'order': [...order]..removeAt(i)})),
            ]),
          ),
        ));
      }
      if (_nodes.any((n) => !order.contains(n['name']))) {
        kids.add(Padding(padding: const EdgeInsets.only(top: 8), child: Btn(tr('添加节点', 'Add nodes'), minH: 40, onTap: () => _addOrder(g))));
      }
    }
    return section(l, tr('节点策略', 'Node strategy'), kids, top: 8);
  }

  Widget _small(String icon, String label, VoidCallback? onTap) {
    return Builder(builder: (context) {
      final l = Skin.of(context);
      return Tap(onTap: onTap, label: label, child: SizedBox(width: 38, height: 44, child: Center(child: Ic(icon, size: 18, color: onTap == null ? l.faint : l.ink2))));
    });
  }

  void _saveTargets(List<Map> list) {
    app.send('setTargets', {'route': id, 'targets': list}).then((_) => app.refreshRouting());
  }

  Widget _sites(Look l, Map g) {
    final targets = _list(g, 'targets');
    final sites = _list(g, 'sites');
    int? delayOf(Map t) {
      for (final s in sites) {
        if (s['url'] == t['url'] && s['tested'] == true) return s['delay'] as int?;
      }
      return null;
    }

    void menu(int i) {
      final t = targets[i];
      pickSheet<String>(app, t['name'] as String, [
        if (i > 0) PickOption('main', tr('设为主要测试地址', 'Make primary test URL'), sub: tr('自动选择节点时用它判断快慢', 'Automatic selection uses it to compare speed')),
        if (targets.length > 1) PickOption('del', tr('移除', 'Remove'), danger: true),
      ], (v) {
        final next = [...targets];
        if (v == 'main') {
          next.insert(0, next.removeAt(i));
        } else {
          next.removeAt(i);
        }
        _saveTargets(next);
      });
    }

    return section(l, tr('测试地址', 'Test URLs'), [
      for (final (i, t) in targets.indexed)
        navRow(l, t['name'] as String, i == 0 ? tr('主要', 'Primary') : (() {
          final d = delayOf(t);
          return d == null ? '' : (d > 0 ? '$d ms' : tr('超时', 'Timeout'));
        })(), sub: t['url'] as String, onTap: () => menu(i)),
      Padding(
        padding: const EdgeInsets.only(top: 8),
        child: Wrap(spacing: 6, runSpacing: 6, children: [
          if (targets.length > 1)
            Btn(tr('测试这些网站', 'Test these sites'), minH: 40, onTap: _sitesBusy ? null : () async {
              setState(() => _sitesBusy = true);
              await app.send('testSites', {'route': id});
              await app.refreshRouting();
              if (mounted) setState(() => _sitesBusy = false);
            }, child: _sitesBusy ? Shim(active: true, child: Text(tr('测试中', 'Testing'), style: l.text(14, weight: 600, lh: 20))) : null),
          Btn(tr('添加', 'Add'), minH: 40, onTap: () => inputSheet(app, tr('添加测试地址', 'Add test URL'), (v) {
                if (v.isNotEmpty) _saveTargets([...targets, {'name': '', 'url': v}]);
              }, hint: 'https://…', keyboard: TextInputType.url, action: tr('添加', 'Add'))),
          Btn(tr('恢复默认', 'Reset'), minH: 40, kind: BtnKind.plain, onTap: () {
            app.send('setTargets', {'route': id, 'targets': null}).then((_) => app.refreshRouting());
          }),
        ]),
      ),
    ]);
  }

  void _addRule(Map s) {
    app.showSheet((_) => _RuleSheet(app: app, route: id, service: s['id'] as String, serviceName: s['name'] as String? ?? ''));
  }

  void _addService(Map g) {
    final nav = Navigator.of(context);
    final others = [
      for (final o in _groups(app))
        if (o['id'] != id)
          for (final s in _list(o, 'services'))
            if (s['mine'] != true) PickOption<String>(s['id'] as String, s['name'] as String, sub: o['name'] as String?),
    ];
    pickSheet<String>(app, tr('添加服务', 'Add a service'), [
      PickOption('', tr('新建服务', 'New service'), leading: Builder(builder: (c) => Ic('plus', size: 18, color: Skin.of(c).ink2))),
      ...others,
    ], (sid) async {
      if (sid.isEmpty) {
        _newService();
        return;
      }
      if (await app.routeOp('moveService', {'id': sid, 'to': id}) && nav.mounted && mounted) setState(() => _show(sid));
    }, search: others.length > 8);
  }

  void _newService() {
    inputSheet(app, tr('新建服务', 'New service'), (name) async {
      if (name.isEmpty || !await app.routeOp('newService', {'id': id, 'name': name})) return;
      final made = _list(_g, 'services').where((s) => s['name'] == name).firstOrNull;
      if (made != null && mounted) setState(() => _show(made['id'] as String));
    }, hint: tr('名称', 'Name'), action: tr('新建', 'Create'));
  }

  void _serviceMenu(Map g, Map s) {
    final nav = Navigator.of(context);
    final sid = s['id'] as String;
    final name = s['name'] as String;
    final builtin = s['builtin'] == true, mine = s['mine'] == true, changed = s['changed'] == true;
    final named = _list(g, 'services').where((o) => o['mine'] != true).length;
    pickSheet<String>(app, name, [
      if (!builtin && !mine) PickOption('rename', tr('重命名', 'Rename')),
      if (!mine) PickOption('move', tr('移到其他路由组', 'Move to another group')),
      if (!mine && g['direct'] != true && named >= 2) PickOption('split', tr('单独成组', 'Make its own group'), sub: tr('沿用现在的节点策略', 'Keeps the current node policy')),
      if (builtin && changed) PickOption('reset', tr('恢复默认', 'Reset')),
      if (!builtin && !mine) PickOption('delete', tr('删除', 'Delete'), danger: true),
    ], (v) {
      switch (v) {
        case 'rename':
          inputSheet(app, tr('重命名服务', 'Rename service'), (t) {
            if (t.isNotEmpty) app.routeOp('renameService', {'id': sid, 'name': t});
          }, initial: name);
        case 'move':
          pickSheet<String>(app, tr('移到', 'Move to'), [for (final o in _groups(app)) if (o['id'] != id) PickOption(o['id'] as String, o['name'] as String, leading: Builder(builder: (c) => Square(Skin.of(c).route(o['id'] as String))))], (to) async {
            if (await app.routeOp('moveService', {'id': sid, 'to': to})) nav.pushReplacement(routeOf(GroupPage(app: app, id: to, service: sid)));
          });
        case 'split':
          _split(nav, sid);
        case 'reset':
          app.routeOp('removeService', {'id': sid});
        case 'delete':
          confirmSheet(app, tr('删除「{0}」', 'Delete {0}', [name]), tr('删除', 'Delete'), () => app.routeOp('removeService', {'id': sid}), body: tr('它里面的规则也会一起删除。', 'Its rules are deleted with it.'));
      }
    });
  }

  Future<void> _split(NavigatorState nav, String sid) async {
    if (!await app.routeOp('splitService', {'id': sid})) return;
    for (final g in _groups(app)) {
      if (g['id'] != id && _list(g, 'services').any((s) => s['id'] == sid)) {
        nav.pushReplacement(routeOf(GroupPage(app: app, id: g['id'] as String, service: sid)));
        return;
      }
    }
  }

  Widget _ruleRow(Look l, Map s, Map r) {
    final builtin = r['builtin'] == true, off = r['off'] == true || s['off'] == true;
    return Container(
      constraints: const BoxConstraints(minHeight: 44),
      padding: const EdgeInsets.only(left: 2),
      child: Row(children: [
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Line1(r['shown'] as String? ?? '', l.text(14, color: off ? l.mute : l.ink, lh: 20)),
            Text(r['kindName'] as String? ?? '', style: l.text(12, color: l.mute, lh: 16)),
          ]),
        ),
        if (builtin)
          Toggle(on: r['off'] != true, scale: .7, label: r['shown'] as String?, onTap: () => app.routeOp('toggleRule', {'id': s['id'], 'text': r['text']}))
        else
          _small('x', tr('删除规则', 'Delete rule'), () => app.routeOp('removeRule', {'kind': r['kind'], 'text': r['value'], 'route': id})),
      ]),
    );
  }

  String _serviceSub(Map s) {
    if (s['off'] == true) return tr('已停用，这些流量回到「其他流量」', 'Off: its traffic goes back to Other');
    final live = _live(s);
    if (live.isEmpty) return tr('还没有规则', 'No rules yet');
    final first = live.take(2).map((r) => r['shown'] as String? ?? '').join(tr('、', ', '));
    final line = tr('{0} 条规则 · {1}', live.length == 1 ? '{0} rule · {1}' : '{0} rules · {1}', [live.length, first]);
    return live.length > 2 ? '$line${tr(' 等', ' and more')}' : line;
  }

  Widget _service(Look l, Map g, Map s) {
    final sid = s['id'] as String;
    final open = _openServices.contains(sid);
    final rules = _list(s, 'rules');
    final off = s['off'] == true, mine = s['mine'] == true;
    final tag = mine ? null : (s['builtin'] == true ? (s['changed'] == true ? '${tr('内置', 'Built-in')} · ${tr('已改动', 'Changed')}' : tr('内置', 'Built-in')) : tr('自定义', 'Custom'));
    return Column(key: _serviceKeys.putIfAbsent(sid, GlobalKey.new), crossAxisAlignment: CrossAxisAlignment.start, children: [
      hairBottom(
        l,
        Row(children: [
          Expanded(
            child: Tap(
              onTap: () => setState(() => open ? _openServices.remove(sid) : _openServices.add(sid)),
              child: Container(
                constraints: const BoxConstraints(minHeight: 52),
                padding: const EdgeInsets.symmetric(vertical: 6),
                child: Row(children: [
                  AnimatedRotation(turns: open ? .25 : 0, duration: const Duration(milliseconds: 240), curve: easeCard, child: Ic('chev', size: 15, color: l.mute)),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                      Row(children: [
                        Flexible(child: Line1(s['name'] as String? ?? sid, l.text(15, color: off ? l.mute : l.ink, lh: 21))),
                        if (tag != null) ...[const SizedBox(width: 6), Text(tag, maxLines: 1, style: l.text(11.5, color: l.faint, lh: 16))],
                      ]),
                      Text(_serviceSub(s), maxLines: 1, overflow: TextOverflow.ellipsis, style: l.text(12.5, color: l.mute, lh: 17)),
                    ]),
                  ),
                ]),
              ),
            ),
          ),
          if (!mine) Toggle(on: !off, scale: .82, label: s['name'] as String?, onTap: () => app.routeOp('toggleService', {'id': sid})),
        ]),
      ),
      Accordion(
        open: open,
        builder: (context) => Padding(
          padding: const EdgeInsets.fromLTRB(21, 4, 0, 10),
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            if (mine) Padding(padding: const EdgeInsets.only(bottom: 4), child: Text(tr('在活动页点开一条连接，选「让它走…」加的规则放在这里。', 'Rules made with Send via… in Activity land here.'), style: l.text(12.5, color: l.mute, lh: 17))),
            if (rules.isEmpty) Padding(padding: const EdgeInsets.symmetric(vertical: 4), child: Text(tr('还没有规则，在下面加一条。', 'No rules yet. Add one below.'), style: l.text(13, color: l.faint, lh: 18))),
            for (final r in rules) _ruleRow(l, s, r),
            const SizedBox(height: 6),
            Row(children: [
              Btn(tr('添加规则', 'Add rule'), minH: 36, fontSize: 13, onTap: () => _addRule(s)),
              const SizedBox(width: 6),
              if (!mine) Btn(tr('更多', 'More'), minH: 36, fontSize: 13, padH: 0, kind: BtnKind.plain, onTap: () => _serviceMenu(g, s)),
            ]),
          ]),
        ),
      ),
    ]);
  }

  void _groupMenu(BuildContext context, Map g) {
    final name = g['name'] as String;
    final nav = Navigator.of(context);
    pickSheet<String>(app, name, [
      PickOption('rename', tr('重命名', 'Rename')),
      PickOption('delete', tr('删除路由组', 'Delete route group'), danger: true),
    ], (v) {
      if (v == 'rename') {
        inputSheet(app, tr('重命名路由组', 'Rename route group'), (t) {
          if (t.isNotEmpty) app.routeOp('renameGroup', {'id': id, 'name': t});
        }, initial: name);
        return;
      }
      pickSheet<String>(app, tr('「{0}」里的服务移到', 'Move the services in {0} to', [name]), [for (final o in _groups(app)) if (o['id'] != id && o['reject'] != true) PickOption(o['id'] as String, o['name'] as String)], (to) async {
        if (await app.routeOp('deleteGroup', {'id': id, 'to': to})) nav.maybePop();
      });
    });
  }

  /// 我的规则 with rules first, the services, then an empty 我的规则.
  List<Map> _sorted(Map g) {
    int at(Map s) => s['mine'] != true ? 1 : (_list(s, 'rules').isNotEmpty ? 0 : 2);
    final list = [..._list(g, 'services')];
    final index = {for (final (i, s) in list.indexed) s: i};
    list.sort((a, b) {
      final c = at(a) - at(b);
      return c != 0 ? c : index[a]! - index[b]!;
    });
    return list;
  }

  Widget _note(Look l, String text) => Container(
        width: double.infinity,
        margin: const EdgeInsets.fromLTRB(16, 8, 16, 0),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
        decoration: BoxDecoration(color: l.paper2, borderRadius: l.br(10)),
        child: Text(text, style: l.text(13, color: l.ink2, lh: 18)),
      );

  String _countLine(Map g) {
    final services = _list(g, 'services');
    final named = services.where((s) => s['mine'] != true && s['off'] != true).length;
    final live = services.fold(0, (n, s) => n + _live(s).length);
    final pct = ((app.todayShare[id] ?? 0) * 100).round();
    if (g['reject'] == true) return tr('{0} 个服务 · {1} 条规则', '{0} services · {1} rules', [named, live]);
    if (id == 'default') return tr('今日 {0}% 的流量', "{0}% of today's traffic", [pct]);
    return tr('{0} 个服务 · {1} 条规则 · 今日 {2}% 的流量', "{0} services · {1} rules · {2}% of today's traffic", [named, live, pct]);
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: app,
      builder: (context, _) {
        final l = Skin.of(context);
        final g = _g;
        if (g == null) return SubPage(title: tr('路由组', 'Route groups'), children: const []);
        _scrollWhenReady(g);
        final direct = g['direct'] == true, reject = g['reject'] == true;
        final canEdit = id != 'default' && !direct;
        final named = _list(g, 'services').where((s) => s['mine'] != true).length;
        return SubPage(
          title: g['name'] as String? ?? id,
          eager: true,
          actions: [
            Btn(tr('添加服务', 'Add a service'), minH: 44, onTap: () => _addService(g)),
            if (canEdit) ...[const SizedBox(width: 8), IconBox('more', label: tr('更多', 'More'), onTap: () => _groupMenu(context, g))],
          ],
          children: [
            Padding(padding: const EdgeInsets.fromLTRB(16, 2, 16, 0), child: Text(_countLine(g), style: l.text(13, color: l.mute, lh: 18))),
            if (reject)
              _note(l, tr('命中这里规则的连接会被拒绝，不经过节点，所以没有节点策略。', "Connections matching the rules here are refused, with no node, so there's no node strategy."))
            else if (direct)
              _note(l, tr('这里的流量直接连接，不经过节点，所以没有节点策略。', "Traffic here connects directly, with no node, so there's no node strategy."))
            else
              _strategy(l, g, _cfgOf(app, id)),
            if (id == 'default') _note(l, tr('没命中任何规则的流量走这里。想让某个网站固定走这里，可以在这里加一条规则。', 'Traffic no rule matched goes here. To keep a site here on purpose, add a rule here.')),
            section(l, tr('服务 · {0}', 'Services · {0}', [named]), [for (final s in _sorted(g)) _service(l, g, s)]),
            if (!direct) _sites(l, g),
          ],
        );
      },
    );
  }
}

/// Adds a rule to a service: the kind can be left to be worked out, and what
/// will be added shows as it's typed. It stays up when the rule is refused.
class _RuleSheet extends StatefulWidget {
  const _RuleSheet({required this.app, required this.route, required this.service, required this.serviceName});
  final AppState app;
  final String route, service, serviceName;

  @override
  State<_RuleSheet> createState() => _RuleSheetState();
}

class _RuleSheetState extends State<_RuleSheet> {
  final _c = TextEditingController();
  String _kind = ''; // worked out from what's typed
  Map? _preview;
  String? _error, _conflict;
  bool _busy = false;
  int _asked = 0;

  Map<String, Object?> get _args => {'kind': _kind, 'text': _c.text.trim(), 'route': widget.route, 'service': widget.service};

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  Future<void> _changed() async {
    final text = _c.text.trim();
    final ask = ++_asked;
    if (text.isEmpty) {
      setState(() {
        _preview = _error = _conflict = null;
      });
      return;
    }
    final out = await widget.app.previewRule(_kind, text);
    if (!mounted || ask != _asked) return;
    setState(() {
      _preview = out?['error'] == null ? out : null;
      _error = out?['error'] as String?;
      _conflict = null;
    });
  }

  Future<void> _save() async {
    if (_busy || _c.text.trim().isEmpty) return;
    setState(() => _busy = true);
    final out = await widget.app.addServiceRule(_args);
    if (!mounted) return;
    if (out.conflict == null && out.error == null) {
      widget.app.closeSheet();
      return;
    }
    setState(() {
      _busy = false;
      _conflict = out.conflict;
      _error = out.error;
    });
  }

  Future<void> _move() async {
    final args = _args;
    setState(() => _busy = true);
    await widget.app.routeOp('moveRule', args);
    if (mounted) widget.app.closeSheet();
  }

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final kinds = _list(widget.app.routing, 'kinds');
    Widget chip(String kind, String name) => Padding(
          padding: const EdgeInsets.only(right: 6),
          child: Chip(
            text: name,
            on: _kind == kind,
            onTap: () {
              _kind = kind;
              _changed();
            },
          ),
        );
    final conflict = _conflict;
    return Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      Text(tr('添加规则', 'Add rule'), style: l.text(15, weight: 700, lh: 21)),
      Text(widget.serviceName, maxLines: 1, overflow: TextOverflow.ellipsis, style: l.text(12.5, color: l.mute, lh: 17)),
      const SizedBox(height: 8),
      if (conflict != null)
        Padding(
          padding: const EdgeInsets.only(bottom: 8),
          child: Row(children: [
            Expanded(child: Text(tr('{0} 已经在 {1} 里，一条规则只能属于一个服务。', '{0} is already in {1}; a rule belongs to one service.', [_preview?['value'] ?? _c.text.trim(), conflict]), style: l.text(13, color: l.warnT, lh: 18))),
            const SizedBox(width: 8),
            Btn(tr('移过来', 'Move here'), minH: 36, padH: 10, fontSize: 13, onTap: _busy ? null : _move),
          ]),
        )
      else
        Padding(
          padding: const EdgeInsets.only(bottom: 8),
          child: Text(
            _error ?? (_preview != null ? tr('添加后：{0} · {1}', 'Will add: {0} · {1}', [_preview!['value'], _preview!['kindName']]) : tr('粘贴链接也可以，会自动提取域名。', 'A pasted link works too: the domain is taken from it.')),
            style: l.text(13, color: _error != null ? l.signalT : (_preview != null ? l.ink2 : l.mute), lh: 18),
          ),
        ),
      SizedBox(
        height: 36,
        child: ListView(scrollDirection: Axis.horizontal, children: [
          chip('', tr('自动识别', 'Detect')),
          for (final k in kinds) chip(k['id'] as String, k['name'] as String),
        ]),
      ),
      const SizedBox(height: 8),
      Container(
        constraints: const BoxConstraints(minHeight: 44),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 11),
        decoration: BoxDecoration(color: l.paper2, borderRadius: l.br(11)),
        child: BareField(hint: tr('域名、IP 或链接', 'A domain, IP or link'), controller: _c, autofocus: true, keyboardType: TextInputType.url, onChanged: (_) => _changed(), onSubmitted: (_) => _save()),
      ),
      const SizedBox(height: 10),
      Btn(tr('添加', 'Add'), kind: BtnKind.accent, expand: true, minH: 44, onTap: _busy ? null : _save),
    ]);
  }
}

/// 我的规则: what the user added, across the groups.
class MyRulesPage extends StatefulWidget {
  const MyRulesPage({super.key, required this.app});
  final AppState app;

  @override
  State<MyRulesPage> createState() => _MyRulesPageState();
}

class _MyRulesPageState extends State<MyRulesPage> {
  @override
  void initState() {
    super.initState();
    widget.app.refreshRouting();
  }

  @override
  Widget build(BuildContext context) {
    final app = widget.app;
    return ListenableBuilder(
      listenable: app,
      builder: (context, _) {
        final l = Skin.of(context);
        final rows = <Widget>[];
        for (final g in _groups(app)) {
          for (final s in _list(g, 'services')) {
            for (final r in _list(s, 'rules')) {
              if (r['builtin'] == true) continue;
              rows.add(hairBottom(
                l,
                Container(
                  constraints: const BoxConstraints(minHeight: 52),
                  padding: const EdgeInsets.symmetric(vertical: 6),
                  child: Row(children: [
                    Square(l.route(g['id'] as String)),
                    const SizedBox(width: 10),
                    Expanded(child: rowLabel(l, r['shown'] as String? ?? '', '${r['kindName']} · ${g['name']}${s['mine'] == true ? '' : ' · ${s['name']}'}')),
                    Tap(
                      label: tr('删除规则', 'Delete rule'),
                      onTap: () => confirmSheet(app, tr('删除这条规则', 'Delete this rule'), tr('删除', 'Delete'), () => app.routeOp('removeRule', {'kind': r['kind'], 'text': r['value'], 'route': g['id']}), body: r['shown'] as String?),
                      child: SizedBox(width: 40, height: 44, child: Center(child: Ic('x', size: 18, color: l.ink2))),
                    ),
                  ]),
                ),
              ));
            }
          }
        }
        return SubPage(
          title: tr('我的规则', 'My rules'),
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: rows.isEmpty
                  ? Padding(padding: const EdgeInsets.only(top: 24), child: Text(tr('还没有规则。在活动页点开一条连接，可以让它走指定路由。', 'No rules yet. Open a connection in Activity to send it through a chosen route.'), style: l.text(14, color: l.mute, lh: 20)))
                  : Column(children: rows),
            ),
          ],
        );
      },
    );
  }
}

/// Pushes a page in 设置's own stack, so swiping back works.
void pushPage(BuildContext context, Widget page) {
  Navigator.of(context).push(routeOf(page));
}

PageRoute<void> routeOf(Widget page) => PageRouteBuilder<void>(
      transitionDuration: const Duration(milliseconds: 320),
      reverseTransitionDuration: const Duration(milliseconds: 260),
      pageBuilder: (_, _, _) => page,
      transitionsBuilder: (_, a, _, child) => SlideTransition(
        position: Tween(begin: const Offset(1, 0), end: Offset.zero).animate(CurvedAnimation(parent: a, curve: easeCard)),
        child: child,
      ),
    );
