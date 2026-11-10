import 'dart:convert';

import 'package:bowel/app/i18n.dart';
import 'package:bowel/app/state.dart';
import 'package:bowel/board/cards.dart';
import 'package:bowel/board/fly.dart';
import 'package:bowel/pages/frame.dart';
import 'package:bowel/pages/routing.dart';
import 'package:bowel/pages/sheet.dart';
import 'package:bowel/ui/atoms.dart';
import 'package:bowel/ui/skin.dart';
import 'package:flutter/material.dart' show DefaultMaterialLocalizations, Material, MaterialType;
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

Map _service(String id, String name, {bool builtin = false, bool mine = false, bool off = false, List<String> rules = const []}) => {
      'id': id,
      'name': name,
      'builtin': builtin,
      'mine': mine,
      'off': off,
      'changed': false,
      'rules': [
        for (final r in rules) {'text': 'DOMAIN-SUFFIX,$r', 'kind': 'suffix', 'kindName': '域名后缀', 'value': r, 'shown': r, 'builtin': builtin, 'off': false},
      ],
    };

Map _group(String id, String name, List<Map> services, {bool direct = false, bool reject = false, String mode = 'auto'}) => {
      'id': id,
      'name': name,
      'direct': direct,
      'reject': reject,
      'custom': false,
      'mode': mode,
      'regions': <String>[],
      'providers': <String>[],
      'order': <String>[],
      'fixed': '',
      'now': '',
      'services': services,
      'targets': direct ? <Map>[] : [{'name': 'gstatic', 'url': 'https://www.gstatic.com/generate_204'}],
      'sites': <Map>[],
    };

Map _routing() => {
      'groups': [
        _group('default', '默认', [_service('mine:default', '我的规则', mine: true)]),
        _group('ai', 'AI 服务', [
          _service('mine:ai', '我的规则', mine: true),
          _service('openai', 'OpenAI', builtin: true, rules: ['openai.com', 'chatgpt.com', 'oaistatic.com']),
          _service('work', '工作', rules: ['work.example.com']),
        ]),
        _group('solo', '单项', [_service('mine:solo', '我的规则', mine: true), _service('only', '唯一', rules: ['only.example.com'])]),
        _group('direct', '直连', [_service('mine:direct', '我的规则', mine: true)], direct: true),
        _group('reject', '拦截', [_service('mine:reject', '我的规则', mine: true), _service('ads', '广告', builtin: true, off: true, rules: ['ads.example.com'])], direct: true, reject: true),
      ],
      'nodes': [
        {'name': 'a|东京 01', 'display': '东京 01', 'provider': 'P1', 'providerId': 'p1', 'country': 'JP', 'flag': 'jp', 'delay': 80},
        {'name': 'b|香港 02', 'display': '香港 02', 'provider': 'P2', 'providerId': 'p2', 'country': 'HK', 'flag': 'hk', 'delay': -1},
      ],
      'regions': [
        {'code': 'JP', 'name': '东京', 'count': 1},
        {'code': 'HK', 'name': '香港', 'count': 1},
      ],
      'providers': [
        {'id': 'p1', 'name': 'P1'},
        {'id': 'p2', 'name': 'P2'},
      ],
      'kinds': [
        {'id': 'suffix', 'name': '域名后缀'},
        {'id': 'domain', 'name': '域名'},
        {'id': 'keyword', 'name': '域名关键词'},
        {'id': 'ip', 'name': 'IP 段'},
      ],
    };

/// Answers the way the bridge would, and writes down the route operations.
final _ops = <Map>[];
String? _refuseWith;

void _fakeCore() {
  TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(const MethodChannel('bowel/core'), (call) async {
    final method = call.arguments['method'] as String;
    final args = jsonDecode(call.arguments['payload'] as String) as Map;
    switch (method) {
      case 'routing':
        return jsonEncode(_routing());
      case 'parseRule':
        return jsonEncode({'kindName': '域名后缀', 'value': args['text']});
      case 'routeOp':
        if (args['op'] == 'addRule' && _refuseWith != null) throw PlatformException(code: 'x', message: _refuseWith);
        _ops.add(args);
        return 'null';
    }
    return 'null';
  });
}

