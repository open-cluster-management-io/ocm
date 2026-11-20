#!/bin/bash
# Deletes only the kind clusters created by setup.sh.
set -euo pipefail
for c in "${HUB:-was-hub}" "${SPOKE1:-was-spoke1}" "${SPOKE2:-was-spoke2}"; do
  kind delete cluster --name "${c}"
done
