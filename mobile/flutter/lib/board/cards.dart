import 'dart:io' show Platform;

import 'package:flutter/widgets.dart';

import '../app/i18n.dart';
import '../app/sample.dart';
import '../app/state.dart';
import '../pages/sheet.dart' show PickOption, pickSheet;
import '../ui/atoms.dart';
import '../ui/skin.dart';
import 'fly.dart';

final bool _android = Platform.isAndroid;

Widget cardView(BuildContext context, AppState s, String id, String kind, String variant) {
  return switch (id) {
    'map' => _MapCard(s, kind),
    'speed' => _SpeedCard(s, kind, variant),
    'apps' => _AppsCard(s, kind, variant),
    'routes' => _RoutesCard(s, kind),
    'mode' => _ModeCard(s, kind, variant),
    _ => _HeroCard(s, kind, variant),
  };
}

// ---- pieces shared by the cards ----

FlyTag _flyText(String id, String text, TextStyle style) => FlyTag(id: id, text: text, style: style, build: (s) => Line1(text, s!, clip: true));

class ExitName extends StatelessWidget {
  const ExitName(this.node, this.style, {super.key});
  final Node node;
  final TextStyle style;

  @override
  Widget build(BuildContext context) {
    final fs = style.fontSize!;
    return Row(mainAxisSize: MainAxisSize.min, children: [
      FlagDot(node.flag, fs * .92),
      SizedBox(width: fs * .3),
      Flexible(child: Line1(node.name, style)),
    ]);
  }
}

FlyTag _flyExit(String id, Node n, TextStyle style) => FlyTag(id: id, text: n.name, style: style, build: (s) => ExitName(n, s!));

Widget _pad(List<Widget> kids, {double bottom = 8}) => Padding(
      padding: EdgeInsets.fromLTRB(16, 10, 16, bottom),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: kids),
    );

class _Hairline extends StatelessWidget {
  const _Hairline({required this.child, this.top = true});
  final Widget child;
  final bool top;

  @override
  Widget build(BuildContext context) {
    final side = BorderSide(color: Skin.of(context).hair2);
    return DecoratedBox(decoration: BoxDecoration(border: top ? Border(top: side) : Border(bottom: side)), child: child);
  }
}

/// A squeezed card: as much of it as its new size has room for.
/// Text room inside the squeezed 118 pt column, whose padding is 16 + 12.
const _columnRoom = 90.0;

/// The largest size at which [text] fits the column, so a value such as
/// "↓ 12.54 MB/s" shrinks instead of being cut off.
double _columnFit(Look l, String text) {
  for (var size = 17.0; size > 12; size -= .5) {
    final p = TextPainter(
      text: TextSpan(text: text, style: l.text(size, weight: 600, display: true, lh: 23)),
      textDirection: TextDirection.ltr,
      maxLines: 1,
    )..layout();
    final width = p.width;
    p.dispose();
    if (width <= _columnRoom) return size;
  }
  return 12;
}

class _Compact extends StatelessWidget {
  const _Compact({required this.id, required this.variant, required this.kicker, required this.value, this.flyKey, this.sub, this.list = const [], this.valueNode, this.shim = false});

  final String id, variant, kicker, value;
  final String? flyKey;
  final Widget? sub;
  final List<Widget> list;
  final Node? valueNode;
  final bool shim;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final v = variant;
    final k = _flyText('k-$id', kicker, l.text(11.5, weight: 500, color: l.mute, lh: 15, ls: .06));
    final column = v == 'column';
    final text = value;
    final style = l.text(column && valueNode == null ? _columnFit(l, text) : (column ? 17 : 15), weight: 600, display: true, lh: column ? 23 : 20);
    Widget val;
    if (valueNode != null && flyKey != null) {
      val = Shim(active: shim, child: _flyExit(flyKey!, valueNode!, style));
    } else if (flyKey != null) {
      val = _flyText(flyKey!, text, style);
    } else {
      val = column ? Text(text, style: style) : Line1(text, style, clip: true);
    }
    if (v == 'strip') {
      return Padding(
        padding: const EdgeInsets.fromLTRB(16, 0, 12, 0),
        child: Column(mainAxisAlignment: MainAxisAlignment.center, crossAxisAlignment: CrossAxisAlignment.start, children: [
          Unroll(child: k),
          Row(children: [
            Expanded(child: Align(alignment: Alignment.centerLeft, child: Unroll(child: val))),
            if (sub != null) Padding(padding: const EdgeInsets.only(left: 8, right: 4), child: Unroll(step: 2, child: sub!)),
          ]),
        ]),
      );
    }
    return Padding(
      padding: EdgeInsets.fromLTRB(16, column ? 12 : 9, 12, 8),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Unroll(child: k),
        const SizedBox(height: 1),
        Flexible(child: Unroll(step: 2, child: val)),
        if (sub != null && v != 'mini') ...[const SizedBox(height: 1), Unroll(step: 4, child: sub!)],
        if (list.isNotEmpty && (column || v == 'wide')) ...[
          const SizedBox(height: 12),
          for (final (i, w) in list.indexed)
            Padding(padding: EdgeInsets.only(bottom: i == list.length - 1 ? 0 : 10), child: Unroll(step: 4 + i * 2, child: w)),
        ],
      ]),
    );
  }
}

/// Header reads, footer acts: everything you press in an open card is at the bottom.
class _Detail extends StatefulWidget {
  const _Detail({required this.s, required this.flyId, required this.kicker, required this.title, required this.body, this.footer = const [], this.above, this.below});

  final AppState s;
  final String flyId;
  final Widget kicker, title, body;
  final List<Widget> footer;
  final Widget? above, below;

  @override
  State<_Detail> createState() => _DetailState();
}

