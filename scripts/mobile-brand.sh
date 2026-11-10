#!/usr/bin/env bash
# Writes the mobile app's brand assets from the desktop's: the Android vectors
# and the Flutter logo path from logo.go's line (TestMobileLogo). The iOS app
# icon needs no output: the Xcode project compiles assets/icon/AppIcon.icon
# directly.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="$ROOT/mobile/flutter"
cd "$ROOT"
MOHO_MOBILE_LOGO="$APP" CGO_ENABLED=0 go test -count=1 -run '^TestMobileLogo$' . >/dev/null
echo "wrote the mobile brand assets to $APP"
