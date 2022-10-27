#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# Unset CDPATH so that path interpolation can work correctly
# https://github.com/kubernetes/kubernetes/issues/52255
unset CDPATH

# Until all GOPATH references are removed from all build scripts as well,
# explicitly disable module mode to avoid picking up user-set GO111MODULE preferences.
# As individual scripts (like hack/update-vendor.sh) make use of go modules,
# they can explicitly set GO111MODULE=on
export GO111MODULE=off

# The root of the build/dist directory
KUBE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"

source "${KUBE_ROOT}/hack/lib/util.sh"
source "${KUBE_ROOT}/hack/lib/logging.sh"

kube::log::install_errexit
kube::util::ensure-bash-version

source "${KUBE_ROOT}/hack/lib/etcd.sh"
