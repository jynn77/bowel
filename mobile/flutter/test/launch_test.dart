import 'dart:async';

import 'package:bowel/pages/launch.dart';
import 'package:bowel/ui/skin.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  Widget launch(Future<void> ready, VoidCallback onOpen) => Directionality(
        textDirection: TextDirection.ltr,
        child: Skin(
          look: Look.of('paper', false),
          child: Launch(ready: ready, onOpen: onOpen, child: const Text('app')),
        ),
      );

  testWidgets('a slow core holds the drawn line', (tester) async {
    final ready = Completer<void>();
    var opened = 0;
    await tester.pumpWidget(launch(ready.future, () => opened++));
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump(const Duration(seconds: 2));
    expect(find.text('app'), findsNothing);
    expect(opened, 0);

    ready.complete();
    await tester.pump();
    await tester.pump();
    expect(find.text('app'), findsOneWidget);
    expect(find.byType(FadeTransition), findsOneWidget);
    expect(opened, 1);

    await tester.pumpAndSettle();
    expect(find.byType(FadeTransition), findsNothing);
    expect(find.text('app'), findsOneWidget);
  });

  testWidgets('a fast core waits for the line to finish', (tester) async {
    await tester.pumpWidget(launch(Future<void>.value(), () {}));
    await tester.pump(const Duration(milliseconds: 500));
    expect(find.text('app'), findsNothing);
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump();
    expect(find.text('app'), findsOneWidget);
  });
}