class _DetailState extends State<_Detail> {
  double _pull = 0;

  @override
  Widget build(BuildContext context) {
    return Column(children: [
      GestureDetector(
        behavior: HitTestBehavior.opaque,
        onVerticalDragStart: (_) => _pull = 0,
        onVerticalDragUpdate: (d) => _pull += d.delta.dy,
        onVerticalDragEnd: (_) {
          if (_pull > 32) widget.s.collapse();
        },
        child: Container(
          constraints: const BoxConstraints(minHeight: 58),
          padding: const EdgeInsets.fromLTRB(16, 8, 6, 8),
          alignment: Alignment.centerLeft,
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [widget.kicker, Unroll(child: widget.title)]),
        ),
      ),
      Expanded(child: widget.body),
      ?widget.above,
      _Foot(s: widget.s, children: widget.footer),
      ?widget.below,
    ]);
  }
}

class _Foot extends StatelessWidget {
  const _Foot({required this.s, required this.children});
  final AppState s;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return _Hairline(
      child: Container(
        constraints: const BoxConstraints(minHeight: 62),
        padding: const EdgeInsets.fromLTRB(16, 8, 12, 10),
        child: Row(children: [
          // A long language pans here rather than running under the close button.
          Expanded(
            child: SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(children: [
                for (final (i, c) in children.indexed) ...[if (i > 0) const SizedBox(width: 8), Unroll(step: i * 2, child: c)],
              ]),
            ),
          ),
          const SizedBox(width: 8),
          Unroll(step: children.length * 2, child: IconBox('x', onTap: s.collapse, label: tr('收起', 'Collapse'))),
        ]),
      ),
    );
  }
}

Widget _detKicker(Look l, String id, String text) => _flyText('k-$id', text, l.text(11.5, weight: 500, color: l.mute, lh: 15, ls: .06));
TextStyle _h3(Look l) => l.text(21, weight: 700, display: true, lh: 27);

Widget _noteF(Look l, String text) => Line1(text, l.text(13, color: l.mute, lh: 18), clip: true);

/// The group's node strategy. The footer has no room for all three at once on
/// a phone, so it shows the current one and asks in a sheet.
Widget _strategyBtn(AppState s, RouteCfg r) => Builder(builder: (context) {
      final l = Skin.of(context);
      final name = switch (r.policy) { 'order' => tr('备用', 'Backup'), 'fixed' => tr('固定', 'Fixed'), _ => tr('自动', 'Auto') };
      return Btn(
        name,
        minH: 44,
        padH: 12,
        fontSize: 13.5,
        onTap: () => pickSheet<String>(
          s,
          tr('节点策略', 'Node strategy'),
          [
            PickOption('auto', tr('自动最快', 'Fastest')),
            PickOption('order', tr('顺序备用', 'Backup order')),
            PickOption('fixed', tr('固定节点', 'Fixed node')),
          ],
          (v) => s.setStrategy(r.id, v),
          value: r.policy == 'order' || r.policy == 'fixed' ? r.policy : 'auto',
        ),
        child: Row(mainAxisSize: MainAxisSize.min, children: [
          Text(name, maxLines: 1, softWrap: false, style: l.text(13.5, weight: 600, lh: 13.5 * 1.4)),
          const SizedBox(width: 3),
          Ic('down', size: 14, color: l.mute),
        ]),
      );
    });

// ---- map ----

class _StatePill extends StatelessWidget {
  const _StatePill(this.s);
  final AppState s;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    Widget text(String t) => Text(t, style: l.text(14, weight: 600, lh: 20));
    final List<Widget> kids;
    if (s.pending == 'take') {
      kids = [Dot(l.mute), const SizedBox(width: 7), Shim(active: true, child: text(s.on ? tr('正在断开', 'Disconnecting') : tr('正在接管', 'Connecting')))];
    } else if (s.issue) {
      kids = [Dot(l.signal), const SizedBox(width: 7), text(tr('异常', 'Problem'))];
    } else if (!s.on) {
      kids = [Dot(l.mute), const SizedBox(width: 7), text(tr('已关闭', 'Off'))];
    } else {
      final m = s.mode == 'rule' ? tr('规则', 'Rules') : (s.mode == 'global' ? tr('全局', 'Global') : tr('直连', 'Direct'));
      kids = [Dot(l.good), const SizedBox(width: 7), text(tr('正常', 'OK')), const SizedBox(width: 7), Text('VPN · $m', style: l.text(12.5, color: l.mute, lh: 18))];
    }
    return Container(
      height: 30,
      padding: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(color: l.paper2, borderRadius: l.br(15)),
      child: Row(mainAxisSize: MainAxisSize.min, children: kids),
    );
  }
}

