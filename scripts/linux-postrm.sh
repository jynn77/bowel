#!/bin/sh
# Runs as root after the bowel package is removed: the .deb's postrm, the
# .rpm's %postun, and post_remove of the AUR package's install file.
#
# Turning TUN on leaves a polkit rule per user that the package does not own
# (internal/tun/dnsrule_linux.go): /etc/polkit-1/rules.d/49-bowel-tun-<uid>.rules.
# Without the app nothing would take it away again. The copy of the core and
# bowel's settings are in each user's home and stay, as a package's user data
# does.
#
# dpkg passes "remove" or "purge" (and "upgrade" and others, when the package
# stays), rpm passes the number of versions left: 0 when the last one goes.
case "${1:-}" in
remove | purge | 0)
	rm -f /etc/polkit-1/rules.d/49-bowel-tun-*.rules
	;;
esac
exit 0
