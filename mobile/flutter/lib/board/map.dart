import 'dart:convert';
import 'dart:math' as math;
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/scheduler.dart';
import 'package:flutter/widgets.dart';

import '../app/i18n.dart';
import '../app/sample.dart';
import '../app/state.dart';
import '../ui/land.dart';
import '../ui/skin.dart';

final Uint8List _land = base64.decode(landBase64);

bool _cell(int r, int c) {
  if (r < 0 || r >= 180) return false;
  final i = r * 360 + (c % 360 + 360) % 360;
  return (_land[i >> 3] >> (i & 7)) & 1 == 1;
}

/// How much of a spot is land, 0 to 1. The mask is one degree to a cell, which
/// a phone's view can magnify to a dot or more: blending the four cells around
/// the spot rounds the coast instead of drawing its staircase.
double _landAt(double lat, double lon) {
  final fr = 90 - lat - .5, fc = lon + 180 - .5;
  final r = fr.floor(), c = fc.floor();
  final dr = fr - r, dc = fc - c;
  double v(int a, int b) => _cell(a, b) ? 1 : 0;
  return v(r, c) * (1 - dr) * (1 - dc) + v(r, c + 1) * (1 - dr) * dc + v(r + 1, c) * dr * (1 - dc) + v(r + 1, c + 1) * dr * dc;
}

double _unwrap(double lon, double around) {
  while (lon - around > 180) {
    lon -= 360;
  }
  while (lon - around < -180) {
    lon += 360;
  }
  return lon;
}

class _View {
  _View(this.lon0, this.lon1, this.lat0, this.lat1);
  double lon0, lon1, lat0, lat1;
}

/// The area that shows every place, shaped like the box it's drawn in, the
/// same way the desktop map finds it. [top] is the part of the box something
/// else covers: the view grows until the northernmost place sits below it.
_View _fit(List<(double, double)> at, double around, double aspect, double top) {
  var lon0 = 1e9, lon1 = -1e9, lat0 = 1e9, lat1 = -1e9;
  for (final (lat, lon) in at) {
    final u = _unwrap(lon, around);
    lon0 = math.min(lon0, u);
    lon1 = math.max(lon1, u);
    lat0 = math.min(lat0, lat);
    lat1 = math.max(lat1, lat);
  }
  if (lon0 > lon1) (lon0, lon1, lat0, lat1) = (96.0, 150.0, -4.0, 46.0);
  final cx = (lon0 + lon1) / 2;
  var cy = (lat0 + lat1) / 2;
  var w = math.max(lon1 - lon0, 40.0) * 1.3, h = math.max(lat1 - lat0, 24.0) * 1.45;
  final k = math.cos(cy * math.pi / 180);
  if (w * k / h < aspect) {
    w = h * aspect / k;
  } else {
    h = w * k / aspect;
  }
  h = math.min(h, 150.0);
  cy = math.max(-75 + h / 2, math.min(80 - h / 2, cy));
  final v = _View(cx - w / 2, cx + w / 2, cy - h / 2 - h * .04, cy + h / 2 - h * .04);
  if (top > 0 && at.isNotEmpty) {
    final north = at.map((p) => p.$1).reduce(math.max);
    final grown = (north - top * v.lat0) / (1 - top);
    if (grown > v.lat1) {
      final more = (v.lon1 - v.lon0) * ((grown - v.lat0) / (v.lat1 - v.lat0) - 1) / 2;
      v.lat1 = grown;
      v.lon0 -= more;
      v.lon1 += more;
    }
  }
  return v;
}

class _Arc {
  _Arc(this.lat, this.lon);
  double lat, lon, width = 0, alpha = 0, grown = 0;
}

/// Everything the map keeps between frames. It eases toward its targets, so a
/// route that changes exit swings across rather than jumping.
class _MapState {
  final arcs = <String, _Arc>{};
  double pad = 0, grid = -1;
  _View? view;
  double youLat = 0, youLon = 0;
  bool busy = true;
  String? dotsKey;
  Float32List inland = Float32List(0), coast = Float32List(0);
  final labels = <String, TextPainter>{};
}

class ExitMap extends StatefulWidget {
  const ExitMap({super.key, required this.state, required this.expanded});

  final AppState state;
  final bool expanded;

  @override
  State<ExitMap> createState() => _ExitMapState();
}