class _MapCard extends StatelessWidget {
  const _MapCard(this.s, this.kind);
  final AppState s;
  final String kind;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    if (kind != 'expanded') {
      return Stack(children: [Positioned(left: 12, top: 8, child: _StatePill(s))]);
    }
    final px = s.proxied;
    return Stack(children: [
      Positioned(
        left: 16,
        top: 10,
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text(tr('出口地图', 'Exit map'), style: l.text(11.5, color: l.mute, lh: 15, ls: .06)),
          Unroll(child: Text(tr('{0} 条线路', px.length == 1 ? '{0} line' : '{0} lines', [px.length]), style: _h3(l))),
        ]),
      ),
      Positioned(
        left: 12,
        right: 12,
        bottom: 10,
        child: Column(children: [
          for (final (i, r) in px.indexed) ...[
            Unroll(
              step: (i + 1) * 2,
              child: Tap(
                onTap: () => s.openSub(r.id),
                child: Container(
                  constraints: const BoxConstraints(minHeight: 44),
                  padding: const EdgeInsets.symmetric(horizontal: 12),
                  decoration: BoxDecoration(
                    color: l.sheet.withValues(alpha: .9),
                    borderRadius: l.br(11),
                    border: Border.all(color: l.hair),
                  ),
                  child: Row(children: [
                    Square(l.route(r.id)),
                    const SizedBox(width: 9),
                    FlagDot(node(r.node!).flag, 16),
                    const SizedBox(width: 5),
                    Expanded(child: Line1(r.node!, l.text(14, weight: 600, lh: 20))),
                    const SizedBox(width: 9),
                    Line1(r.name, l.text(13, color: l.ink2, lh: 18), clip: true),
                    const SizedBox(width: 9),
                    SizedBox(width: 56, child: Text(latText(node(r.node!).delay), textAlign: TextAlign.right, maxLines: 1, style: l.text(13, color: l.mute, lh: 18))),
                  ]),
                ),
              ),
            ),
            const SizedBox(height: 6),
          ],
          Unroll(
            step: 8,
            child: Padding(
              padding: const EdgeInsets.only(left: 4),
              child: Row(children: [
                Expanded(child: Line1(tr('点一条线路换出口', 'Tap a line to change exit'), l.text(13, color: l.mute, lh: 18))),
                IconBox('x', onTap: s.collapse, label: tr('收起', 'Collapse')),
              ]),
            ),
          ),
        ]),
      ),
    ]);
  }
}

// ---- speed ----

class _SpeedCard extends StatelessWidget {
  const _SpeedCard(this.s, this.kind, this.variant);
  final AppState s;
  final String kind, variant;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final (dn, du) = sizeParts(s.on ? stats.down : 0);
    final (un, uu) = sizeParts(s.on ? stats.up : 0);
    final down = dn, unit = '$du/s', up = un, upUnit = '$uu/s';
    final today = sizeText(stats.today);
    final spdValue = '↓ $down $unit';
    if (kind == 'normal') {
      final st = l.text(20, weight: 700, display: true, lh: 26);
      final small = l.text(12, color: l.mute, lh: 26);
      final smallLow = l.text(12, color: l.mute, lh: 18);
      return _pad([
        _flyText('k-speed', tr('此刻速度', 'Speed now'), l.text(12, weight: 500, color: l.mute, lh: 18, ls: .08)),
        const SizedBox(height: 3),
        FlyTag(
          id: 'spd',
          text: spdValue,
          style: st,
          build: (x) => Text.rich(TextSpan(style: x, children: [TextSpan(text: '↓ $down'), TextSpan(text: ' $unit', style: small)]), maxLines: 1, softWrap: false),
        ),
        Unroll(
          step: 2,
          child: Text.rich(TextSpan(style: l.text(13, color: l.ink2, lh: 18), children: [TextSpan(text: '↑ $up '), TextSpan(text: '$upUnit · ${tr('今日 {0}', 'Today {0}', [today])}', style: smallLow)]), maxLines: 1, softWrap: false),
        ),
        const Spacer(),
        const Unroll(step: 4, child: TrafficStack(38, 16)),
      ]);
    }
    if (kind == 'compact') {
      final ink = l.text(12.5, color: l.ink2, lh: 17.5);
      return _Compact(id: 'speed', variant: variant, kicker: tr('此刻速度', 'Speed now'), value: spdValue, flyKey: 'spd', list: [
        Text('↑ $up $upUnit', style: ink),
        Text(tr('今日 {0}', 'Today {0}', [today]), style: ink),
        Text(tr('本月 {0}', 'This month {0}', [sizeText(stats.month)]), style: ink),
      ]);
    }
    // Sample data has no history, so it scales today's.
    final k = {'day': 1.0, 'week': 6.2, 'month': 21.0}[s.range]!;
    final bytes = s.rangeBytes;
    final total = bytes == null ? 0 : bytes.values.fold(0, (a, b) => a + b);
    double partOf(String id) => bytes == null ? share[id] ?? 0 : (total == 0 ? 0 : (bytes[id] ?? 0) / total);
    int sizeOf(String id) => bytes == null ? (stats.today * k * (share[id] ?? 0)).round() : bytes[id] ?? 0;
    return _Detail(
      s: s,
      flyId: 'speed',
      kicker: _detKicker(l, 'speed', tr('流量', 'Traffic')),
      title: _flyText('spd', spdValue, _h3(l)),
      body: ListView(padding: EdgeInsets.zero, children: [
        const Unroll(child: Padding(padding: EdgeInsets.fromLTRB(16, 2, 16, 12), child: TrafficStack(40, 72))),
        for (final (i, r) in s.routes.indexed)
          Unroll(
            step: (i * 2 + 2).clamp(0, 8),
            child: _TallyRow(color: l.route(r.id), name: r.name, share: partOf(r.id), value: sizeText(sizeOf(r.id))),
          ),
      ]),
      footer: [
        Seg<String>(options: [('day', tr('今天', 'Today')), ('week', tr('7 天', '7 days')), ('month', tr('30 天', '30 days'))], value: s.range, onChanged: (v) => s.set(() => s.range = v)),
      ],
    );
  }
}

