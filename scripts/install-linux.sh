#!/bin/sh
# Adds the bowel package repository to this machine and installs bowel, which
# the system's package manager then keeps up to date:
#
#   curl -fsSL https://21stware.github.io/packages/install.sh | sh
#
# The release workflow publishes this file next to the repository.
set -eu

# Everything runs from main, called on the last line, so that a download cut
# short runs nothing.
main() {
	repo="${BOWEL_REPO_URL:-https://21stware.github.io/packages}"

	if [ "$(id -u)" -eq 0 ]; then
		sudo=""
	elif command -v sudo >/dev/null 2>&1; then
		sudo="sudo"
	else
		echo "Run this as root, or install sudo." >&2
		exit 1
	fi

	if command -v apt-get >/dev/null 2>&1; then
		curl -fsSL "$repo/bowel.gpg" | $sudo tee /usr/share/keyrings/bowel-archive-keyring.gpg >/dev/null
		$sudo chmod 644 /usr/share/keyrings/bowel-archive-keyring.gpg
		echo "deb [signed-by=/usr/share/keyrings/bowel-archive-keyring.gpg] $repo/apt stable main" |
			$sudo tee /etc/apt/sources.list.d/bowel.list >/dev/null
		$sudo apt-get update
		$sudo apt-get install -y bowel
	elif command -v dnf >/dev/null 2>&1; then
		$sudo tee /etc/yum.repos.d/bowel.repo >/dev/null <<EOF
[bowel]
name=bowel
baseurl=$repo/rpm/\$basearch
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=$repo/bowel.asc
EOF
		$sudo dnf install -y bowel
	else
		echo "This script supports apt and dnf. Other packages are at https://github.com/21stware/bowel/releases/latest" >&2
		exit 1
	fi
}

main "$@"