class _ExitMapState extends State<ExitMap> with SingleTickerProviderStateMixin {
  late final Ticker _ticker;
  final _s = _MapState();
  final _tick = ValueNotifier<int>(0);
  Duration _last = Duration.zero;
  double _dt = 1 / 60;
  int _frame = 0;

  @override
  void initState() {
    super.initState();
    _ticker = createTicker((elapsed) {
      // With nothing moving but the pulse around you, 20 frames a second do.
      if (!_s.busy && ++_frame % 3 != 0) return;
      _dt = ((elapsed - _last).inMicroseconds / 1e6).clamp(1 / 240, 1 / 15);
      _last = elapsed;
      _tick.value++;
    })
      ..start();
  }

  @override
  void dispose() {
    _ticker.dispose();
    _tick.dispose();
    for (final l in _s.labels.values) {
      l.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return RepaintBoundary(
      child: CustomPaint(
        size: Size.infinite,
        painter: _MapPainter(
          repaint: _tick,
          app: widget.state,
          expanded: widget.expanded,
          look: Skin.of(context),
          s: _s,
          dt: () => _dt,
          reduced: MediaQuery.disableAnimationsOf(context),
        ),
      ),
    );
  }
}

class _Line {
  _Line(this.route, this.node, this.place, this.live, this.arc, this.at, this.bend);
  final RouteCfg route;
  final Node node;
  final Place? place;
  final bool live;
  final _Arc arc;
  final Offset at;
  final double bend;
  String get label => place == null ? '?' : (node.city.isEmpty ? regionName(node.flag) : node.city);
}

const _growIn = Cubic(.45, 0, .2, 1);

/// The land's dots, in points apart.
const _pitch = 6.0;

/// How long, in milliseconds, a comet takes along its arc.
const _flow = 3400;

class _MapPainter extends CustomPainter {
  _MapPainter({required Listenable repaint, required this.app, required this.expanded, required this.look, required this.s, required this.dt, required this.reduced}) : super(repaint: repaint);

  final AppState app;
  final bool expanded, reduced;
  final Look look;
  final _MapState s;
  final double Function() dt;

  double _ease(double rate) => 1 - math.pow(1 - rate, dt() * 60).toDouble();
  double _lerp(double a, double b, double k) => a + (b - a) * k;

  (Size, void Function(Canvas, Offset)) _text(String text, bool bold, Color color) {
    final key = '$text|$bold|${look.id}|${look.isDark}|${color.toARGB32()}';
    TextPainter make(bool stroke) {
      final base = look.text(bold ? 13 : 11.5, weight: bold ? 700 : 500, lh: bold ? 16 : 14, color: color);
      final style = stroke
          ? base.copyWith(
              color: null,
              foreground: Paint()
                ..style = PaintingStyle.stroke
                ..strokeWidth = 3
                ..strokeJoin = StrokeJoin.round
                ..color = look.paper,
              shadows: const [])
          : base.copyWith(shadows: const []);
      return TextPainter(text: TextSpan(text: text, style: style), textDirection: TextDirection.ltr)..layout();
    }

    final stroke = s.labels['$key|s'] ??= make(true);
    final fill = s.labels[key] ??= make(false);
    return (
      fill.size,
      (c, o) {
        stroke.paint(c, o);
        fill.paint(c, o);
      }
    );
  }

  /// Puts a label beside a dot of radius [r], on the first side with room.
  /// Exits sit close together at this size, and no label may cover another.
  void _place(Canvas c, String text, Offset at, double r, bool bold, Color color, Size box, List<Rect> taken, {bool must = false}) {
    final (size, draw) = _text(text, bold, color);
    final tw = size.width, th = size.height, gap = r + 5;
    final sides = [
      Offset(at.dx + gap, at.dy - th / 2),
      Offset(at.dx - gap - tw, at.dy - th / 2),
      Offset(at.dx - tw / 2, at.dy - gap - th + 2),
      Offset(at.dx - tw / 2, at.dy + gap - 2),
    ];
    for (final o in sides) {
      final rect = o & size;
      if (rect.left < 6 || rect.right > box.width - 6 || rect.top < 0 || rect.bottom > box.height) continue;
      if (taken.any((t) => t.overlaps(rect))) continue;
      taken.add(rect);
      draw(c, o);
      return;
    }
    if (!must) return;
    final o = Offset(sides[0].dx.clamp(6.0, math.max(6.0, box.width - 6 - tw)), sides[0].dy.clamp(0.0, math.max(0.0, box.height - th)));
    taken.add(o & size);
    draw(c, o);
  }

