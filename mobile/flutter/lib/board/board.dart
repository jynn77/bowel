import 'dart:math' as math;

import 'package:flutter/foundation.dart' show mapEquals;
import 'package:flutter/widgets.dart';

import '../app/state.dart';
import '../pages/frame.dart';
import '../ui/atoms.dart';
import '../ui/skin.dart';
import 'cards.dart';
import 'fly.dart';
import 'map.dart';

// A thumb reaches the lower half of a phone easily and the upper half badly,
// so the top is for looking and everything you operate sits low. This order
// never changes: a card grows where it is and its neighbours squeeze.
const _rows = [
  ['map'],
  ['speed', 'apps'],
  ['routes'],
  ['mode'],
  ['hero'],
];
const _rowH = {'speed': 104.0, 'routes': 166.0, 'mode': 86.0, 'hero': 146.0};
const _strip = 52.0, _mapStrip = 64.0, _column = 118.0;

// A big window has no thumb to design for, so it uses the desktop's board: a
// grid whose tracks are given up by the rows and columns the open card isn't in.
class _Slot {
  const _Slot(this.id, this.row, this.cols);
  final String id;
  final int row;
  final List<int> cols;
}

class _Grid {
  const _Grid(this.cols, this.rows, this.slots);
  final List<double> cols, rows;
  final List<_Slot> slots;
}

const _wide = _Grid([400, 330, 330], [318, 150, 218], [
  _Slot('hero', 0, [0]),
  _Slot('map', 0, [1, 2]),
  _Slot('mode', 1, [0]),
  _Slot('routes', 1, [1, 2]),
  _Slot('speed', 2, [0]),
  _Slot('apps', 2, [1, 2]),
]);

// Narrower than any desktop window, so two columns: three would leave cards
// too thin to read. It's the same grid, with the map on top where it has room.
const _medium = _Grid([1, 1], [300, 200, 220, 200], [
  _Slot('map', 0, [0, 1]),
  _Slot('hero', 1, [0]),
  _Slot('mode', 1, [1]),
  _Slot('routes', 2, [0, 1]),
  _Slot('speed', 3, [0]),
  _Slot('apps', 3, [1]),
]);

const _sqRow = 78.0, _sqCol = 190.0;

/// [tracks] scaled to fill [total]: closed tracks stay at [sq] points, and the
/// [open] ones share the rest in equal parts.
List<double> _openTracks(List<double> tracks, double total, double sq, Iterable<int> open) {
  final closed = tracks.length - open.length;
  final each = (total - closed * sq) / open.length;
  return [for (var i = 0; i < tracks.length; i++) open.contains(i) ? each : sq];
}

Map<String, Rect> _gridLayout(_Grid g, String? x, double height, double width) {
  final gapsW = g.cols.length - 1.0, gapsH = g.rows.length - 1.0;
  final fullW = width - gapsW, fullH = height - gapsH;
  double sum(List<double> a) => a.reduce((p, n) => p + n);
  var cols = [for (final c in g.cols) c * fullW / sum(g.cols)];
  var rows = [for (final r in g.rows) r * fullH / sum(g.rows)];
  // Most cards hold a fixed amount: a row is never shorter than its tallest
  // one, and the rows with room to spare, the map's first, make up for it.
  final mins = [
    for (var i = 0; i < rows.length; i++)
      g.slots.where((s) => s.row == i).fold(0.0, (p, s) => math.max(p, _rowH[s.id] ?? 0)),
  ];
  double over(int i) => math.max(0.0, rows[i] - mins[i]);
  final short = [for (var i = 0; i < rows.length; i++) math.max(0.0, mins[i] - rows[i])].reduce((p, n) => p + n);
  if (short > 0) {
    final spare = [for (var i = 0; i < rows.length; i++) over(i)].reduce((p, n) => p + n);
    final keep = spare > short ? (spare - short) / spare : 0.0;
    rows = [for (var i = 0; i < rows.length; i++) mins[i] + over(i) * keep];
  }
  for (final s in g.slots) {
    if (s.id != x) continue;
    // A card as wide as the grid has no column to take from.
    if (s.cols.length < g.cols.length) cols = _openTracks(cols, fullW, _sqCol, s.cols);
    rows = _openTracks(rows, fullH, _sqRow, [s.row]);
  }
  final out = <String, Rect>{};
  double before(List<double> a, int i) => a.take(i).fold(0.0, (p, n) => p + n) + i;
  for (final s in g.slots) {
    final w = s.cols.fold(0.0, (p, c) => p + cols[c]) + s.cols.length - 1;
    out[s.id] = Rect.fromLTWH(before(cols, s.cols.first), before(rows, s.row), w, rows[s.row]);
  }
  return out;
}