class _TallyRow extends StatelessWidget {
  const _TallyRow({required this.color, required this.name, required this.share, required this.value});
  final Color color;
  final String name, value;
  final double share;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    return _Hairline(
      child: Container(
        constraints: const BoxConstraints(minHeight: 40),
        padding: const EdgeInsets.symmetric(horizontal: 16),
        child: Row(children: [
          Square(color),
          const SizedBox(width: 8),
          SizedBox(width: language.startsWith('zh') ? 62 : 96, child: Line1(name, l.text(13.5, lh: 20))),
          const SizedBox(width: 8),
          Expanded(
            child: Container(
              height: 5,
              decoration: BoxDecoration(color: l.paper2, borderRadius: BorderRadius.circular(3)),
              clipBehavior: Clip.hardEdge,
              alignment: Alignment.centerLeft,
              child: FractionallySizedBox(widthFactor: share, heightFactor: 1, child: ColoredBox(color: color)),
            ),
          ),
          const SizedBox(width: 8),
          SizedBox(width: 74, child: Text(value, textAlign: TextAlign.right, maxLines: 1, style: l.text(13.5, color: l.ink2, lh: 20))),
        ]),
      ),
    );
  }
}

// ---- apps ----

class _AppsCard extends StatelessWidget {
  const _AppsCard(this.s, this.kind, this.variant);
  final AppState s;
  final String kind, variant;

  String _short((String, String, String, String) t) {
    if (_android) return t.$1;
    final parts = t.$2.split('.');
    return parts[parts.length - 2];
  }

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final name = _android ? tr('谁在用', 'Top apps') : tr('正在连接', 'Connecting');
    final foot = l.text(12, color: l.mute, lh: 18);
    final go = Btn(tr('在活动中查看', 'View in Activity'), minH: 44, onTap: () => s.selectTab(1));
    if (!s.on) {
      if (kind == 'normal') {
        return _pad([_flyText('k-apps', name, l.text(12, weight: 500, color: l.mute, lh: 18, ls: .08)), const Spacer(), Unroll(child: Text(tr('{0} 条连接', '{0} connections', [0]), style: foot))]);
      }
      if (kind == 'compact') return _Compact(id: 'apps', variant: variant, kicker: name, value: tr('{0} 条', '{0}', [0]));
      return _Detail(s: s, flyId: 'apps', kicker: _detKicker(l, 'apps', name), title: Text(tr('{0} 条连接', '{0} connections', [0]), style: _h3(l)), body: const SizedBox.expand(), footer: [go]);
    }
    if (kind == 'normal') {
      final row = l.text(12.5, weight: 500, lh: 23);
      final rate = l.text(12.5, color: l.mute, lh: 23);
      return _pad([
        _flyText('k-apps', name, l.text(12, weight: 500, color: l.mute, lh: 18, ls: .08)),
        const SizedBox(height: 3),
        for (final (i, t) in topApps.take(2).indexed)
          Unroll(
            step: (i + 1) * 2,
            child: Row(children: [Expanded(child: Line1(_short(t), row)), const SizedBox(width: 6), Text(t.$4, maxLines: 1, style: rate)]),
          ),
        const Spacer(),
        Unroll(step: 6, child: Text(tr('{0} 条连接', stats.active == 1 ? '{0} connection' : '{0} connections', [stats.active]), style: foot)),
      ]);
    }
    if (kind == 'compact') {
      final ink = l.text(12.5, color: l.ink2, lh: 17.5);
      return _Compact(
        id: 'apps',
        variant: variant,
        kicker: name,
        value: _android ? (topApps.isEmpty ? '—' : topApps[0].$1) : tr('{0} 条', '{0}', [stats.active]),
        sub: Text(_android ? (topApps.isEmpty ? '' : topApps[0].$4) : tr('活跃连接', 'Active connections'), style: l.text(12, color: l.mute, lh: 17)),
        list: [for (final t in topApps.skip(_android ? 1 : 0).take(3)) Text(_short(t), style: ink, maxLines: 1)],
      );
    }
    return _Detail(
      s: s,
      flyId: 'apps',
      kicker: _detKicker(l, 'apps', name),
      title: Text(tr('{0} 条连接', stats.active == 1 ? '{0} connection' : '{0} connections', [stats.active]), style: _h3(l)),
      body: ListView(padding: EdgeInsets.zero, children: [
        for (final (i, t) in topApps.indexed)
          Unroll(
            step: (i * 2).clamp(0, 8),
            child: _Hairline(
              child: Container(
                constraints: const BoxConstraints(minHeight: 46),
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: Row(children: [
                  Square(l.route(t.$3)),
                  const SizedBox(width: 8),
                  Expanded(child: Line1(_android ? t.$1 : t.$2, l.text(13.5, lh: 20))),
                  Text(t.$4, maxLines: 1, style: l.text(13.5, color: l.ink2, lh: 20)),
                ]),
              ),
            ),
          ),
      ]),
      footer: [go],
    );
  }
}

// ---- routes ----

