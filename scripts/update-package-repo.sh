#!/usr/bin/env bash
# Adds the .deb and .rpm files of INCOMING to the APT and DNF repositories in
# SITE, the tree that GitHub Pages serves from 21stware/packages, and signs
# them. The newest KEEP_VERSIONS versions stay: a GitHub Pages site should stay
# under 1 GB and each version is about 190 MB. Needs PACKAGES_GPG_PRIVATE_KEY,
# gpg, apt-ftparchive and createrepo_c.
#
#   scripts/update-package-repo.sh incoming site
set -euo pipefail
SCRIPTS="$(cd "$(dirname "$0")" && pwd)"
INCOMING="$(cd "$1" && pwd)"
mkdir -p "$2"
SITE="$(cd "$2" && pwd)"
KEEP_VERSIONS="${KEEP_VERSIONS:-2}"
. "$SCRIPTS/package-gpg.sh"
SIGN=(gpg --batch --yes --local-user "$SIGNING_KEY" --digest-algo SHA512)

# prune <dir> <glob>: removes all but the newest KEEP_VERSIONS files.
prune() {
	find "$1" -maxdepth 1 -type f -name "$2" | sort -V | head -n "-$KEEP_VERSIONS" | xargs -r rm -f
}

POOL="$SITE/apt/pool/main/b/bowel"
mkdir -p "$POOL"
cp "$INCOMING"/*.deb "$POOL/"
for arch in amd64 arm64; do
	prune "$POOL" "bowel_*_$arch.deb"
done

for rpm in "$INCOMING"/*.rpm; do
	arch="${rpm%.rpm}"
	arch="${arch##*.}"
	mkdir -p "$SITE/rpm/$arch"
	cp "$rpm" "$SITE/rpm/$arch/"
done
for arch in x86_64 aarch64; do
	prune "$SITE/rpm/$arch" "bowel-*.$arch.rpm"
	createrepo_c --quiet --update "$SITE/rpm/$arch"
	"${SIGN[@]}" --armor --detach-sign --output "$SITE/rpm/$arch/repodata/repomd.xml.asc" "$SITE/rpm/$arch/repodata/repomd.xml"
done

cd "$SITE/apt"
DIST=dists/stable
for arch in amd64 arm64; do
	mkdir -p "$DIST/main/binary-$arch"
	apt-ftparchive --arch "$arch" packages pool >"$DIST/main/binary-$arch/Packages"
	gzip -9nkf "$DIST/main/binary-$arch/Packages"
done
rm -f "$DIST/Release" "$DIST/InRelease" "$DIST/Release.gpg"
RELEASE="$(mktemp)"
apt-ftparchive \
	-o APT::FTPArchive::Release::Origin=bowel \
	-o APT::FTPArchive::Release::Label=bowel \
	-o APT::FTPArchive::Release::Suite=stable \
	-o APT::FTPArchive::Release::Codename=stable \
	-o APT::FTPArchive::Release::Architectures="amd64 arm64" \
	-o APT::FTPArchive::Release::Components=main \
	release "$DIST" >"$RELEASE"
mv "$RELEASE" "$DIST/Release"
chmod 644 "$DIST/Release"
"${SIGN[@]}" --clearsign --output "$DIST/InRelease" "$DIST/Release"
"${SIGN[@]}" --armor --detach-sign --output "$DIST/Release.gpg" "$DIST/Release"

cd "$SITE"
gpg --batch --yes --armor --export "$SIGNING_KEY" >bowel.asc
gpg --batch --yes --export "$SIGNING_KEY" >bowel.gpg
cp "$SCRIPTS/install-linux.sh" install.sh
# Jekyll has no business with this tree.
touch .nojekyll