Widget _host(AppState app, Widget page, {double width = 360, double height = 760}) => Localizations(
      locale: const Locale('en'),
      delegates: const [DefaultMaterialLocalizations.delegate, DefaultWidgetsLocalizations.delegate],
      child: MediaQuery(
        data: MediaQueryData(size: Size(width, height)),
        child: Skin(
          look: Look.of('paper', false),
          child: Frame(
            data: const FrameData(top: 0, barInset: 0, nativeBar: false, homeIndicator: 0, form: WindowForm.phone),
            child: Align(
              alignment: Alignment.topLeft,
              child: SizedBox(
                width: width,
                height: height,
                child: Navigator(
                  onGenerateRoute: (_) => PageRouteBuilder<void>(
                    pageBuilder: (_, _, _) => Material(
                      type: MaterialType.transparency,
                      child: AnimatedBuilder(
                        animation: app,
                        builder: (context, _) => Stack(fit: StackFit.expand, children: [page, SheetHost(app: app)]),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );

Widget _card(AppState app) => FlyHost(
      child: CellMark(
        child: CellScope(cell: 'routes', shown: true, child: AnimatedBuilder(animation: app, builder: (context, _) => cardView(context, app, 'routes', 'expanded', 'wide'))),
      ),
    );

AppState _app() => AppState()..routing = _routing();

Future<void> _settle(WidgetTester tester) async {
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 500));
  await tester.pump(const Duration(milliseconds: 500));
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    _ops.clear();
    _refuseWith = null;
    language = 'zh';
    _fakeCore();
  });

  tearDown(() => language = 'zh');

  for (final lang in ['zh', 'en', 'ja', 'zh-Hant']) {
    testWidgets('$lang: the pages fit a narrow phone', (tester) async {
      language = lang;
      tester.view.physicalSize = const Size(360, 760);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      final app = _app();
      await tester.pumpWidget(_host(app, RoutesPage(app: app)));
      await _settle(tester);
      expect(tester.takeException(), isNull);
      for (final id in ['default', 'ai', 'direct', 'reject']) {
        await tester.pumpWidget(const SizedBox());
        await tester.pumpWidget(_host(app, GroupPage(app: app, id: id)));
        await _settle(tester);
        expect(tester.takeException(), isNull, reason: id);
      }
    });

    testWidgets('$lang: the route card footer holds the strategy on a narrow phone', (tester) async {
      language = lang;
      tester.view.physicalSize = const Size(360, 760);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      final app = AppState()
        ..x = 'routes'
        ..sub = 'ai';
      await tester.pumpWidget(_host(app, _card(app)));
      await tester.pump(const Duration(seconds: 1));
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets('the route card changes its strategy from a sheet', (tester) async {
    final app = AppState()
      ..x = 'routes'
      ..sub = 'ai';
    await tester.pumpWidget(_host(app, _card(app)));
    await tester.pump(const Duration(seconds: 1));
    await tester.tap(find.text('自动'));
    await _settle(tester);
    await tester.tap(find.text('顺序备用'));
    await _settle(tester);
    expect(app.sheet, isNull);
    expect(app.route('ai').policy, 'order');
    expect(find.text('备用'), findsOneWidget);
  });

  testWidgets('a built-in service takes rules, and a kind is left to be worked out', (tester) async {
    final app = _app();
    await tester.pumpWidget(_host(app, GroupPage(app: app, id: 'ai', service: 'openai')));
    await _settle(tester);
    await tester.tap(find.text('添加规则').first);
    await _settle(tester);
    await tester.enterText(find.byType(EditableText), 'new.example.com');
    await _settle(tester);
    expect(find.text('添加后：new.example.com · 域名后缀'), findsOneWidget);
    await tester.tap(find.text('添加').last);
    await _settle(tester);
    expect(_ops.single['op'], 'addRule');
    expect(_ops.single['kind'], '');
    expect(_ops.single['service'], 'openai');
    expect(app.sheet, isNull);
  });

  testWidgets('a conflict stays in the sheet and offers to move the rule', (tester) async {
    _refuseWith = 'conflict:OpenAI';
    final app = _app();
    await tester.pumpWidget(_host(app, GroupPage(app: app, id: 'ai', service: 'work')));
    await _settle(tester);
    await tester.tap(find.text('添加规则').last);
    await _settle(tester);
    await tester.enterText(find.byType(EditableText), 'openai.com');
    await _settle(tester);
    await tester.tap(find.text('添加').last);
    await _settle(tester);
    expect(app.sheet, isNotNull);
    expect(find.text('openai.com 已经在 OpenAI 里，一条规则只能属于一个服务。'), findsOneWidget);
    await tester.tap(find.text('移过来'));
    await _settle(tester);
    expect(_ops.single['op'], 'moveRule');
    expect(app.sheet, isNull);
  });

  testWidgets('only the groups that can change have a rename and delete menu', (tester) async {
    final app = _app();
    await tester.pumpWidget(_host(app, GroupPage(app: app, id: 'default')));
    await _settle(tester);
    expect(find.bySemanticsLabel('更多'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    await tester.pumpWidget(_host(app, GroupPage(app: app, id: 'ai')));
    await _settle(tester);
    expect(find.bySemanticsLabel('更多'), findsOneWidget);
  });

  testWidgets('a group with one service does not offer to split it', (tester) async {
    final app = _app();
    await tester.pumpWidget(_host(app, GroupPage(app: app, id: 'solo', service: 'only')));
    await _settle(tester);
    await tester.tap(find.text('更多'));
    await _settle(tester);
    expect(find.text('单独成组'), findsNothing);
    expect(find.text('移到其他路由组'), findsOneWidget);
  });

  testWidgets('adding a service offers those of the other groups', (tester) async {
    final app = _app();
    await tester.pumpWidget(_host(app, GroupPage(app: app, id: 'solo')));
    await _settle(tester);
    await tester.tap(find.text('添加服务'));
    await _settle(tester);
    expect(find.text('新建服务'), findsOneWidget);
    await tester.tap(find.text('OpenAI'));
    await _settle(tester);
    expect(_ops.single, containsPair('op', 'moveService'));
    expect(_ops.single, containsPair('id', 'openai'));
    expect(_ops.single, containsPair('to', 'solo'));
  });

  testWidgets('a field opened beside a nested navigator takes the focus', (tester) async {
    final open = ValueNotifier(false);
    await tester.pumpWidget(Localizations(
      locale: const Locale('en'),
      delegates: const [DefaultMaterialLocalizations.delegate, DefaultWidgetsLocalizations.delegate],
      child: MediaQuery(
        data: const MediaQueryData(size: Size(360, 760)),
        child: Skin(
          look: Look.of('paper', false),
          child: Material(
            type: MaterialType.transparency,
            child: Overlay(initialEntries: [
              OverlayEntry(
                builder: (_) => ValueListenableBuilder(
                  valueListenable: open,
                  builder: (context, shown, _) => Stack(children: [
                    Navigator(onGenerateRoute: (_) => PageRouteBuilder<void>(pageBuilder: (_, _, _) => const SizedBox.expand())),
                    if (shown) const BareField(hint: 'name', autofocus: true),
                  ]),
                ),
              ),
            ]),
          ),
        ),
      ),
    ));
    await _settle(tester);
    open.value = true;
    await _settle(tester);
    expect(FocusManager.instance.primaryFocus?.context?.findAncestorWidgetOfExactType<BareField>(), isNotNull);
  });
}
