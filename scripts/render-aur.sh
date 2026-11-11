#!/usr/bin/env bash
# Writes the PKGBUILD and .SRCINFO of the bowel-bin AUR package, which unpacks
# the release's Debian packages. MyGo's own PKGBUILD covers one architecture
# and builds from local files, which the AUR cannot use. Also writes
# bowel-bin.install, which runs scripts/linux-postrm.sh when the package is
# removed.
#
#   scripts/render-aur.sh 0.2.0 <sha256 of amd64 .deb> <sha256 of arm64 .deb> out
set -euo pipefail
VERSION="$1"
SHA_AMD64="$2"
SHA_ARM64="$3"
OUT="$4"

DESC="A desktop client for mihomo"
URL="https://github.com/21stware/bowel"
LICENSE="PolyForm-Noncommercial-1.0.0"
HERE="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$OUT"

# pacman calls post_remove only when the package goes, not on an upgrade.
{
	echo 'post_remove() {'
	echo "	/bin/sh -c '$(grep -v '^#' "$HERE/linux-postrm.sh" | sed "s/'/'\\\\''/g")' sh remove"
	echo '}'
} >"$OUT/bowel-bin.install"

cat >"$OUT/PKGBUILD" <<EOF
# Maintainer: 21stware <https://github.com/21stware/bowel>
# Updated by the release workflow of $URL
pkgname=bowel-bin
pkgver=$VERSION
pkgrel=1
pkgdesc='$DESC'
arch=('x86_64' 'aarch64')
url='$URL'
license=('$LICENSE')
depends=('gtk3' 'webkit2gtk-4.1')
provides=('bowel')
conflicts=('bowel')
options=('!strip' '!debug')
install=bowel-bin.install
source_x86_64=("bowel_\${pkgver}_amd64.deb::$URL/releases/download/v\${pkgver}/bowel_\${pkgver}_amd64.deb")
source_aarch64=("bowel_\${pkgver}_arm64.deb::$URL/releases/download/v\${pkgver}/bowel_\${pkgver}_arm64.deb")
sha256sums_x86_64=('$SHA_AMD64')
sha256sums_aarch64=('$SHA_ARM64')

package() {
	tar -xf data.tar.* -C "\$pkgdir"
}
EOF

cat >"$OUT/.SRCINFO" <<EOF
pkgbase = bowel-bin
	pkgdesc = $DESC
	pkgver = $VERSION
	pkgrel = 1
	url = $URL
	arch = x86_64
	arch = aarch64
	license = $LICENSE
	depends = gtk3
	depends = webkit2gtk-4.1
	provides = bowel
	conflicts = bowel
	options = !strip
	options = !debug
	install = bowel-bin.install
	source_x86_64 = bowel_${VERSION}_amd64.deb::$URL/releases/download/v$VERSION/bowel_${VERSION}_amd64.deb
	sha256sums_x86_64 = $SHA_AMD64
	source_aarch64 = bowel_${VERSION}_arm64.deb::$URL/releases/download/v$VERSION/bowel_${VERSION}_arm64.deb
	sha256sums_aarch64 = $SHA_ARM64

pkgname = bowel-bin
EOF
