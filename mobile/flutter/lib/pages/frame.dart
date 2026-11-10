import 'package:flutter/material.dart' show Icons;
import 'package:flutter/widgets.dart';

import '../app/i18n.dart';
import '../app/state.dart';
import '../ui/atoms.dart';
import '../ui/skin.dart';

/// How much room the window has, which decides the board and the navigation.
/// A window's size can change at any moment (rotation, split screen), so this
/// is never a question of which device the app is on.
enum WindowForm { phone, medium, wide }

/// Under 600 tall, with the bars taken off, the grids can't hold their cards.
WindowForm windowFormOf(Size s) => s.width < 600 || s.height < 600 ? WindowForm.phone : (s.width < 900 ? WindowForm.medium : WindowForm.wide);

class FrameData {
  const FrameData({required this.top, required this.barInset, required this.nativeBar, required this.homeIndicator, required this.form});

  final double top;

  /// The height the floating system bar covers. 0 when Flutter draws the bar.
  final double barInset, homeIndicator;
  final bool nativeBar;
  final WindowForm form;

  /// Flutter draws the tabs along the top, as the desktop window has them.
  bool get drawnTopBar => !nativeBar && form != WindowForm.phone;

  /// What the board keeps clear above the bar: its height and a small gap.
  double get barReserve => barInset + 9;
}

class Frame extends InheritedWidget {
  const Frame({super.key, required this.data, required super.child});

  final FrameData data;

  static FrameData of(BuildContext context) => context.dependOnInheritedWidgetOfExactType<Frame>()!.data;

  @override
  bool updateShouldNotify(Frame oldWidget) {
    final a = data, b = oldWidget.data;
    return a.top != b.top || a.barInset != b.barInset || a.nativeBar != b.nativeBar || a.homeIndicator != b.homeIndicator || a.form != b.form;
  }
}

Widget pageTitle(Look l, String text) => Padding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
      child: Text(text, style: l.text(30, weight: 700, display: true, lh: 36)),
    );

/// A list page: the list runs under the floating bar, and search and add
/// float just above it, with a fade so rows don't collide with them.
class ListPage extends StatelessWidget {
  const ListPage({super.key, required this.title, required this.list, this.dock, this.dockH = 0, this.marginTop = 8});

  final String title;

  /// Receives the padding the list should leave at its bottom.
  final Widget Function(double bottom) list;
  final Widget? dock;
  final double dockH, marginTop;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final f = Frame.of(context);
    final kb = MediaQuery.viewInsetsOf(context).bottom;
    final clearance = f.barReserve;
    if (!f.nativeBar) {
      return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        pageTitle(l, title),
        SizedBox(height: marginTop),
        Expanded(child: list(10)),
        if (dock != null)
          Container(
            padding: EdgeInsets.fromLTRB(16, 8, 16, kb > 0 ? kb + 8 : 8),
            decoration: BoxDecoration(border: Border(top: BorderSide(color: l.hair))),
            child: dock,
          ),
      ]);
    }
    final dockBottom = kb > f.barInset ? kb + 6 : clearance;
    final fade = clearance + dockH + 20;
    return Stack(fit: StackFit.expand, children: [
      Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        pageTitle(l, title),
        SizedBox(height: marginTop),
        Expanded(child: list(dock == null ? clearance + 10 : clearance + dockH + 10)),
      ]),
      if (dock != null) ...[
        Positioned(
          left: 0,
          right: 0,
          bottom: 0,
          height: kb > f.barInset ? dockBottom + dockH + 20 : fade,
          child: IgnorePointer(
            child: DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topCenter,
                  end: Alignment.bottomCenter,
                  colors: [l.paper.withValues(alpha: 0), l.paper, l.paper.withValues(alpha: .72)],
                  stops: const [0, .42, 1],
                ),
              ),
            ),
          ),
        ),
        Positioned(left: 16, right: 16, bottom: dockBottom, child: Padding(padding: const EdgeInsets.only(bottom: 6), child: dock)),
      ],
    ]);
  }
}

class Toast extends StatelessWidget {
  const Toast(this.text, {super.key});
  final String text;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    return Positioned(
      left: 0,
      right: 0,
      bottom: Frame.of(context).barReserve + 12,
      child: IgnorePointer(
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 24),
          child: Center(
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
              decoration: BoxDecoration(color: l.ink, borderRadius: l.br(10)),
              child: Text(text, maxLines: 3, overflow: TextOverflow.ellipsis, textAlign: TextAlign.center, style: l.text(13.5, color: l.paper, lh: 19).copyWith(shadows: const [])),
            ),
          ),
        ),
      ),
    );
  }
}