class _RoutesCard extends StatelessWidget {
  const _RoutesCard(this.s, this.kind);
  final AppState s;
  final String kind;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final others = s.routes.skip(1).toList();
    final all = others.where((r) => !r.direct).toList();
    final px = all.length > 3 ? all.sublist(0, 3) : all; // the rest are one tap away
    if (kind == 'normal') {
      final nameWidth = _nameColumn(px, l.text(14.5, weight: 600, lh: 20));
      return _pad(bottom: 2, [
        _flyText('k-routes', all.length > 3 ? tr('分流 · {0} 条', 'By rule · {0}', [all.length]) : tr('分流', 'By rule'), l.text(12, weight: 500, color: l.mute, lh: 18, ls: .08)),
        for (final (i, r) in px.indexed)
          Unroll(
            step: (i + 1) * 2,
            child: Tap(
              onTap: () => s.openSub(r.id),
              child: i == px.length - 1 ? _routeLine(l, r, nameWidth) : _Hairline(top: false, child: _routeLine(l, r, nameWidth)),
            ),
          ),
      ]);
    }
    if (kind == 'compact') {
      return Padding(
        padding: const EdgeInsets.only(left: 16, right: 12),
        child: Column(mainAxisAlignment: MainAxisAlignment.center, crossAxisAlignment: CrossAxisAlignment.start, children: [
          Unroll(child: _flyText('k-routes', tr('分流', 'By rule'), l.text(11.5, weight: 500, color: l.mute, lh: 15, ls: .06))),
          Unroll(
            step: 2,
            child: Row(children: [
              for (final (i, r) in px.indexed) ...[
                if (i > 0) const SizedBox(width: 12),
                Square(l.route(r.id), size: 8, radius: 2),
                const SizedBox(width: 4),
                Flexible(child: Line1(r.node!, l.text(12.5, weight: 500, color: l.ink2, lh: 20))),
              ],
            ]),
          ),
        ]),
      );
    }
    final sub = s.sub == null ? null : s.route(s.sub!);
    if (sub != null) {
      final n = node(sub.node!);
      return _Detail(
        s: s,
        flyId: 'routes',
        kicker: Row(mainAxisSize: MainAxisSize.min, children: [
          _flyText('rn-${sub.id}', sub.name, l.text(11.5, weight: 500, color: l.mute, lh: 15, ls: .06)),
          Text(tr(' · 选择出口', ' · Choose exit'), style: l.text(11.5, weight: 500, color: l.mute, lh: 15, ls: .06)),
        ]),
        title: Shim(active: s.pending == 'node:${sub.id}', child: _flyExit('exit-${sub.id}', n, _h3(l))),
        body: _PickerList(s: s, route: sub),
        above: _PickerTool(s: s, route: sub),
        footer: [IconBox('chev', flip: true, onTap: s.subBack, label: tr('返回路由', 'Back to routes')), ..._pickerFoot(s, sub)],
      );
    }
    return _Detail(
      s: s,
      flyId: 'routes',
      kicker: _detKicker(l, 'routes', tr('路由', 'Route')),
      title: Text(tr('每类流量走哪里', 'Where each kind of traffic goes'), style: _h3(l)),
      body: ListView(padding: EdgeInsets.zero, children: [
        for (final (i, r) in s.routes.indexed)
          Unroll(step: (i * 2).clamp(0, 8), child: _routeDetail(l, r)),
      ]),
      footer: [Btn(tr('管理路由组', 'Manage route groups'), minH: 44, padH: 12, fontSize: 13.5, onTap: s.showRoutes)],
    );
  }

  /// Fits the longest name shown, so the nodes beside them still line up.
  double _nameColumn(List<RouteCfg> routes, TextStyle style) {
    var width = 62.0;
    for (final r in routes) {
      final p = TextPainter(text: TextSpan(text: r.name, style: style), textDirection: TextDirection.ltr, maxLines: 1)..layout();
      if (p.width.ceilToDouble() > width) width = p.width.ceilToDouble();
      p.dispose();
    }
    return width > 112 ? 112 : width;
  }

  Widget _routeLine(Look l, RouteCfg r, double nameWidth) {
    final n = node(r.node!);
    return SizedBox(
      height: 44,
      child: Row(children: [
        Square(l.route(r.id)),
        const SizedBox(width: 9),
        SizedBox(width: nameWidth, child: Align(alignment: Alignment.centerLeft, child: _flyText('rn-${r.id}', r.name, l.text(14.5, weight: 600, lh: 20)))),
        const SizedBox(width: 9),
        Expanded(child: Align(alignment: Alignment.centerLeft, child: Shim(active: s.pending == 'node:${r.id}', child: _flyExit('exit-${r.id}', n, l.text(14.5, weight: 600, lh: 20))))),
        const SizedBox(width: 8),
        Text(latText(n.delay), maxLines: 1, style: l.text(13, color: l.mute, lh: 18)),
      ]),
    );
  }

  Widget _routeDetail(Look l, RouteCfg r) {
    final n = r.direct ? null : node(r.node!);
    final ms = l.text(12.5, color: l.mute, lh: 18);
    final content = _Hairline(
      child: Container(
        constraints: const BoxConstraints(minHeight: 58),
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
        child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Padding(padding: const EdgeInsets.only(top: 5), child: Square(l.route(r.id))),
          const SizedBox(width: 9),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Row(children: [
                _flyText('rn-${r.id}', r.name, l.text(15, weight: 600, lh: 20)),
                const SizedBox(width: 8),
                Expanded(
                  child: Align(
                    alignment: Alignment.centerRight,
                    child: n == null
                        ? Text(tr('直接连接', 'Direct'), style: l.text(14.5, weight: 600, lh: 20))
                        : (r.id == 'default' ? ExitName(n, l.text(14.5, weight: 600, lh: 20)) : _flyExit('exit-${r.id}', n, l.text(14.5, weight: 600, lh: 20))),
                  ),
                ),
              ]),
              const SizedBox(height: 1),
              Row(children: [
                Expanded(child: Line1(rulesOf[r.id]!, l.text(12.5, color: l.mute, lh: 18))),
                const SizedBox(width: 9),
                Text('${n != null ? '${latText(n.delay)} · ' : ''}${(share[r.id]! * 100).round()}%', maxLines: 1, style: ms),
              ]),
            ]),
          ),
        ]),
      ),
    );
    return r.direct ? content : Tap(onTap: () => s.openSub(r.id), child: content);
  }
}

// ---- choosing an exit ----

List<Widget> _pickerFoot(AppState s, RouteCfg r) => [
      _strategyBtn(s, r),
      IconBox('search', on: s.searching, label: tr('搜索节点', 'Search nodes'), onTap: () => s.set(() {
            s.searching = !s.searching;
            s.nodeQuery = '';
          })),
      Btn(
        tr('测速', 'Test'),
        minH: 44,
        padH: 12,
        fontSize: 13.5,
        onTap: s.testing ? null : s.test,
        child: s.testing ? _ShimLabel(tr('测速中', 'Testing'), 13.5) : null,
      ),
    ];

