import 'dart:io';

import 'package:bowel/app/i18n.dart';
import 'package:bowel/app/state.dart';
import 'package:bowel/pages/frame.dart';
import 'package:bowel/pages/setup.dart';
import 'package:bowel/ui/skin.dart';
import 'package:flutter/material.dart' show DefaultMaterialLocalizations, Material, MaterialType;
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

Widget _host(AppState app, Size size) => Localizations(
      locale: const Locale('en'),
      delegates: const [DefaultMaterialLocalizations.delegate, DefaultWidgetsLocalizations.delegate],
      child: MediaQuery(
        data: MediaQueryData(size: size),
        child: Skin(
          look: Look.of('paper', false),
          child: Frame(
            data: const FrameData(top: 47, barInset: 0, nativeBar: false, homeIndicator: 0, form: WindowForm.phone),
            child: Navigator(onGenerateRoute: (_) => PageRouteBuilder<void>(pageBuilder: (_, _, _) => Material(type: MaterialType.transparency, child: SetupPage(app: app)))),
          ),
        ),
      ),
    );

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // Widths decide this page, so it needs the app's real font: the test font
  // draws every letter as wide as a kanji.
  setUpAll(() async {
    final bytes = await File('assets/fonts/OPPOSans4.0.ttf').readAsBytes();
    await (FontLoader('OPPO Sans')..addFont(Future.value(ByteData.sublistView(bytes)))).load();
  });

  tearDown(() => language = 'zh');

  for (final lang in ['zh', 'en', 'ja', 'zh-Hant']) {
    for (final size in const [Size(360, 640), Size(375, 667)]) {
      for (final step in [1, 2]) {
        testWidgets('$lang: step $step fits ${size.width.toInt()}x${size.height.toInt()}', (tester) async {
          language = lang;
          tester.view.physicalSize = size;
          tester.view.devicePixelRatio = 1;
          addTearDown(tester.view.reset);
          await tester.pumpWidget(_host(AppState()..setup = step, size));
          expect(tester.takeException(), isNull);
        });
      }
    }
  }
}
