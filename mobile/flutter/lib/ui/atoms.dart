import 'package:flutter/material.dart' hide Chip;
import 'package:flutter_svg/flutter_svg.dart';

import '../app/i18n.dart';
import '../app/sample.dart';
import 'flags.dart';
import 'logo_path.dart';
import 'skin.dart';
import 'skin_data.dart';

/// The shared easing of cards, accordions and flights.
const easeCard = Cubic(.32, .86, .22, 1);

const _icons = {
  'chev': '<path d="m9 6 6 6-6 6"/>',
  'x': '<path d="M6 6l12 12M18 6 6 18"/>',
  'check': '<path d="m5 12 5 5 9-10"/>',
  'search': '<circle cx="11" cy="11" r="6.5"/><path d="m16 16 4.5 4.5"/>',
  'scan': '<path d="M4 8V5.5A1.5 1.5 0 0 1 5.5 4H8M16 4h2.5A1.5 1.5 0 0 1 20 5.5V8M20 16v2.5a1.5 1.5 0 0 1-1.5 1.5H16M8 20H5.5A1.5 1.5 0 0 1 4 18.5V16M4 12h16"/>',
  'file': '<path d="M7 3.5h7l4 4V20a.5.5 0 0 1-.5.5h-10.500A.5.5 0 0 1 6.500 20V4a.5.5 0 0 1 .5-.5Z"/><path d="M14 3.500V8h4"/>',
  'pause': '<path d="M9 6v12M15 6v12"/>',
  'play': '<path d="M8 5.500v13l10-6.500Z"/>',
  'plus': '<path d="M12 5v14M5 12h14"/>',
  'more': '<path d="M5.500 12h.01M12 12h.01M18.500 12h.01" stroke-width="2.600"/>',
  'up': '<path d="m6 15 6-6 6 6"/>',
  'down': '<path d="m6 9 6 6 6-6"/>',
  'copy': '<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5.500 15H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1h9a1 1 0 0 1 1 1v.500"/>',
};

class Ic extends StatelessWidget {
  const Ic(this.name, {super.key, this.size = 20, this.color});

  final String name;
  final double size;
  final Color? color;

  @override
  Widget build(BuildContext context) => SvgPicture.string(
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#000" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${_icons[name]}</svg>',
        width: size,
        height: size,
        colorFilter: ColorFilter.mode(color ?? DefaultTextStyle.of(context).style.color ?? Skin.of(context).ink, BlendMode.srcIn),
      );
}

/// The app's icon inside the app: the skin's color as chosen, not the darker
/// one that reads as a line on paper, with the logo's line on it.
class LogoTile extends StatelessWidget {
  const LogoTile({super.key, this.size = 44});

  final double size;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final fill = (skinDefs[l.id] ?? skinDefs['paper']!).accentD;
    const ink = Color(0xFF171615), white = Color(0xFFFFFFFF);
    final line = contrast(ink, fill) >= contrast(white, fill) ? ink : white;
    return ExcludeSemantics(
      child: Container(
        width: size,
        height: size,
        alignment: Alignment.center,
        decoration: BoxDecoration(color: fill, borderRadius: BorderRadius.circular(size * .225)),
        child: SvgPicture.string(
          '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"><path d="$logoPathData" fill="none" stroke="#000" stroke-width="$logoPathWidth" stroke-linecap="round" stroke-linejoin="round"/></svg>',
          width: size * .72 * 200 / 194,
          height: size * .72 * 200 / 194,
          colorFilter: ColorFilter.mode(line, BlendMode.srcIn),
        ),
      ),
    );
  }
}

class FlagDot extends StatelessWidget {
  const FlagDot(this.flag, this.size, {super.key});

  final String flag;
  final double size;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final svg = flagSvgs[flag];
    if (svg == null) {
      return Container(width: size, height: size, decoration: BoxDecoration(shape: BoxShape.circle, border: Border.all(color: l.mute, width: 1.5)));
    }
    return Container(
      width: size,
      height: size,
      foregroundDecoration: BoxDecoration(shape: BoxShape.circle, border: Border.all(color: l.hair, width: 1)),
      child: ClipOval(child: SvgPicture.string('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512">$svg</svg>', width: size, height: size)),
    );
  }
}

/// Dims under the finger. Everything tappable goes through this.
class Tap extends StatefulWidget {
  const Tap({super.key, required this.onTap, required this.child, this.pressedColor, this.label});