/// Android has no system tab bar to host, so Flutter draws Material 3's. It
/// follows the skin. iOS uses the real one, from the native shell.
class AndroidBar extends StatelessWidget {
  const AndroidBar({super.key, required this.app});
  final AppState app;

  static List<(IconData, String)> get items => [
        (Icons.speed, tr('代理', 'Proxy')),
        (Icons.graphic_eq, tr('活动', 'Activity')),
        (Icons.layers, 'Provider'),
        (Icons.settings, tr('设置', 'Settings')),
      ];

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    final inset = MediaQuery.paddingOf(context).bottom;
    return Container(
      padding: EdgeInsets.fromLTRB(8, 10, 8, inset > 0 ? inset : 12),
      decoration: BoxDecoration(color: l.card, border: Border(top: BorderSide(color: l.hair2))),
      child: Row(children: [
        for (final (i, it) in items.indexed)
          Expanded(
            child: GestureDetector(
              behavior: HitTestBehavior.opaque,
              onTap: () => app.selectTab(i),
              child: Column(mainAxisSize: MainAxisSize.min, children: [
                Stack(clipBehavior: Clip.none, alignment: Alignment.center, children: [
                  AnimatedContainer(
                    duration: const Duration(milliseconds: 240),
                    curve: const Cubic(.2, 0, 0, 1),
                    width: app.tab == i ? 64 : 26,
                    height: 32,
                    decoration: BoxDecoration(color: app.tab == i ? l.accent.withValues(alpha: .34) : const Color(0x00000000), borderRadius: l.br(16)),
                  ),
                  Icon(it.$1, size: 24, color: app.tab == i ? l.ink : l.ink2),
                  if (i == 2) Positioned(top: 1, right: 16, child: Dot(l.warn)),
                ]),
                const SizedBox(height: 3),
                Text(it.$2, maxLines: 1, style: l.text(12, weight: app.tab == i ? 700 : 500, color: app.tab == i ? l.ink : l.ink2, lh: 16).copyWith(shadows: const [])),
              ]),
            ),
          ),
      ]),
    );
  }
}

/// The tabs on a big Android window: the desktop's segmented row, along the top.
class TopTabs extends StatelessWidget {
  const TopTabs({super.key, required this.app});
  final AppState app;

  @override
  Widget build(BuildContext context) {
    final l = Skin.of(context);
    return SizedBox(
      height: 50,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 14),
        child: Row(children: [
          Container(
            padding: const EdgeInsets.all(3),
            decoration: BoxDecoration(color: l.hair2, borderRadius: l.br(10)),
            child: Row(mainAxisSize: MainAxisSize.min, children: [
              for (final (i, it) in AndroidBar.items.indexed)
                GestureDetector(
                  behavior: HitTestBehavior.opaque,
                  onTap: () => app.selectTab(i),
                  child: AnimatedContainer(
                    duration: const Duration(milliseconds: 200),
                    padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
                    decoration: BoxDecoration(color: app.tab == i ? l.accent : const Color(0x00000000), borderRadius: l.br(7)),
                    child: Row(mainAxisSize: MainAxisSize.min, children: [
                      Text(it.$2, maxLines: 1, style: l.text(13, weight: 600, color: app.tab == i ? l.onAccent : l.ink2, lh: 18).copyWith(shadows: const [])),
                      if (i == 2) ...[const SizedBox(width: 6), Dot(l.warn)],
                    ]),
                  ),
                ),
            ]),
          ),
        ]),
      ),
    );
  }
}

/// A row's hairline, along its bottom edge.
Widget hairBottom(Look l, Widget child) => DecoratedBox(decoration: BoxDecoration(border: Border(bottom: BorderSide(color: l.hair2))), child: child);

/// A row's hairline, along its top edge.
Widget hairTop(Look l, Widget child) => DecoratedBox(decoration: BoxDecoration(border: Border(top: BorderSide(color: l.hair2))), child: child);

Widget eyebrow(Look l, String text) => Text(text, style: l.text(12, weight: 500, color: l.mute, lh: 18, ls: .08));
