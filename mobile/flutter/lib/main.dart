import 'dart:io' show Platform;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show SystemNavigator;

import 'app/sample.dart';
import 'app/shell.dart';
import 'app/state.dart';
import 'board/board.dart';
import 'pages/activity.dart';
import 'pages/deco.dart';
import 'pages/frame.dart';
import 'pages/launch.dart';
import 'pages/providers.dart';
import 'pages/settings.dart';
import 'pages/sheet.dart';
import 'pages/setup.dart';
import 'ui/skin.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final app = AppState();
  Shell.instance.onTabSelected = (i) => app.selectTab(i, fromShell: true);
  await Shell.instance.start();
  await app.load();
  runApp(BowelApp(app: app, ready: app.attachCore()));
}

class BowelApp extends StatelessWidget {
  const BowelApp({super.key, required this.app, required this.ready});

  final AppState app;

  /// The core has started, while the launch animation plays.
  final Future<void> ready;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      debugShowCheckedModeBanner: false,
      title: 'bowel',
      theme: ThemeData(fontFamily: 'OPPO Sans', splashFactory: NoSplash.splashFactory, highlightColor: Colors.transparent),
      home: AnimatedBuilder(animation: app, builder: (context, _) => _Root(app: app, ready: ready)),
    );
  }
}

class _Root extends StatefulWidget {
  const _Root({required this.app, required this.ready});
  final AppState app;
  final Future<void> ready;

  @override
  State<_Root> createState() => _RootState();
}

class _RootState extends State<_Root> {
  String? _styled;
  bool _open = false;

  void _syncShell(Look l) {
    final barHidden = widget.app.setup != 0 || !_open;
    final warn = providers.any((p) => p.warn != null);
    final key = '${l.id}|${l.isDark}|${l.accent.toARGB32()}|$barHidden|$warn';
    if (key == _styled) return;
    _styled = key;
    final shell = Shell.instance;
    shell.setStyle(l.accent, dark: l.darkGlass, paper: l.paper);
    shell.setBadge(2, on: warn, color: l.warn);
    shell.setBarHidden(barHidden);
  }

  @override
  Widget build(BuildContext context) {
    final app = widget.app;
    final l = Look.of(app.skin, app.wantsDark(MediaQuery.platformBrightnessOf(context)), custom: app.accent);
    _syncShell(l);
    return Skin(
      look: l,
      child: Material(
        type: MaterialType.transparency,
        child: MediaQuery.withClampedTextScaling(
          maxScaleFactor: 1,
          child: ColoredBox(
            color: l.paper,
            child: Launch(
              ready: widget.ready,
              onOpen: () => setState(() => _open = true),
              child: ValueListenableBuilder<EdgeInsets?>(
                valueListenable: Shell.instance.insets,
                builder: (context, native, _) {
                  final media = MediaQuery.paddingOf(context);
                  final frame = FrameData(
                    top: native?.top ?? media.top,
                    barInset: native?.bottom ?? 0,
                    nativeBar: Shell.instance.native,
                    homeIndicator: media.bottom,
                    form: windowFormOf(MediaQuery.sizeOf(context)),
                  );
                  return Frame(
                    data: frame,
                    child: Stack(fit: StackFit.expand, children: [
                      app.setup != 0 ? SetupPage(app: app) : _Tabs(app: app),
                      const IgnorePointer(child: Deco()),
                    ]),
                  );
                },
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _Tabs extends StatelessWidget {
  const _Tabs({required this.app});
  final AppState app;

  @override
  Widget build(BuildContext context) {
    final frame = Frame.of(context);
    final kb = MediaQuery.viewInsetsOf(context).bottom;
    Widget page(int i, Widget child) => TickerMode(enabled: app.tab == i, child: child);
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (didPop) return;
        final nav = app.settingsNav.currentState;
        if (app.sheet != null) {
          app.closeSheet();
        } else if (app.tab == 3 && nav != null && nav.canPop()) {
          nav.pop();
        } else if (app.tab == 0 && app.x != null) {
          app.collapse();
        } else if (app.tab != 0) {
          app.selectTab(0);
        } else {
          SystemNavigator.pop();
        }
      },
      child: Column(children: [
      Expanded(
        // Full height, so a sheet's scrim reaches over the status bar.
        child: Stack(fit: StackFit.expand, children: [
          Padding(
            // With the bar up top, nothing else keeps the pages off the gesture area.
            padding: EdgeInsets.only(top: frame.top, bottom: frame.drawnTopBar && kb == 0 ? frame.homeIndicator : 0),
            child: Column(children: [
              if (frame.drawnTopBar) TopTabs(app: app),
              Expanded(
                child: IndexedStack(
                  index: app.tab,
                  children: [
                    page(
                      0,
                      Padding(
                        // a keyboard squeezes the opened card rather than covering its footer
                        padding: EdgeInsets.only(bottom: frame.nativeBar ? (kb > frame.barInset ? kb : frame.barReserve) : kb),
                        child: Board(app: app),
                      ),
                    ),
                    page(1, ActivityPage(app: app)),
                    page(2, ProvidersPage(app: app)),
                    page(3, SettingsPage(app: app)),
                  ],
                ),
              ),
            ]),
          ),
          SheetHost(app: app),
          if (app.toast != null) Toast(app.toast!),
        ]),
      ),
      if (!frame.nativeBar && !frame.drawnTopBar && !Platform.isIOS) AndroidBar(app: app),
    ]),
    );
  }
}