  final VoidCallback? onTap;
  final Widget child;
  final String? label;

  /// When set, the press tints this widget's background instead of dimming it.
  final Color? pressedColor;

  @override
  State<Tap> createState() => _TapState();
}

class _TapState extends State<Tap> {
  bool _down = false;

  void _set(bool v) {
    if (_down != v && mounted) setState(() => _down = v);
  }

  @override
  Widget build(BuildContext context) {
    final tinted = widget.pressedColor != null;
    return Semantics(
      button: true,
      label: widget.label,
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTapDown: widget.onTap == null ? null : (_) => _set(true),
        onTapCancel: () => _set(false),
        onTapUp: (_) => _set(false),
        onTap: widget.onTap,
        child: tinted
            ? ColoredBox(color: _down ? widget.pressedColor! : const Color(0x00000000), child: widget.child)
            : AnimatedOpacity(opacity: _down ? .55 : 1, duration: Duration(milliseconds: _down ? 40 : 160), child: widget.child),
      ),
    );
  }
}

/// One line, never wrapped.
class Line1 extends StatelessWidget {
  const Line1(this.text, this.style, {super.key, this.clip = false, this.align});

  final String text;
  final TextStyle style;
  final bool clip;
  final TextAlign? align;

  @override
  Widget build(BuildContext context) => Text(text, style: style, maxLines: 1, softWrap: false, textAlign: align, overflow: clip ? TextOverflow.clip : TextOverflow.ellipsis);
}

class Dot extends StatelessWidget {
  const Dot(this.color, {super.key});
  final Color color;

  @override
  Widget build(BuildContext context) => Container(width: 8, height: 8, decoration: BoxDecoration(color: color, shape: BoxShape.circle));
}

class Square extends StatelessWidget {
  const Square(this.color, {super.key, this.size = 10, this.radius = 3});
  final Color color;
  final double size, radius;

  @override
  Widget build(BuildContext context) => Container(width: size, height: size, decoration: BoxDecoration(color: color, borderRadius: Skin.of(context).br(radius)));
}

class Toggle extends StatelessWidget {
  const Toggle({super.key, required this.on, this.onTap, this.scale = 1, this.label});

  final bool on;
  final VoidCallback? onTap;
  final double scale;
  final String? label;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    const d = Duration(milliseconds: 180);
    final body = SizedBox(
      width: 58 * scale,
      height: 34 * scale,
      child: FittedBox(
        child: AnimatedContainer(
          duration: d,
          width: 58,
          height: 34,
          decoration: BoxDecoration(color: on ? l.accent : l.faint, borderRadius: l.br(17), boxShadow: on ? l.lit : const []),
          child: Stack(children: [
            AnimatedPositioned(
              duration: d,
              top: 3,
              left: on ? 27 : 3,
              width: 28,
              height: 28,
              child: Container(decoration: BoxDecoration(color: const Color(0xFFFFFFFF), borderRadius: l.br(14), boxShadow: const [BoxShadow(color: Color(0x4D000000), offset: Offset(0, 1), blurRadius: 3)])),
            ),
          ]),
        ),
      ),
    );
    if (onTap == null) return body;
    return Semantics(
      toggled: on,
      label: label,
      child: GestureDetector(behavior: HitTestBehavior.opaque, onTap: onTap, child: ConstrainedBox(constraints: const BoxConstraints(minHeight: 44), child: Center(widthFactor: 1, child: body))),
    );
  }
}

class Seg<T> extends StatelessWidget {
  const Seg({super.key, required this.options, required this.value, required this.onChanged, this.padH = 13, this.minH = 40, this.fontSize = 13.5, this.outer = 2});

