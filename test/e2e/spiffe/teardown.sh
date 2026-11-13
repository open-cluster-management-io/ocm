#!/usr/bin/env bash
# Deletes the two e2e kind clusters, and nothing else.
set -euo pipefail
source "$(dirname "$0")/env.sh"
for c in "$HUB_CLUSTER" "$SPOKE_CLUSTER"; do
  kind delete cluster --name "$c" --kubeconfig "$KUBECONFIG"
done