class _ShimLabel extends StatelessWidget {
  const _ShimLabel(this.text, this.size);
  final String text;
  final double size;

  @override
  Widget build(BuildContext context) => Shim(active: true, child: Text(text, maxLines: 1, style: Skin.of(context).text(size, weight: 600, lh: size * 1.4)));
}

class _PickerTool extends StatelessWidget {
  const _PickerTool({required this.s, this.route});
  final AppState s;

  /// The route being changed, when the card has its services to link to.
  final RouteCfg? route;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final child = s.searching
        ? Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: SearchField(hint: tr('搜索节点', 'Search nodes'), minH: 40, autofocus: true, onChanged: (v) => s.set(() => s.nodeQuery = v)),
          )
        : SizedBox(
            height: 36,
            child: Row(children: [
              Expanded(child: _regionChips(l)),
              if (route != null)
                Padding(
                  padding: const EdgeInsets.only(left: 6, right: 16),
                  child: Btn(tr('服务和规则', 'Services and rules'), minH: 36, padH: 10, fontSize: 13, onTap: () => s.showGroup(route!.id)),
                ),
            ]),
          );
    return Unroll(
      child: _Hairline(child: Padding(padding: const EdgeInsets.fromLTRB(0, 8, 0, 6), child: child)),
    );
  }

  Widget _regionChips(Look l) {
    final count = <String, int>{};
    for (final n in nodes) {
      count[n.flag] = (count[n.flag] ?? 0) + 1;
    }
    final keys = count.keys.where((k) => k.isNotEmpty).toList()..sort((a, b) => count[b]!.compareTo(count[a]!));
    Widget chip(String v, String text, String? flag, int c) => Padding(
          padding: const EdgeInsets.only(right: 6),
          child: Chip(text: text, on: s.region == v, count: c, leading: flag == null ? null : FlagDot(flag, 15), onTap: () => s.set(() => s.region = v)),
        );
    return ListView(scrollDirection: Axis.horizontal, padding: const EdgeInsets.symmetric(horizontal: 16), children: [
      chip('all', tr('全部', 'All'), null, nodes.length),
      for (final k in keys) chip(k, regionName(k), k, count[k]!),
      if (count[''] != null) chip('none', tr('未识别', 'Unidentified'), '', count['']!),
    ]);
  }
}

class _PickerList extends StatelessWidget {
  const _PickerList({required this.s, required this.route});
  final AppState s;
  final RouteCfg route;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final q = s.nodeQuery.trim().toLowerCase();
    final list = nodes.where((n) {
      final inRegion = s.region == 'all' || (s.region == 'none' ? n.flag.isEmpty : n.flag == s.region);
      return inRegion && (q.isEmpty || n.name.toLowerCase().contains(q));
    }).toList()
      ..sort(byLatency);
    if (list.isEmpty) return Center(child: Text(tr('没有匹配的节点', 'No matching nodes'), style: l.text(14, color: l.mute, lh: 20)));
    return ListView.builder(
      padding: EdgeInsets.zero,
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      itemCount: list.length + 1,
      itemBuilder: (context, i) {
        if (i == list.length) {
          return _Hairline(
            child: Container(
              constraints: const BoxConstraints(minHeight: 52),
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Row(children: [
                Expanded(child: Text(tr('命中的规则', 'Matched rules'), style: l.text(14.5, lh: 20))),
                Text(tr('{0} 条', '{0}', [rulesOf[route.id]!.split(' ').first]), style: l.text(13, color: l.mute, lh: 18)),
                Ic('chev', size: 15, color: l.mute),
              ]),
            ),
          );
        }
        return Unroll(step: (i * 2).clamp(0, 8), child: _PickRow(s: s, route: route, n: list[i]));
      },
    );
  }
}

class _PickRow extends StatelessWidget {
  const _PickRow({required this.s, required this.route, required this.n});
  final AppState s;
  final RouteCfg route;
  final Node n;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final on = n.name == route.node;
    final switching = s.pending == 'node:${route.id}' && on;
    final sub = l.text(12.5, color: l.mute, lh: 18);
    return Tap(
      onTap: () => s.pickNode(route.id, n.name),
      child: Container(
        constraints: const BoxConstraints(minHeight: 58),
        padding: const EdgeInsets.fromLTRB(16, 6, 14, 6),
        decoration: BoxDecoration(color: on ? l.hover : null, border: Border(bottom: BorderSide(color: l.hair2))),
        child: Row(children: [
          FlagDot(n.flag, 24),
          const SizedBox(width: 11),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Shim(active: switching, child: _flyText('pick-${n.name}', n.name, l.text(15, weight: 600, lh: 22))),
              if (switching)
                Text(tr('正在切换', 'Switching'), style: sub)
              else
                Row(children: [
                  if (n.city.isNotEmpty) ...[Text(n.city, style: sub), const SizedBox(width: 4)],
                  SourceTag(n.source),
                  Flexible(child: Line1(' · ${n.proto} · ${n.provider}', sub)),
                ]),
            ]),
          ),
          SizedBox(
            width: 54,
            child: Align(
              alignment: Alignment.centerRight,
              child: s.testing ? const Skeleton() : Text(latText(n.delay), maxLines: 1, style: l.text(13.5, color: latColor(l, n.delay), lh: 20)),
            ),
          ),
          const SizedBox(width: 11),
          SizedBox(width: 17, child: on ? Ic('check', size: 17, color: l.ink) : null),
        ]),
      ),
    );
  }
}

// ---- mode ----