  final List<(T, String)> options;
  final T value;
  final ValueChanged<T> onChanged;
  final double padH, minH, fontSize, outer;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    return Container(
      padding: EdgeInsets.all(outer),
      decoration: BoxDecoration(color: l.paper2, borderRadius: l.br(11)),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          for (final (i, o) in options.indexed) ...[
            if (i > 0) const SizedBox(width: 2),
            Tap(
              onTap: () => onChanged(o.$1),
              child: AnimatedContainer(
                duration: const Duration(milliseconds: 160),
                constraints: BoxConstraints(minHeight: minH),
                padding: EdgeInsets.symmetric(horizontal: padH),
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  color: o.$1 == value ? l.segOn : const Color(0x00000000),
                  borderRadius: l.br(8),
                  boxShadow: o.$1 == value ? const [BoxShadow(color: Color(0x1A000000), offset: Offset(0, 1), blurRadius: 2)] : const [],
                ),
                child: Text(o.$2, maxLines: 1, style: l.text(fontSize, weight: o.$1 == value ? 600 : 500, color: o.$1 == value ? l.ink : l.ink2, lh: 20)),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class Chip extends StatelessWidget {
  const Chip({super.key, required this.text, required this.on, required this.onTap, this.leading, this.count});

  final String text;
  final bool on;
  final VoidCallback onTap;
  final Widget? leading;
  final int? count;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final c = on ? l.paper : l.ink2;
    return Tap(
      onTap: onTap,
      child: Container(
        constraints: const BoxConstraints(minHeight: 36),
        padding: const EdgeInsets.symmetric(horizontal: 12),
        decoration: BoxDecoration(color: on ? l.ink : const Color(0x00000000), borderRadius: l.br(18)),
        child: Row(mainAxisSize: MainAxisSize.min, children: [
          if (leading != null) ...[leading!, const SizedBox(width: 5)],
          Text(text, maxLines: 1, style: l.text(13.5, weight: on ? 600 : 400, color: c, lh: 20)),
          if (count != null) ...[const SizedBox(width: 5), Opacity(opacity: .6, child: Text('$count', style: l.text(11.5, color: c, lh: 16)))],
        ]),
      ),
    );
  }
}

enum BtnKind { accent, outline, quiet, danger, plain }

class Btn extends StatelessWidget {
  const Btn(this.text, {super.key, required this.onTap, this.kind = BtnKind.quiet, this.minH = 40, this.padH = 14, this.fontSize = 14, this.leading, this.expand = false, this.child});

  final String text;
  final VoidCallback? onTap;
  final BtnKind kind;
  final double minH, padH, fontSize;
  final Widget? leading;
  final bool expand;

  /// Replaces the label, for a shimmering "working" state.
  final Widget? child;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final (bg, fg) = switch (kind) {
      BtnKind.accent => (l.accent, l.onAccent),
      BtnKind.outline => (null, l.ink),
      BtnKind.quiet => (l.paper2, l.ink),
      BtnKind.danger => (null, l.signalT),
      BtnKind.plain => (null, l.ink),
    };
    return Tap(
      onTap: onTap,
      child: Container(
        width: expand ? double.infinity : null,
        constraints: BoxConstraints(minHeight: minH),
        padding: EdgeInsets.symmetric(horizontal: padH),
        alignment: expand ? Alignment.center : null,
        decoration: BoxDecoration(
          color: bg,
          borderRadius: l.br(20),
          border: kind == BtnKind.outline ? Border.all(color: l.ink, width: 1.5) : null,
          boxShadow: kind == BtnKind.accent ? l.lit : const [],
        ),
        child: DefaultTextStyle.merge(
          style: TextStyle(color: fg),
          child: Row(mainAxisSize: MainAxisSize.min, mainAxisAlignment: MainAxisAlignment.center, children: [
            if (leading != null) ...[leading!, const SizedBox(width: 5)],
            child ?? Text(text, maxLines: 1, softWrap: false, style: l.text(fontSize, weight: 600, color: fg, lh: fontSize * 1.4)),
          ]),
        ),
      ),
    );
  }
}

/// The square icon button: close, search, back.
class IconBox extends StatelessWidget {
  const IconBox(this.icon, {super.key, required this.onTap, this.on = false, this.flip = false, this.label});

  final String icon;
  final VoidCallback onTap;
  final bool on, flip;
  final String? label;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    Widget g = Ic(icon, color: on ? l.onAccent : l.ink2);
    if (flip) g = Transform.flip(flipX: true, child: g);
    return Tap(
      onTap: onTap,
      label: label,
      child: Container(width: 44, height: 44, alignment: Alignment.center, decoration: BoxDecoration(color: on ? l.accent : l.paper2, borderRadius: l.br(12)), child: g),
    );
  }
}

class SourceTag extends StatelessWidget {
  const SourceTag(this.source, {super.key});
  final String source;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final (bg, fg) = switch (source) {
      '实测' => (l.good.withValues(alpha: .14), l.goodT),
      '按名称' => (l.mute.withValues(alpha: .16), l.mute),
      _ => (l.warn.withValues(alpha: .16), l.warnT),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 5),
      decoration: BoxDecoration(color: bg, borderRadius: l.br(5)),
      child: Text(
        switch (source) {
          '实测' => tr('实测', 'Measured'),
          '按名称' => tr('按名称', 'By name'),
          '未知' => tr('未知', 'Unknown'),
          _ => source,
        },
        style: l.text(11.5, weight: 600, color: fg, lh: 18),
      ),
    );
  }
}