Map<String, Rect> boardLayout(WindowForm form, String? x, double height, double width) {
  if (form == WindowForm.wide) return _gridLayout(_wide, x, height, width);
  if (form == WindowForm.medium) return _gridLayout(_medium, x, height, width);
  final out = <String, Rect>{};
  double strip(List<String> r) => r[0] == 'map' ? _mapStrip : _strip;
  final open = x == null ? -1 : _rows.indexWhere((r) => r.contains(x));
  final rest = _rowH.values.reduce((a, b) => a + b);
  var others = 0.0;
  for (var i = 0; i < _rows.length; i++) {
    if (i != open) others += strip(_rows[i]);
  }
  final openH = height - others;
  var y = 0.0;
  for (var i = 0; i < _rows.length; i++) {
    final r = _rows[i];
    final double h = x == null ? (r[0] == 'map' ? math.max(_mapStrip, height - rest) : _rowH[r[0]]!) : (i == open ? openH : strip(r));
    // the neighbour in a shared row narrows and stays on its own side
    final List<double> widths = i == open && r.length == 2
        ? (r[0] == x ? [width - _column - 1, _column] : [_column, width - _column - 1])
        : (r.length == 2 ? [(width - 1) / 2, (width - 1) / 2] : [width]);
    final drawn = math.max(0.0, i == _rows.length - 1 ? h : h - 1);
    var at = 0.0;
    for (var k = 0; k < r.length; k++) {
      out[r[k]] = Rect.fromLTWH(at, y, widths[k], drawn);
      at += widths[k] + 1;
    }
    y += h;
  }
  return out;
}

String variantOf(Rect b) => b.height < 120 ? (b.width < 260 ? 'mini' : 'strip') : (b.width < 170 ? 'column' : 'wide');

class Board extends StatefulWidget {
  const Board({super.key, required this.app});

  final AppState app;

  @override
  State<Board> createState() => _BoardState();
}

class _BoardState extends State<Board> {
  final _host = GlobalKey<FlyHostState>();
  Size _size = Size.zero;
  WindowForm _form = WindowForm.phone;

  void _morph(VoidCallback change, {Map<String, String>? alias}) {
    final host = _host.currentState;
    if (host == null) {
      change();
      return;
    }
    host.morph(change, finalCell: (cell) => boardLayout(_form, widget.app.x, _size.height, _size.width)[cell]!, alias: alias);
  }

  @override
  void initState() {
    super.initState();
    widget.app.morpher = _morph;
  }

  @override
  void dispose() {
    if (identical(widget.app.morpher, _morph)) widget.app.morpher = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final app = widget.app;
    _form = Frame.of(context).form;
    return LayoutBuilder(
      builder: (context, c) {
        final h = c.maxHeight, w = c.maxWidth;
        _size = Size(w, h);
        final boxes = boardLayout(_form, app.x, h, w), normal = boardLayout(_form, null, h, w);
        return FlyHost(
          key: _host,
          child: ColoredBox(
            color: app.twisted ? l.paper : l.hair,
            child: Stack(
              clipBehavior: Clip.hardEdge,
              children: [
                for (final id in boxes.keys)
                  AnimatedPositioned.fromRect(
                    key: ValueKey(id),
                    rect: boxes[id]!,
                    duration: const Duration(milliseconds: 560),
                    curve: easeCard,
                    child: _Cell(id: id, app: app, box: boxes[id]!, normalBox: normal[id]!, kind: app.x == null ? 'normal' : (app.x == id ? 'expanded' : 'compact')),
                  ),
                if (app.twisted) Positioned.fill(child: _Walls(boxes: boxes, color: Color.alphaBlend(l.hair, l.paper))),
              ],
            ),
          ),
        );
      },
    );
  }
}

