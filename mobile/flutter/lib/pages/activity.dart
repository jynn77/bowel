import 'package:flutter/services.dart' show Clipboard, ClipboardData;
import 'package:flutter/widgets.dart';

import '../app/i18n.dart';
import '../app/sample.dart';
import '../app/state.dart';
import '../ui/atoms.dart';
import '../ui/skin.dart';
import 'frame.dart';
import 'sheet.dart';

class ActivityPage extends StatefulWidget {
  const ActivityPage({super.key, required this.app});

  final AppState app;

  @override
  State<ActivityPage> createState() => _ActivityPageState();
}

class _ActivityPageState extends State<ActivityPage> {
  String _query = '';

  bool _matches(Conn c) {
    for (final term in _query.trim().toLowerCase().split(RegExp(r'\s+'))) {
      if (term.isEmpty) continue;
      final at = term.indexOf(':');
      final key = at > 0 ? term.substring(0, at) : '', v = at > 0 ? term.substring(at + 1) : term;
      if (v.isEmpty) continue;
      final hit = switch (key) {
        'host' => c.host.toLowerCase().contains(v),
        'route' => c.routeName.toLowerCase().contains(v),
        'node' => c.node.toLowerCase().contains(v),
        _ => '${c.host} ${c.app} ${c.routeName} ${c.node}'.toLowerCase().contains(v),
      };
      if (!hit) return false;
    }
    return true;
  }

