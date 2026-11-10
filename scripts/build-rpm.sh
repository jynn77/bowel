#!/usr/bin/env bash
# Repackages a Debian package from `mygo build` as an rpm, so that both install
# the same files. MyGo writes no rpm itself. Needs dpkg-deb and nfpm.
#
#   scripts/build-rpm.sh build/linux-amd64/bowel_0.2.0_amd64.deb build/linux-amd64
set -euo pipefail
DEB="$1"
OUT="$2"

WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
dpkg-deb -x "$DEB" "$WORK/root"

# nfpm maps amd64 and arm64 to x86_64 and aarch64 itself. Only the files of the
# app are listed under /usr/share: a tree there would make the rpm own
# directories that belong to the system.
cat >"$WORK/nfpm.yaml" <<EOF
name: bowel
version: "$(dpkg-deb -f "$DEB" Version)"
release: 1
arch: $(dpkg-deb -f "$DEB" Architecture)
platform: linux
section: Applications/Internet
vendor: 21stware
homepage: https://github.com/21stware/bowel
license: PolyForm-Noncommercial-1.0.0
description: $(dpkg-deb -f "$DEB" Description | head -1)
depends:
  - gtk3
  - webkit2gtk4.1
contents:
  - src: $WORK/root/opt/bowel
    dst: /opt/bowel
    type: tree
  - src: $WORK/root/usr/share/applications/bowel.desktop
    dst: /usr/share/applications/bowel.desktop
  - src: $WORK/root/usr/share/icons/hicolor/256x256/apps/bowel.png
    dst: /usr/share/icons/hicolor/256x256/apps/bowel.png
  - src: $WORK/root/usr/share/icons/hicolor/512x512/apps/bowel.png
    dst: /usr/share/icons/hicolor/512x512/apps/bowel.png
EOF

mkdir -p "$OUT"
nfpm package --config "$WORK/nfpm.yaml" --packager rpm --target "$OUT"