class _ModeCard extends StatelessWidget {
  const _ModeCard(this.s, this.kind, this.variant);
  final AppState s;
  final String kind, variant;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final m = modes.firstWhere((x) => x.$1 == s.mode);
    if (kind == 'normal') {
      return Stack(children: [
        Positioned.fill(child: _pad([_flyText('k-mode', tr('模式', 'Mode'), l.text(12, weight: 500, color: l.mute, lh: 18, ls: .08))])),
        Positioned(
          left: 8,
          right: 8,
          top: 32,
          child: Unroll(
            step: 2,
            child: Row(children: [
              for (final x in modes) Expanded(child: _ModeWord(s: s, id: x.$1, label: x.$2)),
            ]),
          ),
        ),
        Positioned(right: 16, top: 10, child: Unroll(step: 2, child: Text(m.$4, maxLines: 1, style: l.text(12.5, color: l.mute, lh: 15)))),
      ]);
    }
    if (kind == 'compact') {
      return _Compact(id: 'mode', variant: variant, kicker: tr('模式', 'Mode'), value: m.$2, sub: Text(m.$4, maxLines: 1, style: l.text(12, color: l.mute, lh: 17)));
    }
    return _Detail(
      s: s,
      flyId: 'mode',
      kicker: _detKicker(l, 'mode', tr('模式', 'Mode')),
      title: Text(tr('模式与规则', 'Mode and rules'), style: _h3(l)),
      body: ListView(padding: EdgeInsets.zero, children: [
        for (final (i, x) in modes.indexed)
          Unroll(
            step: (i + 1) * 2,
            child: Tap(
              onTap: () => s.setMode(x.$1),
              child: _Hairline(
                child: Container(
                  width: double.infinity,
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Row(mainAxisAlignment: MainAxisAlignment.spaceBetween, children: [
                      Text(x.$2, style: l.text(16, weight: 600, display: true, lh: 24)),
                      if (x.$1 == s.mode) Text(tr('使用中', 'In use'), style: l.text(12.5, weight: 500, color: l.goodT, lh: 18)),
                    ]),
                    const SizedBox(height: 2),
                    Text(x.$3, style: l.text(12.5, color: l.mute, lh: 18)),
                  ]),
                ),
              ),
            ),
          ),
        Unroll(
          step: 8,
          child: _Hairline(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 6),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(tr('规则分布', 'Rule distribution'), style: l.text(12, weight: 500, color: l.mute, lh: 18, ls: .08)),
                for (final r in s.routes.skip(1))
                  _Hairline(
                    child: SizedBox(
                      height: 34,
                      child: Row(children: [
                        Square(l.route(r.id)),
                        const SizedBox(width: 8),
                        Expanded(child: Text(r.name, style: l.text(13.5, lh: 20))),
                        Text(tr('{0} 条', '{0}', [rulesOf[r.id]!.split(' ').first]), style: l.text(13.5, color: l.ink2, lh: 20)),
                      ]),
                    ),
                  ),
              ]),
            ),
          ),
        ),
      ]),
      footer: [_noteF(l, m.$4)],
    );
  }
}

class _ModeWord extends StatelessWidget {
  const _ModeWord({required this.s, required this.id, required this.label});
  final AppState s;
  final String id, label;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final on = id == s.mode;
    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTap: () => s.setMode(id),
      child: Container(
        constraints: const BoxConstraints(minHeight: 44),
        padding: const EdgeInsets.fromLTRB(8, 6, 8, 8),
        child: Stack(clipBehavior: Clip.none, children: [
          Text(label, maxLines: 1, softWrap: false, style: l.text(21, weight: 700, color: on ? l.ink : l.mute, display: true, lh: 28).copyWith(shadows: on ? null : const [])),
          if (on)
            Positioned(
              left: 0,
              bottom: -5,
              width: 42,
              height: 3,
              child: FlyTag.box(id: 'mode-ul', build: (_) => DecoratedBox(decoration: BoxDecoration(color: l.ink, borderRadius: l.br(2)))),
            ),
        ]),
      ),
    );
  }
}

// ---- the exit you're on, and the master switch ----

const _roomForPower = 300.0;

/// The master switch again in the opened card, at the bottom where a thumb is.
class _PowerButton extends StatelessWidget {
  const _PowerButton(this.s);
  final AppState s;

  @override
  Widget build(BuildContext context) {
    const size = 17.0;
    final taking = s.pending == 'take';
    final String text, working;
    if (s.issue) {
      (text, working) = (tr('重新接管', 'Reconnect'), tr('正在重新接管', 'Reconnecting'));
    } else if (s.on) {
      (text, working) = (tr('关闭 VPN', 'Turn off VPN'), tr('正在断开', 'Disconnecting'));
    } else {
      (text, working) = (tr('开启 VPN', 'Turn on VPN'), tr('正在接管', 'Connecting'));
    }
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 2, 16, 12),
      child: Unroll(
        step: 8,
        child: Btn(
          text,
          kind: s.on && !s.issue ? BtnKind.quiet : BtnKind.accent,
          minH: 56,
          fontSize: size,
          expand: true,
          onTap: taking ? null : (s.issue ? s.retake : s.toggleTake),
          child: taking ? _ShimLabel(working, size) : null,
        ),
      ),
    );
  }
}

class _HeroCard extends StatelessWidget {
  const _HeroCard(this.s, this.kind, this.variant);
  final AppState s;
  final String kind, variant;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final r0 = s.routes.first;
    final n = node(r0.node!);
    final busy = s.pending == 'node:default', taking = s.pending == 'take', direct = s.mode == 'direct';
    final kicker = l.text(12, weight: 500, color: l.mute, lh: 18, ls: .08);
    final exitStyle = l.text(32, weight: 700, display: true, lh: 40);
    final meta = l.text(13.5, color: l.ink2, lh: 20);

