#!/usr/bin/env bash
# Reclaim disk on GitHub-hosted Ubuntu runners before heavy image builds.
set -euo pipefail

echo "::group::Disk before"
df -h /
echo "::endgroup::"

sudo rm -rf \
  /usr/share/dotnet \
  /usr/local/lib/android \
  /opt/ghc \
  /opt/hostedtoolcache/CodeQL \
  /usr/local/share/boost \
  /usr/share/swift \
  /usr/local/.ghcup || true

sudo docker image prune -af || true
sudo docker builder prune -af || true

echo "::group::Disk after"
df -h /
echo "::endgroup::"
