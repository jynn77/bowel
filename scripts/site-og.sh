#!/usr/bin/env bash
# Renders the cards link previews show, site/og-zh.png and site/og-en.png,
# from scripts/og-card.html. Needs Google Chrome. Run it after
# scripts/site-shots.sh when the main window's screenshot changes.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CHROME="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
for lang in zh en; do
  "$CHROME" --headless --hide-scrollbars --force-device-scale-factor=1 --window-size=1200,630 \
    --screenshot="$ROOT/site/og-$lang.png" "file://$ROOT/scripts/og-card.html?$lang" >/dev/null 2>&1
done
ls -l "$ROOT"/site/og-*.png