  @override
  Widget build(BuildContext context) {
    final app = widget.app;
    final l = Skin.of(context);
    final shown = _filtered(app);
    final blocked = conns.where((c) => c.status == 'blocked').length;
    Widget gap(Widget c) => Padding(padding: const EdgeInsets.only(right: 6), child: c);
    final routeName = app.routeFilter.isEmpty ? tr('路由', 'Route') : app.ruleTargets.where((r) => r.id == app.routeFilter).map((r) => r.name).firstOrNull ?? tr('路由', 'Route');
    final sortName = switch (app.sortBy) { 'speed' => tr('按速度', 'By speed'), 'total' => tr('按流量', 'By traffic'), _ => tr('按时间', 'By time') };
    final chips = app.showLog
        ? [
            for (final (v, t) in [('all', tr('全部', 'All')), ('warn', tr('警告', 'Warnings'))]) gap(Chip(text: t, on: app.logFilter == v, onTap: () => app.set(() => app.logFilter = v))),
            gap(Chip(text: tr('复制日志', 'Copy logs'), on: false, leading: Ic('copy', size: 15, color: l.ink2), onTap: () => _copyLogs(app))),
          ]
        : [
            for (final (v, t) in [('all', tr('全部', 'All')), ('active', tr('活跃', 'Active')), ('closed', tr('已关闭', 'Closed')), ('failed', tr('失败', 'Failed'))]) gap(Chip(text: t, on: app.filter == v, onTap: () => app.set(() => app.filter = v))),
            if (blocked > 0 || app.filter == 'blocked') gap(Chip(text: tr('已拦截', 'Blocked'), count: blocked, on: app.filter == 'blocked', onTap: () => app.set(() => app.filter = 'blocked'))),
            gap(Chip(text: routeName, on: app.routeFilter.isNotEmpty, onTap: () => _pickRoute(app))),
            gap(Chip(text: app.netFilter.isEmpty ? tr('协议', 'Protocol') : app.netFilter.toUpperCase(), on: app.netFilter.isNotEmpty, onTap: () => _pickNet(app))),
            gap(Chip(text: sortName, on: app.sortBy != 'time', onTap: () => _pickSort(app))),
            if (shown.any((e) => e.$2.status == 'active'))
              gap(Chip(text: tr('断开这些', 'Disconnect these'), on: false, count: shown.where((e) => e.$2.status == 'active').length, onTap: () => _closeShown(app, shown))),
          ];
    final dock = Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      SizedBox(height: 36, child: ListView(scrollDirection: Axis.horizontal, children: chips)),
      const SizedBox(height: 8),
      Row(children: [
        Seg<bool>(padH: 12, options: [(false, tr('连接', 'Connections')), (true, tr('日志', 'Logs'))], value: app.showLog, onChanged: (v) => app.set(() => app.showLog = v)),
        const SizedBox(width: 8),
        Expanded(child: SearchField(key: ValueKey(app.showLog), hint: app.showLog ? tr('搜索日志', 'Search logs') : 'host:  route:  node:', label: tr('搜索', 'Search'), onChanged: (v) => setState(() => _query = v))),
        const SizedBox(width: 8),
        IconBox(app.paused ? 'play' : 'pause', on: app.paused, label: app.paused ? tr('继续实时刷新', 'Resume live updates') : tr('暂停实时刷新', 'Pause live updates'), onTap: () => app.set(() => app.paused = !app.paused)),
      ]),
    ]);
    return ListPage(
      title: tr('活动', 'Activity'),
      dock: dock,
      dockH: 116,
      list: (bottom) => app.showLog ? _logs(l, app, bottom) : _conns(l, app, shown, bottom),
    );
  }

  List<(int, Conn)> _filtered(AppState app) {
    final rows = [
      for (final (i, c) in conns.indexed)
        if ((app.filter == 'all' || c.status == app.filter) && (app.routeFilter.isEmpty || c.route == app.routeFilter) && (app.netFilter.isEmpty || c.net.toLowerCase() == app.netFilter) && _matches(c)) (i, c),
    ];
    if (app.sortBy == 'speed') rows.sort((a, b) => b.$2.speed.compareTo(a.$2.speed));
    if (app.sortBy == 'total') rows.sort((a, b) => b.$2.bytes.compareTo(a.$2.bytes));
    return rows;
  }

  void _pickRoute(AppState app) => pickSheet<String>(app, tr('只看这条路由', 'Show only this route'), [
        PickOption('', tr('全部路由', 'All routes')),
        for (final r in app.ruleTargets) PickOption(r.id, r.name, leading: Builder(builder: (c) => Square(Skin.of(c).route(r.id)))),
      ], (v) => app.set(() => app.routeFilter = v), value: app.routeFilter);

  void _pickNet(AppState app) => pickSheet<String>(app, tr('协议', 'Protocol'), [PickOption('', tr('全部', 'All')), PickOption('tcp', 'TCP'), PickOption('udp', 'UDP')], (v) => app.set(() => app.netFilter = v), value: app.netFilter);

  void _pickSort(AppState app) => pickSheet<String>(app, tr('排序', 'Sort'), [PickOption('time', tr('按时间', 'By time')), PickOption('speed', tr('按速度', 'By speed')), PickOption('total', tr('按流量', 'By traffic'))], (v) => app.set(() => app.sortBy = v), value: app.sortBy);

  void _closeShown(AppState app, List<(int, Conn)> shown) {
    final list = [for (final e in shown) if (e.$2.status == 'active') e.$2];
    confirmSheet(app, tr('断开这 {0} 条连接', list.length == 1 ? 'Disconnect this connection' : 'Disconnect these {0} connections', [list.length]), tr('断开', 'Disconnect'), () => app.closeMany(list));
  }

  List<(String, String, String)> _shownLogs(AppState app) {
    final q = _query.trim().toLowerCase();
    return [
      for (final e in logs)
        if ((app.logFilter == 'all' || e.$2 == app.logFilter) && (q.isEmpty || e.$3.toLowerCase().contains(q))) e,
    ];
  }

  void _copyLogs(AppState app) {
    Clipboard.setData(ClipboardData(text: [for (final (t, level, x) in _shownLogs(app)) '$t [$level] $x'].join('\n')));
    app.say(tr('已复制', 'Copied'));
  }

  Widget _logs(Look l, AppState app, double bottom) {
    return ListView(padding: EdgeInsets.only(bottom: bottom), keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag, children: [
      for (final (t, level, x) in _shownLogs(app))
        hairBottom(
          l,
          Container(
            constraints: const BoxConstraints(minHeight: 46),
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
            child: Row(children: [
              Text(t, style: l.text(13.5, color: l.mute, lh: 20)),
              const SizedBox(width: 11),
              Expanded(child: Text(x, style: l.text(13.5, color: level == 'warn' ? l.warnT : l.ink, lh: 20))),
            ]),
          ),
        ),
    ]);
  }

  Widget _conns(Look l, AppState app, List<(int, Conn)> rows, double bottom) {
    return ListView(padding: EdgeInsets.only(bottom: bottom), keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag, children: [
      for (final (i, c) in rows) _ConnRow(key: ValueKey(i), app: app, index: i, c: c),
      Padding(padding: const EdgeInsets.all(12), child: Text(tr('{0} 条连接', stats.active == 1 ? '{0} connection' : '{0} connections', [stats.active]), textAlign: TextAlign.center, style: l.text(12.5, color: l.mute, lh: 18))),
    ]);
  }
}

