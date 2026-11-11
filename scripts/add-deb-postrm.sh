#!/usr/bin/env bash
# Adds scripts/linux-postrm.sh to a Debian package from `mygo build` as its
# postrm, in place: MyGo writes no maintainer scripts. Needs dpkg-deb.
#
#   scripts/add-deb-postrm.sh build/linux-amd64/bowel_0.3.0_amd64.deb
set -euo pipefail
DEB="$1"
HERE="$(cd "$(dirname "$0")" && pwd)"

WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
dpkg-deb -R "$DEB" "$WORK/root"
install -m 0755 "$HERE/linux-postrm.sh" "$WORK/root/DEBIAN/postrm"
# gzip, as MyGo wrote it: every dpkg reads it. The files were unpacked as
# this user, and are root's in the package.
dpkg-deb -Zgzip --root-owner-group -b "$WORK/root" "$DEB" >/dev/null