class LatencyBars extends StatelessWidget {
  const LatencyBars(this.history, {super.key});
  final List<int> history;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    return SizedBox(
      height: 16,
      child: Row(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.end, children: [
        for (final (i, v) in history.indexed) ...[
          if (i > 0) const SizedBox(width: 2),
          Container(width: 3, height: v * 1.5, color: l.ink2.withValues(alpha: .75)),
        ],
      ]),
    );
  }
}

Color latColor(Look l, int d) => d < 0 ? l.mute : (d == 0 ? l.signalT : (d < 130 ? l.goodT : l.warnT));

class _StackPainter extends CustomPainter {
  _StackPainter(this.count, this.look) : stack = trafficStack, routeOrder = trafficRoutes;
  final int count;
  final List<String> routeOrder;
  final List<List<double>> stack;
  final Look look;

  @override
  void paint(Canvas canvas, Size size) {
    final cols = stack.length > count ? stack.sublist(stack.length - count) : stack;
    if (cols.isEmpty) return;
    final top = cols.map((c) => c.reduce((a, b) => a + b)).reduce((a, b) => a > b ? a : b);
    if (top <= 0) return;
    const gap = 2.0;
    final w = (size.width - gap * (count - 1)) / count;
    final p = Paint();
    for (var i = 0; i < cols.length; i++) {
      var y = size.height - cols[i].reduce((a, b) => a + b) / top * size.height;
      for (var k = 0; k < cols[i].length; k++) {
        final h = cols[i][k] / top * size.height;
        p.color = look.route(k < routeOrder.length ? routeOrder[k] : '$k');
        canvas.drawRect(Rect.fromLTWH(i * (w + gap), y, w, h), p);
        y += h;
      }
    }
  }

  @override
  bool shouldRepaint(_StackPainter old) => old.count != count || !identical(old.look, look) || !identical(old.stack, stack);
}

class TrafficStack extends StatelessWidget {
  const TrafficStack(this.count, this.height, {super.key});
  final int count;
  final double height;

  @override
  Widget build(BuildContext context) => SizedBox(height: height, width: double.infinity, child: CustomPaint(painter: _StackPainter(count, Skin.of(context))));
}

/// A working state: the words shimmer in place, so nothing shifts.
class Shim extends StatefulWidget {
  const Shim({super.key, required this.active, required this.child});
  final bool active;
  final Widget child;

  @override
  State<Shim> createState() => _ShimState();
}

class _ShimState extends State<Shim> with SingleTickerProviderStateMixin {
  late final _c = AnimationController(vsync: this, duration: const Duration(seconds: 1));

  @override
  void initState() {
    super.initState();
    if (widget.active) _c.repeat();
  }

  @override
  void didUpdateWidget(Shim old) {
    super.didUpdateWidget(old);
    if (widget.active && !_c.isAnimating) _c.repeat();
    if (!widget.active && _c.isAnimating) _c.stop();
  }

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    return AnimatedBuilder(
      animation: _c,
      child: widget.child,
      builder: (context, child) {
        if (!widget.active) return child!;
        return ShaderMask(
          blendMode: BlendMode.srcIn,
          shaderCallback: (b) => LinearGradient(
            begin: Alignment.centerLeft,
            end: Alignment.centerRight,
            colors: [l.ink, l.faint, l.ink],
            stops: const [.3, .5, .7],
            tileMode: TileMode.repeated,
            transform: _Slide(-b.width * 2.2 * _c.value),
          ).createShader(Rect.fromLTWH(0, 0, b.width * 2.2, b.height)),
          child: child,
        );
      },
    );
  }
}

class _Slide extends GradientTransform {
  const _Slide(this.dx);
  final double dx;

  @override
  Matrix4 transform(Rect bounds, {TextDirection? textDirection}) => Matrix4.translationValues(dx, 0, 0);
}

/// Where a latency will appear, while it's being measured.
class Skeleton extends StatelessWidget {
  const Skeleton({super.key});

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    return Shim(active: true, child: Container(width: 36, height: 11, decoration: BoxDecoration(color: l.paper2, borderRadius: BorderRadius.circular(4))));
  }
}