/// The 规则 line of an opened connection, with a way to where that rule lives.
class _RuleKv extends StatefulWidget {
  const _RuleKv({required this.app, required this.c});
  final AppState app;
  final Conn c;

  @override
  State<_RuleKv> createState() => _RuleKvState();
}

class _RuleKvState extends State<_RuleKv> {
  ({String group, String service})? _home;

  @override
  void initState() {
    super.initState();
    _look();
  }

  @override
  void didUpdateWidget(_RuleKv old) {
    super.didUpdateWidget(old);
    if (old.c.ruleType != widget.c.ruleType || old.c.payload != widget.c.payload) _look();
  }

  Future<void> _look() async {
    final c = widget.c;
    if (c.ruleType.isEmpty || c.ruleType == 'Match') {
      if (_home != null) setState(() => _home = null);
      return;
    }
    final home = await widget.app.ruleHome(c.ruleType, c.payload);
    if (mounted) setState(() => _home = home);
  }

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final home = _home;
    return hairBottom(
      l,
      Padding(
        padding: const EdgeInsets.symmetric(vertical: 10),
        child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
          SizedBox(width: 72, child: Text(tr('规则', 'Rule'), style: l.text(14, color: l.mute, lh: 20))),
          const SizedBox(width: 14),
          Expanded(child: Text(widget.c.rule, style: l.text(14, lh: 20))),
          if (home != null)
            Tap(
              label: tr('查看这条规则', 'View this rule'),
              onTap: () => widget.app.showService(home.group, home.service),
              child: Padding(padding: const EdgeInsets.only(left: 12), child: Text(tr('查看', 'View'), style: l.text(14, weight: 600, lh: 20).copyWith(decoration: TextDecoration.underline))),
            ),
        ]),
      ),
    );
  }
}

class _ConnRow extends StatelessWidget {
  const _ConnRow({super.key, required this.app, required this.index, required this.c});

  final AppState app;
  final int index;
  final Conn c;

