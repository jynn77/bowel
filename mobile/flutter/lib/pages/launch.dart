import 'dart:ui' show PathMetric;

import 'package:flutter/widgets.dart';

import '../ui/logo_path.dart';
import '../ui/skin.dart';

/// The logo's line drawing itself on the paper, timed as at the end of the
/// demo film. [child] is built once the line is drawn and [ready] has
/// completed, and fades in from under it.
class Launch extends StatefulWidget {
  const Launch({super.key, required this.ready, required this.onOpen, required this.child});

  final Future<void> ready;

  /// Called as [child] starts to show.
  final VoidCallback onOpen;
  final Widget child;

  @override
  State<Launch> createState() => _LaunchState();
}

class _LaunchState extends State<Launch> with TickerProviderStateMixin {
  late final _draw = AnimationController(vsync: this, duration: const Duration(milliseconds: 560));
  late final _fade = AnimationController(vsync: this, duration: const Duration(milliseconds: 240));
  late final _drawn = CurvedAnimation(parent: _draw, curve: Curves.easeInOutSine);
  late final _cover = ReverseAnimation(_fade);
  bool _open = false, _gone = false;

  @override
  void initState() {
    super.initState();
    final still = WidgetsBinding.instance.platformDispatcher.accessibilityFeatures.disableAnimations;
    if (still) _draw.value = 1;
    Future.wait<void>([if (!still) _draw.forward(), widget.ready]).then((_) async {
      if (!mounted) return;
      setState(() => _open = true);
      widget.onOpen();
      // After the child's first build, the expensive frame, or the fade skips it.
      await WidgetsBinding.instance.endOfFrame;
      if (!mounted) return;
      await _fade.forward();
      if (mounted) setState(() => _gone = true);
    });
  }

  @override
  void dispose() {
    _drawn.dispose();
    _draw.dispose();
    _fade.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    return Stack(fit: StackFit.expand, children: [
      if (_open) widget.child else const SizedBox.shrink(),
      if (!_gone)
        FadeTransition(
          opacity: _cover,
          child: ColoredBox(
            color: l.paper,
            child: Center(child: CustomPaint(size: const Size.square(96), painter: _LinePainter(_drawn, l.ink))),
          ),
        ),
    ]);
  }
}

/// [logoPathData] is one `M` and then absolute `C`s, as its generator writes it.
final PathMetric _line = () {
  final n = RegExp(r'[\d.]+').allMatches(logoPathData).map((m) => double.parse(m[0]!)).toList();
  final path = Path()..moveTo(n[0], n[1]);
  for (var i = 2; i + 5 < n.length; i += 6) {
    path.cubicTo(n[i], n[i + 1], n[i + 2], n[i + 3], n[i + 4], n[i + 5]);
  }
  return path.computeMetrics().first;
}();

class _LinePainter extends CustomPainter {
  _LinePainter(this.drawn, this.color) : super(repaint: drawn);

  final Animation<double> drawn;
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    if (drawn.value == 0) return;
    canvas.scale(size.width / 200);
    canvas.drawPath(
      _line.extractPath(0, _line.length * drawn.value),
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeCap = StrokeCap.round
        ..strokeJoin = StrokeJoin.round
        ..strokeWidth = logoPathWidth
        ..color = color,
    );
  }

  @override
  bool shouldRepaint(_LinePainter old) => old.color != color;
}