/// Opens and closes its child in place; the child is only built while shown.
class Accordion extends StatefulWidget {
  const Accordion({super.key, required this.open, required this.builder});

  final bool open;
  final WidgetBuilder builder;

  @override
  State<Accordion> createState() => _AccordionState();
}

class _AccordionState extends State<Accordion> with SingleTickerProviderStateMixin {
  late final _c = AnimationController(vsync: this, duration: const Duration(milliseconds: 420), value: widget.open ? 1 : 0);
  late final _a = CurvedAnimation(parent: _c, curve: easeCard);

  @override
  void didUpdateWidget(Accordion old) {
    super.didUpdateWidget(old);
    if (widget.open != old.open) widget.open ? _c.forward() : _c.reverse();
  }

  @override
  void dispose() {
    _a.dispose();
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _c,
      builder: (context, _) {
        if (_c.isDismissed) return const SizedBox(width: double.infinity);
        return ClipRect(child: Align(alignment: Alignment.topCenter, heightFactor: _a.value, child: widget.builder(context)));
      },
    );
  }
}

class SearchField extends StatelessWidget {
  const SearchField({super.key, required this.hint, this.onChanged, this.controller, this.icon = true, this.minH = 44, this.label, this.keyboardType, this.autofocus = false, this.onSubmitted});

  final String hint;
  final ValueChanged<String>? onChanged;
  final ValueChanged<String>? onSubmitted;
  final TextEditingController? controller;
  final bool icon;
  final double minH;
  final String? label;
  final TextInputType? keyboardType;
  final bool autofocus;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    return Container(
      constraints: BoxConstraints(minHeight: minH),
      padding: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(color: l.paper2, borderRadius: l.br(11)),
      child: Row(children: [
        if (icon) ...[Ic('search', color: l.ink2), const SizedBox(width: 8)],
        Expanded(
          child: BareField(hint: hint, controller: controller, onChanged: onChanged, onSubmitted: onSubmitted, label: label, keyboardType: keyboardType, autofocus: autofocus),
        ),
      ]),
    );
  }
}

/// A borderless text input in the skin's type.
class BareField extends StatefulWidget {
  const BareField({super.key, required this.hint, this.controller, this.onChanged, this.label, this.maxLines = 1, this.keyboardType, this.autofocus = false, this.onSubmitted});

  final String hint;
  final TextEditingController? controller;
  final ValueChanged<String>? onChanged;
  final ValueChanged<String>? onSubmitted;
  final String? label;
  final int? maxLines;
  final TextInputType? keyboardType;
  final bool autofocus;

  @override
  State<BareField> createState() => _BareFieldState();
}

class _BareFieldState extends State<BareField> {
  late final _own = TextEditingController();
  final _focus = FocusNode();
  TextEditingController get _c => widget.controller ?? _own;

  @override
  void initState() {
    super.initState();
    _c.addListener(_changed);
    // Not TextField's autofocus: that gives way when the focus scope already
    // has a focused child, as the settings tab's nested navigator always does.
    if (widget.autofocus) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _focus.requestFocus();
      });
    }
  }

  void _changed() => setState(() {});

  @override
  void dispose() {
    _c.removeListener(_changed);
    _own.dispose();
    _focus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final style = l.text(14.5, lh: 20);
    return Semantics(
      textField: true,
      label: widget.label ?? widget.hint,
      child: GestureDetector(
        behavior: HitTestBehavior.translucent,
        onTap: _focus.requestFocus,
        child: Stack(alignment: widget.maxLines == 1 ? Alignment.centerLeft : Alignment.topLeft, children: [
          if (_c.text.isEmpty) IgnorePointer(child: Line1(widget.hint, style.copyWith(color: l.mute, shadows: const []))),
          // A TextField for the system's paste menu; links are never typed.
          TextField(
            controller: _c,
            focusNode: _focus,
            style: style,
            decoration: const InputDecoration.collapsed(hintText: ''),
            cursorColor: l.accent,
            maxLines: widget.maxLines,
            keyboardType: widget.keyboardType,
            autocorrect: false,
            enableSuggestions: false,
            onChanged: widget.onChanged,
            onSubmitted: widget.onSubmitted,
            keyboardAppearance: l.isDark ? Brightness.dark : Brightness.light,
          ),
        ]),
      ),
    );
  }
}