  Widget _kv(Look l, String a, String b) => hairBottom(
        l,
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 10),
          child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
            SizedBox(width: 72, child: Text(a, style: l.text(14, color: l.mute, lh: 20))),
            const SizedBox(width: 14),
            Expanded(child: Text(b, style: l.text(14, lh: 20))),
          ]),
        ),
      );

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final open = app.openConn == index;
    final related = [for (final e in logs) if (e.$3.contains(c.host)) e];
    final failed = c.status == 'failed', closed = c.status == 'closed', blocked = c.status == 'blocked';
    final sub = l.text(12.5, color: l.mute, lh: 18);
    final parts = c.host.split('.');
    final site = parts.length > 1 ? parts.sublist(parts.length - 2).join('.') : c.host;
    final head = Tap(
      onTap: () => app.set(() => app.openConn = open ? null : index),
      child: Container(
        constraints: const BoxConstraints(minHeight: 62),
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
        decoration: BoxDecoration(color: open ? l.hover : null, border: Border(bottom: BorderSide(color: open ? const Color(0x00000000) : l.hair2))),
        child: Row(children: [
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Line1(c.host, l.text(15, weight: 600, color: failed ? l.signalT : l.ink, lh: 21)),
              Row(children: [
                Square(l.route(c.route), size: 8, radius: 2),
                const SizedBox(width: 4),
                Flexible(child: Line1('${c.routeName}${c.node.isEmpty ? '' : ' · ${c.node}'}', sub)),
              ]),
            ]),
          ),
          const SizedBox(width: 11),
          Column(crossAxisAlignment: CrossAxisAlignment.end, children: [
            Text(c.rate, maxLines: 1, style: l.text(13.5, color: failed ? l.signalT : (blocked ? l.warnT : (closed ? l.mute : l.ink)), lh: 20)),
            if (c.dur.isNotEmpty) Text(c.dur, maxLines: 1, style: l.text(12, color: l.mute, lh: 17)),
          ]),
        ]),
      ),
    );
    return Column(children: [
      head,
      Accordion(
        open: open,
        builder: (context) => Container(
          width: double.infinity,
          color: l.hover,
          padding: const EdgeInsets.fromLTRB(16, 2, 16, 14),
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            if (c.err != null)
              Container(
                width: double.infinity,
                margin: const EdgeInsets.symmetric(vertical: 6),
                padding: const EdgeInsets.fromLTRB(12, 9, 8, 9),
                decoration: BoxDecoration(color: Color.lerp(l.paper, l.signal, .13), borderRadius: l.br(10)),
                child: Row(children: [
                  Text(tr('失败', 'Failed'), style: l.text(13.5, weight: 700, color: l.signalT, lh: 19)),
                  const SizedBox(width: 10),
                  Expanded(child: Text(c.err!, style: l.text(13.5, lh: 19))),
                ]),
              ),
            _kv(l, tr('目标', 'Target'), '${c.ip.isEmpty ? c.host : c.ip}:${c.port} · ${c.net}'),
            _RuleKv(app: app, c: c),
            _kv(l, tr('路由', 'Route'), '${c.routeName}${c.node.isEmpty ? '' : ' → ${c.node}'}'),
            _kv(l, tr('流量', 'Traffic'), c.total),
            const SizedBox(height: 12),
            eyebrow(l, tr('让 {0} 走…', 'Send {0} via…', [site])),
            const SizedBox(height: 7),
            Wrap(spacing: 6, runSpacing: 6, children: [
              for (final r in app.ruleTargets)
                Btn(r.name, minH: 36, leading: Square(l.route(r.id), size: 8, radius: 2), onTap: () => app.addRule(r)),
            ]),
            if (related.isNotEmpty) ...[
              const SizedBox(height: 12),
              eyebrow(l, tr('相关日志', 'Related logs')),
              for (final (t, level, x) in related.reversed.take(4))
                Padding(padding: const EdgeInsets.only(top: 4), child: Text('$t  $x', style: l.text(12.5, color: level == 'warn' ? l.warnT : l.ink2, lh: 17))),
            ],
            const SizedBox(height: 6),
            Row(children: [
              if (!blocked) Btn(tr('断开这条连接', 'Disconnect'), kind: BtnKind.danger, padH: 0, onTap: () => app.closeConn(c)),
              const Spacer(),
              Btn(tr('复制域名', 'Copy domain'), kind: BtnKind.plain, padH: 0, onTap: () {
                Clipboard.setData(ClipboardData(text: c.host));
                app.say(tr('已复制', 'Copied'));
              }),
            ]),
          ]),
        ),
      ),
    ]);
  }
}
