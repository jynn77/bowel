# Sourced by the scripts that sign packages. Imports the release signing key
# from PACKAGES_GPG_PRIVATE_KEY (armored, without a passphrase) into a keyring
# of its own and sets SIGNING_KEY to its fingerprint.
if [ -z "${PACKAGES_GPG_PRIVATE_KEY:-}" ]; then
	echo "PACKAGES_GPG_PRIVATE_KEY is not set" >&2
	exit 1
fi
GNUPGHOME="$(mktemp -d)"
export GNUPGHOME
chmod 700 "$GNUPGHOME"
trap 'gpgconf --kill gpg-agent 2>/dev/null || true; rm -rf "$GNUPGHOME"' EXIT
printf '%s\n' "$PACKAGES_GPG_PRIVATE_KEY" | gpg --batch --quiet --import
SIGNING_KEY="$(gpg --batch --list-secret-keys --with-colons | awk -F: '/^fpr/ { print $10; exit }')"
if [ -z "$SIGNING_KEY" ]; then
	echo "PACKAGES_GPG_PRIVATE_KEY holds no secret key" >&2
	exit 1
fi