    if (kind == 'normal') {
      if (s.issue) {
        return _pad([
          _flyText('k-hero', tr('异常', 'Problem'), kicker),
          const SizedBox(height: 2),
          _flyText('exit-default', tr('VPN 已断开', 'VPN disconnected'), exitStyle.copyWith(color: l.warnT)),
          Unroll(child: Line1(s.issueText ?? tr('被其他 App 断开了，流量现在直接联网', 'Another app disconnected it. Traffic is now going direct.'), meta)),
          const Spacer(),
          Unroll(
            step: 2,
            child: SizedBox(
              width: double.infinity,
              child: Btn(tr('重新接管', 'Reconnect'), kind: BtnKind.accent, onTap: s.retake, expand: true, child: taking ? _ShimLabel(tr('正在重新接管', 'Reconnecting'), 14) : null),
            ),
          ),
        ]);
      }
      return Stack(children: [
        Positioned.fill(
          child: _pad([
            _flyText('k-hero', tr('当前出口 · {0}', 'Exit · {0}', [s.mode == 'global' ? tr('全局', 'Global') : tr('其他流量', 'Other')]), kicker),
            const SizedBox(height: 2),
            Padding(
              padding: const EdgeInsets.only(right: 56),
              child: Shim(
                active: busy,
                child: direct ? _flyText('exit-default', tr('直接连接', 'Direct'), exitStyle) : _flyExit('exit-default', n, exitStyle),
              ),
            ),
            Unroll(
              child: s.alert != null
                  ? Tap(
                      onTap: s.alert!.action == null ? null : s.followAlert,
                      child: Row(children: [
                        Flexible(child: Line1(s.alert!.text, meta.copyWith(color: s.alert!.level == 'error' ? l.signalT : l.warnT))),
                        if (s.alert!.label != null) ...[const SizedBox(width: 6), Text(s.alert!.label!, maxLines: 1, style: l.text(13.5, weight: 600, color: l.ink2, lh: 20)), Ic('chev', size: 13, color: l.ink2)],
                      ]),
                    )
                  : direct
                  ? Line1(tr('所有流量不经过任何节点', 'No traffic goes through a node'), meta)
                  : n.proto.isEmpty
                  ? Line1(s.on ? tr('正在连接', 'Connecting') : tr('开启后显示实际出口', 'Shows the real exit once on'), meta)
                  : Row(children: [
                      if (n.city.isNotEmpty) ...[Text(n.city, style: meta), const SizedBox(width: 5)],
                      SourceTag(n.source),
                      const SizedBox(width: 5),
                      Flexible(child: Line1('· ${n.proto} · ${tr('来自 {0}', 'from {0}', [n.provider])}', meta)),
                    ]),
            ),
            const Spacer(),
            if (!direct) _latRow(context, l, r0, n, busy),
          ]),
        ),
        Positioned(right: 8, top: 2, child: Unroll(child: Toggle(on: s.on, onTap: s.toggleTake, label: tr('接管', 'Connection')))),
      ]);
    }
    if (kind == 'compact') {
      return _Compact(
        id: 'hero',
        variant: variant,
        kicker: tr('当前出口', 'Exit'),
        value: direct ? tr('直接连接', 'Direct') : n.name,
        flyKey: 'exit-default',
        valueNode: direct ? null : n,
        shim: busy,
        sub: direct
            ? null
            : Text.rich(
                TextSpan(style: l.text(12, color: l.mute, lh: 17), children: [TextSpan(text: latText(n.delay), style: TextStyle(color: latColor(l, n.delay))), if (n.city.isNotEmpty) TextSpan(text: ' · ${n.city}')]),
                maxLines: 1,
              ),
      );
    }
    return LayoutBuilder(
      builder: (context, c) => _Detail(
        s: s,
        flyId: 'hero',
        kicker: _detKicker(l, 'hero', tr('其他流量的出口', 'Exit for other traffic')),
        title: Shim(active: busy, child: _flyExit('exit-default', n, _h3(l))),
        body: _PickerList(s: s, route: r0),
        above: _PickerTool(s: s),
        footer: _pickerFoot(s, r0),
        // a phone on its side, or a keyboard, leaves the node list no room for it
        below: c.maxHeight < _roomForPower ? null : _PowerButton(s),
      ),
    );
  }

  Widget _latRow(BuildContext context, Look l, RouteCfg r0, Node n, bool busy) {
    final msStyle = l.text(32, weight: 700, color: n.delay > 0 ? l.goodT : latColor(l, n.delay), display: true, lh: 32);
    final value = n.delay > 0 ? '${n.delay}' : latText(n.delay);
    return SizedBox(
      height: 44,
      child: Row(children: [
        Shim(
          active: busy,
          child: FlyTag(
            id: 'ms-default',
            text: n.delay > 0 ? '${n.delay}ms' : value,
            style: msStyle,
            build: (x) => Text.rich(TextSpan(style: x, children: [TextSpan(text: value), if (n.delay > 0) TextSpan(text: ' ms', style: l.text(13, color: l.mute, lh: 32))]), maxLines: 1, softWrap: false),
          ),
        ),
        const SizedBox(width: 10),
        Unroll(child: LatencyBars(n.history)),
        const Spacer(),
        Unroll(
          step: 2,
          child: Btn(tr('自动', 'Auto'), kind: r0.auto ? BtnKind.quiet : BtnKind.outline, leading: r0.auto ? Ic('check', size: 15, color: l.ink) : null, onTap: () => s.toggleAuto('default')),
        ),
        const SizedBox(width: 10),
        Unroll(step: 4, child: Btn(tr('换节点', 'Change node'), kind: BtnKind.accent, onTap: () => s.open('hero'))),
      ]),
    );
  }
}
