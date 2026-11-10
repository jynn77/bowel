#!/usr/bin/env bash
# Turns the artifacts of a release run into the flat directory that becomes the
# GitHub release: one copy of each file, the rpms signed, and SHA256SUMS over
# everything, which install.ps1 reads to find and check the Windows installer.
# Each build writes its own SHA256SUMS, so a merged one is made here. Needs
# PACKAGES_GPG_PRIVATE_KEY, gpg and rpmsign.
#
#   scripts/prepare-release.sh release dist
set -euo pipefail
FROM="$1"
OUT="$2"
. "$(dirname "$0")/package-gpg.sh"

mkdir -p "$OUT"
while IFS= read -r -d '' file; do
	name="$(basename "$file")"
	if [ ! -e "$OUT/$name" ]; then
		cp "$file" "$OUT/$name"
	elif ! cmp -s "$file" "$OUT/$name"; then
		# Both Windows builds write install.ps1, the same file.
		echo "two different files are named $name" >&2
		exit 1
	fi
done < <(find "$FROM" -type f -print0)

# Ubuntu's rpm looks for gpg2, which it no longer ships.
rpmsign --define "__gpg $(command -v gpg)" --define "_gpg_name $SIGNING_KEY" --addsign "$OUT"/*.rpm

cd "$OUT"
rm -f SHA256SUMS
sums="$(sha256sum -- *)"
printf '%s\n' "$sums" >SHA256SUMS
