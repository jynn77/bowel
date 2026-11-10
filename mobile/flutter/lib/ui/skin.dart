import 'dart:math' as math;

import 'package:flutter/foundation.dart' show TargetPlatform, defaultTargetPlatform;
import 'package:flutter/widgets.dart';

import 'skin_data.dart';

class Palette {
  const Palette({
    required this.paper,
    required this.paper2,
    required this.card,
    required this.sheet,
    required this.ink,
    required this.ink2,
    required this.mute,
    required this.faint,
    required this.hair,
    required this.hair2,
    required this.hover,
    required this.signal,
    required this.good,
    required this.warn,
    required this.routes,
  });

  final Color paper, paper2, card, sheet, ink, ink2, mute, faint, hair, hair2, hover, signal, good, warn;
  final List<Color> routes;
}

class SkinDef {
  const SkinDef({
    required this.name,
    required this.accentL,
    required this.accentD,
    required this.light,
    required this.dark,
    required this.font,
    required this.disp,
    required this.round,
    required this.glow,
  });

  final String name, font, disp;
  final Color? accentL, glow;
  final Color accentD;
  final Palette? light, dark;
  final double round;
}

/// The theme colors offered as swatches, the desktop's. Some need a darker or
/// lighter color on one paper: a lime that glows on graphite is all but white
/// on paper. "ink" is none, the interface in black and white.
const accentPresets = <(String, String?, String?)>[
  ('#DDFB4C', '#7A9E00', null),
  ('#4B3BF5', null, '#7B6DFF'),
  ('#FF6A3D', '#E8552A', null),
  ('#14B8A6', '#0E9F8F', null),
  ('#FF5FA2', '#E54888', null),
  ('ink', null, null),
];

bool validAccent(String a) => a == 'ink' || RegExp(r'^#[0-9a-fA-F]{6}$').hasMatch(a);

Color? _hex(String a) => RegExp(r'^#[0-9a-fA-F]{6}$').hasMatch(a) ? Color(0xFF000000 | int.parse(a.substring(1), radix: 16)) : null;

/// The color drawn for a theme color on the given paper; null for none set.
Color? accentColor(String a, bool dark) {
  for (final (hex, light, night) in accentPresets) {
    if (hex.toLowerCase() == a.toLowerCase()) return _hex((dark ? night : light) ?? hex);
  }
  return _hex(a);
}

const routeIds = ['default', 'ai', 'media', 'dev', 'direct'];

double _channel(double v) => v <= .04045 ? v / 12.92 : math.pow((v + .055) / 1.055, 2.4).toDouble();
double _lum(Color c) => .2126 * _channel(c.r) + .7152 * _channel(c.g) + .0722 * _channel(c.b);

double contrast(Color a, Color b) {
  final x = _lum(a), y = _lum(b);
  return (math.max(x, y) + .05) / (math.min(x, y) + .05);
}

/// Small text must reach 4.5:1 on a phone in daylight, so a color that doesn't
/// is mixed toward the ink until it does.
Color _legible(Color c, Color bg, Color ink) {
  var t = 0.0;
  var r = c;
  while (contrast(r, bg) < 4.5 && t < 1) {
    t += .04;
    r = Color.lerp(c, ink, t)!;
  }
  return r;
}

/// OPPO Sans reads thin on iOS beside the system's own labels. The lighter the
/// weight the more it gains, and 700, the top of the font's axis, stays put, so
/// the steps between weights hold.
double _opticalWeight(int weight) {
  if (defaultTargetPlatform != TargetPlatform.iOS || weight >= 700) return weight.toDouble();
  return weight + (700 - weight) / 6;
}

/// A skin resolved for one appearance: every color and font a widget reads.
class Look {
  Look._({
    required this.id,
    required this.isDark,
    required this.paper,
    required this.paper2,
    required this.card,
    required this.sheet,
    required this.ink,
    required this.ink2,
    required this.mute,
    required this.faint,
    required this.hair,
    required this.hair2,
    required this.hover,
    required this.signal,
    required this.good,
    required this.warn,
    required this.goodT,
    required this.warnT,
    required this.signalT,
    required this.accent,
    required this.onAccent,
    required this.segOn,
    required this.routes,
    required this.font,
    required this.disp,
    required this.round,
    required this.glow,
  });