class _Cell extends StatefulWidget {
  const _Cell({required this.id, required this.app, required this.box, required this.normalBox, required this.kind});

  final String id, kind;
  final AppState app;
  final Rect box, normalBox;

  @override
  State<_Cell> createState() => _CellState();
}

class _CellState extends State<_Cell> with TickerProviderStateMixin {
  late final _enter = AnimationController(vsync: this, duration: const Duration(seconds: 1), value: 1);
  late final _leave = AnimationController(vsync: this, duration: const Duration(milliseconds: 200), value: 1);
  String? _leaving;
  Size _leavingSize = Size.zero;
  bool _down = false;

  Size _sizeFor(_Cell w, String kind) => kind == 'normal' ? w.normalBox.size : w.box.size;

  @override
  void didUpdateWidget(_Cell old) {
    super.didUpdateWidget(old);
    if (old.kind != widget.kind) {
      _leaving = old.kind;
      _leavingSize = _sizeFor(old, old.kind);
      if (_leaving == widget.kind) _leaving = null;
      _leave.forward(from: 0).whenComplete(() {
        if (mounted) setState(() => _leaving = null);
      });
      _enter.forward(from: 0);
    }
  }

  @override
  void dispose() {
    _enter.dispose();
    _leave.dispose();
    super.dispose();
  }

  void _setDown(bool v) {
    if (_down != v) setState(() => _down = v);
  }

  VoidCallback? get _onTap {
    final app = widget.app, id = widget.id;
    if (widget.kind == 'expanded') return id == 'map' ? app.collapse : null;
    return () => app.open(id);
  }

