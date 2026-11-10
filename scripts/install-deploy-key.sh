#!/usr/bin/env bash
# Installs DEPLOY_KEY as ~/.ssh/deploy on a release runner, with GitHub's
# ed25519 host key as `gh api meta` lists it.
set -euo pipefail
mkdir -p ~/.ssh
chmod 700 ~/.ssh
echo 'github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl' >>~/.ssh/known_hosts
printf '%s\n' "$DEPLOY_KEY" >~/.ssh/deploy
chmod 600 ~/.ssh/deploy
