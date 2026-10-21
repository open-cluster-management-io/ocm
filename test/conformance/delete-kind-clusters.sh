#!/bin/bash

set -e

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

configure_kind
for cluster in "${HUB}" "${CLUSTER1}" "${CLUSTER2}"; do
    kind delete cluster --name "${cluster}"
done
