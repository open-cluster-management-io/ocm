#!/usr/bin/env bash
# Deletes the e2e kind clusters, and nothing else.
set -euo pipefail
source "$(dirname "$0")/env.sh"
for c in "$HUB_CLUSTER" "$SPOKE_CLUSTER" "$EAST_CLUSTER"; do
  if kind get clusters | grep -qx "$c"; then
    kind delete cluster --name "$c" --kubeconfig "$KUBECONFIG"
  fi
done