  void _drawLand(Canvas canvas, _View v, Offset anchor, double w, double h, double hh) {
    // Opened, the card's caption sits on the top left and its list covers the
    // bottom: land under either would only get in the way of the words.
    final captioned = expanded && s.pad > 10;
    final bottom = captioned ? hh + 6 : h;
    final key = [v.lon0, v.lon1, v.lat0, v.lat1, anchor.dx, anchor.dy, w, bottom, captioned ? 1.0 : 0.0].map((e) => e.toStringAsFixed(2)).join('|');
    if (s.dotsKey != key) {
      s.dotsKey = key;
      final pxLon = w / (v.lon1 - v.lon0), pxLat = hh / (v.lat1 - v.lat0);
      final inland = <double>[], coast = <double>[];
      final stepLon = _pitch / pxLon / 3, stepLat = _pitch / pxLat / 3;
      final wide = stepLon > .5;
      final x0 = anchor.dx + ((0 - anchor.dx) / _pitch).ceil() * _pitch;
      final y0 = anchor.dy + ((0 - anchor.dy) / _pitch).ceil() * _pitch;
      for (var y = y0; y < bottom; y += _pitch) {
        final lat = v.lat1 - y / pxLat;
        if (lat <= -89 || lat >= 89) continue;
        for (var x = x0; x < w; x += _pitch) {
          if (captioned && y < 66 && x < 150) continue;
          final lon = v.lon0 + x / pxLon;
          // Zoomed out, a dot stands for several cells: it takes them all in.
          var c = 0.0;
          if (wide) {
            for (var i = -1; i <= 1; i++) {
              for (var j = -1; j <= 1; j++) {
                c += _landAt(lat + i * stepLat, lon + j * stepLon) / 9;
              }
            }
          } else {
            c = _landAt(lat, lon);
          }
          if (c >= .55) {
            inland.addAll([x, y]);
          } else if (c >= .3) {
            coast.addAll([x, y]);
          }
        }
      }
      s.inland = Float32List.fromList(inland);
      s.coast = Float32List.fromList(coast);
    }
    final dots = Paint()
      ..strokeCap = StrokeCap.round
      ..color = look.ink.withValues(alpha: .62 * s.grid);
    canvas.drawRawPoints(ui.PointMode.points, s.inland, dots..strokeWidth = 2.3);
    canvas.drawRawPoints(ui.PointMode.points, s.coast, dots..strokeWidth = 1.5);
  }