  /// [custom] is the user's theme color, as [accentPresets] names one or as
  /// #RRGGBB; "" keeps the skin's own.
  factory Look.of(String skin, bool dark, {String custom = ''}) {
    final k = skinDefs[skin] ?? skinDefs['paper']!;
    final p = (dark ? k.dark : null) ?? k.light ?? k.dark!;
    final isDark = identical(p, k.dark);
    final accent = custom == 'ink' ? p.ink : (accentColor(custom, isDark) ?? (isDark ? k.accentD : k.accentL!));
    const darkInk = Color(0xFF171615), white = Color(0xFFFFFFFF);
    return Look._(
      id: skin,
      isDark: isDark,
      paper: p.paper,
      paper2: p.paper2,
      card: p.card,
      sheet: p.sheet,
      ink: p.ink,
      ink2: p.ink2,
      mute: _legible(p.mute, p.sheet, p.ink),
      faint: p.faint,
      hair: p.hair,
      hair2: p.hair2,
      hover: p.hover,
      signal: p.signal,
      good: p.good,
      warn: p.warn,
      goodT: _legible(p.good, p.sheet, p.ink),
      warnT: _legible(p.warn, p.sheet, p.ink),
      signalT: _legible(p.signal, p.sheet, p.ink),
      accent: accent,
      onAccent: custom == 'ink' ? p.paper : (contrast(darkInk, accent) >= contrast(white, accent) ? darkInk : white),
      segOn: isDark ? Color.lerp(p.paper2, p.ink, .2)! : p.sheet,
      routes: p.routes,
      font: k.font,
      disp: k.disp,
      round: k.round,
      glow: k.glow,
    );
  }

  final String id, font, disp;
  final bool isDark;
  final Color paper, paper2, card, sheet, ink, ink2, mute, faint, hair, hair2, hover;
  final Color signal, good, warn, goodT, warnT, signalT, accent, onAccent, segOn;
  final List<Color> routes;
  final double round;
  final Color? glow;

  /// The glass follows the skin's paper, not the system's appearance.
  bool get darkGlass => paper.r * .3 + paper.g * .59 + paper.b * .11 < .5;

  /// An opened card sits on `sheet`, so its quiet controls are mixed from it.
  Look get expanded => Look._(
        id: id,
        isDark: isDark,
        paper: paper,
        paper2: Color.lerp(sheet, ink, .09)!,
        card: card,
        sheet: sheet,
        ink: ink,
        ink2: ink2,
        mute: mute,
        faint: faint,
        hair: hair,
        hair2: hair2,
        hover: hover,
        signal: signal,
        good: good,
        warn: warn,
        goodT: goodT,
        warnT: warnT,
        signalT: signalT,
        accent: accent,
        onAccent: onAccent,
        segOn: segOn,
        routes: routes,
        font: font,
        disp: disp,
        round: round,
        glow: glow,
      );

  /// A route's colour. A group the user made has none of its own: it takes
  /// a steady one from its id.
  Color route(String id) {
    final i = routeIds.indexOf(id);
    if (i >= 0) return routes[i];
    return routes[1 + id.codeUnits.fold(0, (a, b) => a + b) % math.max(1, routes.length - 2)];
  }

  double r(double px) => px * round;
  BorderRadius br(double px) => BorderRadius.circular(px * round);

  List<BoxShadow> get lit => glow == null ? const [] : [BoxShadow(color: accent.withValues(alpha: .5), blurRadius: 14)];
  List<Shadow>? get textGlow => glow == null ? null : [Shadow(color: glow!, blurRadius: 12)];

  /// `lh` is CSS's line height in pixels, `ls` its letter spacing in em.
  TextStyle text(double size, {int weight = 400, Color? color, bool display = false, double? lh, double ls = 0, bool glowing = false}) {
    final family = display ? disp : font;
    return TextStyle(
      fontFamily: family,
      fontFamilyFallback: const ['OPPO Sans', 'PingFang SC'],
      fontSize: size,
      fontWeight: FontWeight.values[(weight / 100).round() - 1],
      fontVariations: family == 'OPPO Sans' ? [FontVariation('wght', _opticalWeight(weight))] : null,
      color: color ?? ink,
      height: (lh ?? size * 1.5) / size,
      leadingDistribution: TextLeadingDistribution.even,
      letterSpacing: ls * size,
      shadows: glowing || display ? textGlow : null,
      decoration: TextDecoration.none,
    );
  }
}

class Skin extends InheritedWidget {
  const Skin({super.key, required this.look, required super.child});

  final Look look;

  static Look of(BuildContext context) => context.dependOnInheritedWidgetOfExactType<Skin>()!.look;

  @override
  bool updateShouldNotify(Skin oldWidget) => !identical(look, oldWidget.look);
}
