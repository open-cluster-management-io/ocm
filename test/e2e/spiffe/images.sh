#!/usr/bin/env bash
# Builds the OCM images from this checkout and loads them into the e2e kind clusters, tagged
# $IMAGE_TAG so that nothing is pulled from a registry instead.
set -euo pipefail
source "$(dirname "$0")/env.sh"
cd "$SPIFFE_E2E_DIR/../../.."
IMAGE_REGISTRY="${IMAGE_REGISTRY:-quay.io/open-cluster-management}"
IMAGE_TAG="${IMAGE_TAG:-spiffe-e2e}"
images=(registration:Dockerfile.registration work:Dockerfile.work placement:Dockerfile.placement
  registration-operator:Dockerfile.registration-operator addon-manager:Dockerfile.addon)

if [[ "${SKIP_BUILD:-}" != true ]]; then
  for i in "${images[@]}"; do
    name=${i%%:*} file=${i#*:}
    docker build -f "build/$file" -t "$IMAGE_REGISTRY/$name:$IMAGE_TAG" \
      --build-arg OS=linux --build-arg ARCH=amd64 .
  done
fi
for c in "$HUB_CLUSTER" "$SPOKE_CLUSTER"; do
  for i in "${images[@]}"; do
    kind load docker-image --name "$c" "$IMAGE_REGISTRY/${i%%:*}:$IMAGE_TAG"
  done
done