  @override
  void paint(Canvas canvas, Size size) {
    final w = size.width, h = size.height;
    if (w <= 0 || h <= 0) return;
    final ease = _ease(.13), slow = _ease(.1), on = app.on;

    final proxied = app.proxied;
    // The opened card lists every line over the map's lower part.
    s.pad = _lerp(s.pad, expanded ? 56.0 + proxied.length * 50 : 0, ease);
    final hh = math.max(40.0, h - s.pad);
    final small = h < 100;

    bool isLive(RouteCfg r) => on && (app.mode == 'rule' || (app.mode == 'global' && r.id == 'default'));
    final you = app.you;
    final around = you?.lon ?? 120.0;
    final known = <(double, double)>[if (you != null) (you.lat, you.lon)];
    final where = <(RouteCfg, Node, Place?)>[];
    for (final r in proxied) {
      final n = node(r.node!);
      final pl = placeOf(n);
      where.add((r, n, pl));
      if (pl != null) known.add((pl.lat, pl.lon));
    }

    // Squeezed to a strip, the status pill takes the left of it: the lines keep
    // to the rest, and only the land runs on underneath.
    final left = small ? math.min(156.0, w * .4) : 0.0;
    final target = _fit(known, around, (w - left) / hh, h >= 170 ? 50 / hh : 0);
    target.lon0 -= left / (w - left) * (target.lon1 - target.lon0);
    final first = s.view == null;
    final v = s.view ??= _View(target.lon0, target.lon1, target.lat0, target.lat1);
    final moved = (v.lon0 - target.lon0).abs() + (v.lon1 - target.lon1).abs() + (v.lat0 - target.lat0).abs() + (v.lat1 - target.lat1).abs() > .02;
    if (moved) {
      v.lon0 = _lerp(v.lon0, target.lon0, ease);
      v.lon1 = _lerp(v.lon1, target.lon1, ease);
      v.lat0 = _lerp(v.lat0, target.lat0, ease);
      v.lat1 = _lerp(v.lat1, target.lat1, ease);
    } else {
      v
        ..lon0 = target.lon0
        ..lon1 = target.lon1
        ..lat0 = target.lat0
        ..lat1 = target.lat1;
    }
    Offset at(double lat, double lon) => Offset((lon - v.lon0) / (v.lon1 - v.lon0) * w, (v.lat1 - lat) / (v.lat1 - v.lat0) * hh);
    (double, double) inView(double fx, double fy) => (target.lat1 - fy * (target.lat1 - target.lat0), target.lon0 + fx * (target.lon1 - target.lon0));

    final (ylat, ylon) = you != null ? (you.lat, _unwrap(you.lon, around)) : inView(.5, .55);
    if (first) {
      s.youLat = ylat;
      s.youLon = ylon;
    }
    s.youLat = _lerp(s.youLat, ylat, ease);
    s.youLon = _lerp(s.youLon, ylon, ease);
    final origin = at(s.youLat, s.youLon);

    final gridTo = on ? .6 : .25;
    s.grid = s.grid < 0 ? gridTo : _lerp(s.grid, gridTo, ease);
    _drawLand(canvas, v, you != null ? origin : at(0, 0), w, h, hh);

    // Lines to the same exit fan out, or only the last would show.
    final sameExit = <String, int>{};
    final lines = <_Line>[];
    for (final (r, n, pl) in where) {
      final (lat, lon) = pl != null ? (pl.lat, _unwrap(pl.lon, around)) : inView(.64, .8);
      final a = s.arcs[r.id] ??= _Arc(lat, lon);
      final lv = isLive(r);
      a.lat = _lerp(a.lat, lat, ease);
      a.lon = _lerp(a.lon, lon, ease);
      a.width = _lerp(a.width, lv ? 1.2 + (share[r.id] ?? 0) * 6 : (on ? 1 : 0), slow);
      a.alpha = _lerp(a.alpha, lv ? .9 : (on ? .3 : 0), slow);
      a.grown = reduced ? (on ? 1 : 0) : (a.grown + dt() / 1.1 * (on ? 1 : -1)).clamp(0.0, 1.0);
      final fan = pl == null ? 0 : (sameExit[n.flag + n.city] = (sameExit[n.flag + n.city] ?? -1) + 1);
      lines.add(_Line(r, n, pl, lv, a, at(a.lat, a.lon), 1 + fan * .42));
    }

    // A strip of a few dozen points has no room for a tall arc.
    final flat = small ? .4 : 1.0;
    var comets = false;
    final now = DateTime.now().millisecondsSinceEpoch;
    final phase = (now % _flow) / _flow;
    for (final (i, l) in lines.indexed) {
      final grown = _growIn.transform(l.arc.grown);
      if (l.arc.alpha < .01 || grown < .01) continue;
      final end = l.at, d = end - origin, len = d.distance;
      if (len < 1) continue;
      final mid = (origin + end) / 2;
      final path = Path()
        ..moveTo(origin.dx, origin.dy)
        ..quadraticBezierTo(mid.dx - d.dy * .22 * l.bend * flat, mid.dy - len * .28 * l.bend * flat, end.dx, end.dy);
      final metric = path.computeMetrics().first;
      final total = metric.length;
      final col = look.route(l.route.id);
      final width = math.max(l.arc.width, .5);
      final trimmed = grown < 1 ? metric.extractPath(0, total * grown) : path;
      if (l.place == null) {
        canvas.drawPath(
          _dashed(trimmed, 3, 5),
          Paint()
            ..style = PaintingStyle.stroke
            ..strokeCap = StrokeCap.round
            ..strokeWidth = width
            ..color = col.withValues(alpha: l.arc.alpha),
        );
        continue;
      }
      // Fainter where it leaves you, full at the exit.
      canvas.drawPath(
        trimmed,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeCap = StrokeCap.round
          ..strokeWidth = width
          ..shader = ui.Gradient.linear(origin, end, [col.withValues(alpha: l.arc.alpha * .3), col.withValues(alpha: l.arc.alpha * .85)]),
      );
      if (reduced || !on || l.arc.alpha < .5 || grown < .99) continue;
      comets = true;
      // Data on the move, two to an arc, quicker in the middle of the way.
      final cw = math.max(width * .9, 2.0);
      for (var k = 0; k < 2; k++) {
        final t = (phase + i * .37 + k / 2) % 1;
        final p = t - .55 * math.sin(2 * math.pi * t) / (2 * math.pi);
        final fade = math.min(1.0, math.min(p / .08, (1 - p) / .1));
        final trail = math.min(.2, 64 / math.max(total, 1));
        const pieces = 6;
        for (var j = 0; j < pieces; j++) {
          final f0 = p - trail * (pieces - j) / pieces;
          final f1 = p - trail * (pieces - j - 1) / pieces;
          if (f1 <= 0) continue;
          final q = (j + 1) / pieces;
          canvas.drawPath(
            metric.extractPath(math.max(f0, 0) * total, f1 * total),
            Paint()
              ..style = PaintingStyle.stroke
              ..strokeCap = StrokeCap.round
              ..strokeWidth = cw * (.45 + .55 * q)
              ..color = col.withValues(alpha: fade * q * q),
          );
        }
        final head = metric.getTangentForOffset(p * total)?.position;
        if (head != null) {
          final hr = cw / 2 + 1.2;
          canvas.drawCircle(head, hr + 3, Paint()..color = col.withValues(alpha: .18 * fade));
          canvas.drawCircle(head, hr, Paint()..color = col.withValues(alpha: fade));
          canvas.drawCircle(head, math.max(hr * .45, .9), Paint()..color = const Color(0xFFFFFFFF).withValues(alpha: .95 * fade));
        }
        // It lands: a ripple at the exit, as the next one sets off.
        if (t < .3) {
          final u = t / .3;
          canvas.drawCircle(
            end,
            5 + 11 * u,
            Paint()
              ..style = PaintingStyle.stroke
              ..strokeWidth = 1.2 * (1 - u) + .3
              ..color = col.withValues(alpha: .45 * (1 - u) * (1 - u)),
          );
        }
      }
    }

    final box = Size(w, hh);
    final col = on ? look.ink : look.faint;
    canvas.drawCircle(origin, 5, Paint()..color = col);
    if (!reduced) {
      final ph = (now / 60) % 14;
      canvas.drawCircle(
        origin,
        8 + ph,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1
          ..color = col.withValues(alpha: math.max(0, .3 - ph / 46)),
      );
    }
    final taken = <Rect>[Rect.fromCircle(center: origin, radius: 7)];
    if (h < 300 || expanded) taken.add(const Offset(0, 0) & const Size(150, 44));
    if (!small) _place(canvas, tr('你', 'You'), origin, 5, true, on ? look.ink : look.mute, box, taken, must: true);

    final head = lines.where((l) => l.place != null).firstOrNull;
    final order = [...lines]..sort((a, b) => (identical(b, head) ? 1 : 0) - (identical(a, head) ? 1 : 0));
    final seen = <String>{};
    for (final l in order) {
      if (l.arc.alpha < .05 || !seen.add(l.label)) continue;
      final alpha = math.min(1.0, l.arc.alpha * 1.4);
      if (l.place == null) {
        canvas.drawPath(
          _dashed(Path()..addOval(Rect.fromCircle(center: l.at, radius: 9)), 2, 3),
          Paint()
            ..style = PaintingStyle.stroke
            ..strokeWidth = 1.5
            ..color = look.warn.withValues(alpha: alpha),
        );
        if (!small) _place(canvas, tr('位置未知', 'Unknown location'), l.at, 9, true, look.warn, box, taken);
        continue;
      }
      final main = head != null && head.label == l.label;
      final r = main ? 6.0 : 4.0;
      canvas.drawCircle(l.at, r, Paint()..color = look.paper.withValues(alpha: alpha));
      canvas.drawCircle(
        l.at,
        r,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = main ? 2.5 : 1.5
          ..color = look.ink.withValues(alpha: alpha),
      );
      if (!small) _place(canvas, l.label, l.at, r, main, look.ink, box, taken, must: main);
    }

    s.busy = moved || comets || !reduced && lines.any((l) => l.arc.grown > 0 && l.arc.grown < 1) || (s.pad - (expanded ? 56.0 + proxied.length * 50 : 0)).abs() > .5;
  }

  Path _dashed(Path source, double on, double off) {
    final out = Path();
    for (final m in source.computeMetrics()) {
      var d = 0.0;
      while (d < m.length) {
        out.addPath(m.extractPath(d, math.min(d + on, m.length)), Offset.zero);
        d += on + off;
      }
    }
    return out;
  }

  @override
  bool shouldRepaint(_MapPainter old) => !identical(old.look, look) || old.expanded != expanded;
}
