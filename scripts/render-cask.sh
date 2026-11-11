#!/usr/bin/env bash
# Prints the Homebrew cask of a release, for Casks/bowel.rb in
# 21stware/homebrew-tap.
#
#   scripts/render-cask.sh 0.2.0 <sha256 of the dmg> >Casks/bowel.rb
set -euo pipefail
VERSION="$1"
SHA256="$2"

cat <<EOF
cask "bowel" do
  version "$VERSION"
  sha256 "$SHA256"

  url "https://github.com/21stware/bowel/releases/download/v#{version}/bowel-#{version}-arm64.dmg"
  name "bowel"
  desc "Desktop client for mihomo"
  homepage "https://bowel.dev/"

  livecheck do
    url :url
    strategy :github_latest
  end

  auto_updates true
  depends_on arch: :arm64
  depends_on macos: :monterey

  app "bowel.app"

  uninstall quit: "dev.moho.app"

  zap trash: "~/Library/Application Support/bowel"
end
EOF