  Widget _view(String kind, Size size, Look look) {
    final shown = kind == widget.kind;
    final body = Skin(
      look: kind == 'expanded' ? look.expanded : look,
      child: CellScope(
        cell: widget.id,
        shown: shown,
        // from the view's own size: a leaving view keeps the shape it had
        child: cardView(context, widget.app, widget.id, kind, variantOf(Offset.zero & size)),
      ),
    );
    return Positioned(
      key: ValueKey(kind),
      left: 0,
      top: 0,
      width: size.width,
      height: size.height,
      child: shown
          ? EntranceScope(notifier: _enter, child: body)
          : IgnorePointer(
              child: AnimatedBuilder(
                animation: _leave,
                child: body,
                builder: (context, child) {
                  final t = const Cubic(.4, 0, 1, 1).transform(_leave.value);
                  return ClipRect(
                    clipper: _Rolled(1 - t),
                    child: Transform.translate(offset: Offset(0, -6 * t), child: child),
                  );
                },
              ),
            ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final expanded = widget.kind == 'expanded';
    final bg = expanded ? l.sheet : (_down ? Color.lerp(l.paper, l.ink, .07)! : l.paper);
    final onTap = _onTap;
    final look = l;
    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTapDown: onTap == null ? null : (_) => _setDown(true),
      onTapUp: (_) => _setDown(false),
      onTapCancel: () => _setDown(false),
      onTap: onTap,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 200),
        color: bg,
        child: ClipRect(
          child: CellMark(
            child: Stack(
              fit: StackFit.expand,
              children: [
                if (widget.id == 'map') Positioned.fill(child: ExitMap(state: widget.app, expanded: expanded)),
                if (_leaving != null) _view(_leaving!, _leavingSize, look),
                _view(widget.kind, _sizeFor(widget, widget.kind), look),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _Rolled extends CustomClipper<Rect> {
  const _Rolled(this.t);
  final double t;

  @override
  Rect getClip(Size size) => Rect.fromLTWH(0, 0, size.width, size.height * t);

  @override
  bool shouldReclip(_Rolled old) => old.t != t;
}

// ---- the twisted lines ----

const _lineSwing = 1.4, _lineWave = 46.0, _lineReach = _lineSwing + 2;

/// How far a line at t along the board bends from its gap. The bulges belong
/// to the board, so a line that moves keeps them and lines that cross meet.
double _lineBend(double t) {
  final u = 2 * math.pi * t / _lineWave;
  return _lineSwing * (.8 * math.sin(u) + .2 * math.sin(u / 3 + 1));
}

/// The lines between the cells when they're drawn bent. They follow the
/// cells, eased the same way the cells are.
class _Walls extends StatefulWidget {
  const _Walls({required this.boxes, required this.color});
  final Map<String, Rect> boxes;
  final Color color;

  @override
  State<_Walls> createState() => _WallsState();
}

class _WallsState extends State<_Walls> with SingleTickerProviderStateMixin {
  late final _c = AnimationController(vsync: this, duration: const Duration(milliseconds: 560), value: 1);
  late Map<String, Rect> _from = widget.boxes, _to = widget.boxes;

  Map<String, Rect> _now() {
    final t = easeCard.transform(_c.value);
    return {for (final e in _to.entries) e.key: Rect.lerp(_from[e.key] ?? e.value, e.value, t)!};
  }

  @override
  void didUpdateWidget(_Walls old) {
    super.didUpdateWidget(old);
    final a = widget.boxes;
    if (a.length == _to.length && a.entries.every((e) => _to[e.key] == e.value)) return;
    _from = _now();
    _to = a;
    _c.forward(from: 0);
  }

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return IgnorePointer(
      child: RepaintBoundary(
        child: AnimatedBuilder(
          animation: _c,
          builder: (context, _) => CustomPaint(size: Size.infinite, painter: _WallsPainter(_now(), widget.color)),
        ),
      ),
    );
  }
}

class _WallsPainter extends CustomPainter {
  _WallsPainter(this.boxes, this.color);
  final Map<String, Rect> boxes;
  final Color color;

  void _wavy(Path path, double gap, Offset a, Offset b, bool down) {
    double along(Offset q) => down ? q.dy : q.dx;
    Offset at(double t) => down ? Offset(gap + _lineBend(t), t) : Offset(t, gap + _lineBend(t));
    final pts = <Offset>[a];
    const step = _lineWave / 8;
    final tb = along(b);
    for (var t = ((along(a) / step).floor() + 1) * step; t < tb - step / 4; t += step) {
      pts.add(at(t));
    }
    pts.add(b);
    path.moveTo(a.dx, a.dy);
    for (var i = 1; i < pts.length - 1; i++) {
      final m = (pts[i] + pts[i + 1]) / 2;
      path.quadraticBezierTo(pts[i].dx, pts[i].dy, m.dx, m.dy);
    }
    path.lineTo(b.dx, b.dy);
  }

  @override
  void paint(Canvas canvas, Size size) {
    final path = Path();
    final w = size.width, h = size.height;
    Offset meet(double x, double y) => Offset(x + _lineBend(y), y + _lineBend(x));
    for (final b in boxes.values) {
      if (b.width < .5 || b.height < .5) continue;
      final left = b.left < .5, top = b.top < .5, right = b.right > w - .5, bottom = b.bottom > h - .5;
      final lx = b.left - .5, ty = b.top - .5, rx = b.right + .5, by = b.bottom + .5;
      if (!right) {
        _wavy(path, rx, top ? Offset(rx + _lineBend(-_lineReach), -_lineReach) : meet(rx, ty), bottom ? Offset(rx + _lineBend(b.bottom + _lineReach), b.bottom + _lineReach) : meet(rx, by), true);
      }
      if (!bottom) {
        _wavy(path, by, left ? Offset(-_lineReach, by + _lineBend(-_lineReach)) : meet(lx, by), right ? Offset(b.right + _lineReach, by + _lineBend(b.right + _lineReach)) : meet(rx, by), false);
      }
    }
    canvas.drawPath(
      path,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1
        ..color = color,
    );
  }

  @override
  bool shouldRepaint(_WallsPainter old) => old.color != color || !mapEquals(old.boxes, boxes);
}
