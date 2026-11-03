#!/usr/bin/env bash

# Dump cluster state and component logs after a failed e2e run, so an
# intermittent failure can be diagnosed from the run's artifacts instead of
# having to wait for it to happen again.
#
# Never fails the job: a missing namespace or an unreachable API server must
# not mask the test failure that triggered this script.

set -u
set -o pipefail

OUTPUT_DIR="${OUTPUT_DIR:-e2e-logs}"
KUBECTL="${KUBECTL:-kubectl}"
# Bound every call. If the API server is unreachable kubectl retries for
# minutes, and collecting diagnostics must not be what times the job out.
REQUEST_TIMEOUT="${REQUEST_TIMEOUT:-30s}"
CALL_TIMEOUT="${CALL_TIMEOUT:-60s}"

mkdir -p "${OUTPUT_DIR}"

# --request-timeout bounds a single request but not kubectl's retry backoff on
# connection errors, so also cap the wall clock. `timeout` is coreutils and is
# present on the GitHub runners; fall back to running unwrapped without it.
# stdin is closed on every call: without credentials kubectl prompts for a
# username and would otherwise block forever waiting on it.
if command -v timeout >/dev/null 2>&1; then
  kube() {
    timeout --kill-after=5s "${CALL_TIMEOUT}" \
      "${KUBECTL}" --request-timeout="${REQUEST_TIMEOUT}" "$@" </dev/null
  }
else
  kube() {
    "${KUBECTL}" --request-timeout="${REQUEST_TIMEOUT}" "$@" </dev/null
  }
fi

# run <file> <kubectl args...> writes both stdout and stderr to the file, so a
# failed command leaves its error in the artifact rather than vanishing.
run() {
  local file="${OUTPUT_DIR}/$1"
  shift
  mkdir -p "$(dirname "${file}")"
  kube "$@" >"${file}" 2>&1
}

echo "Collecting e2e diagnostics into ${OUTPUT_DIR}"

# Cluster-wide overview.
run nodes.txt get nodes -o wide
run pods-all-namespaces.txt get pods --all-namespaces -o wide
run events.txt get events --all-namespaces --sort-by=.lastTimestamp

# OCM custom resources. A hung ManifestWork keeps its finalizer and
# deletionTimestamp, so the full YAML is what identifies the stuck object.
for resource in \
  manifestworks.work.open-cluster-management.io \
  appliedmanifestworks.work.open-cluster-management.io \
  manifestworkreplicasets.work.open-cluster-management.io \
  managedclusters.cluster.open-cluster-management.io \
  managedclusteraddons.addon.open-cluster-management.io \
  clustermanagementaddons.addon.open-cluster-management.io \
  addontemplates.addon.open-cluster-management.io \
  klusterlets.operator.open-cluster-management.io \
  clustermanagers.operator.open-cluster-management.io; do
  short="${resource%%.*}"
  run "resources/${short}.yaml" get "${resource}" --all-namespaces -o yaml
done

# Component logs. Namespaces are discovered rather than hardcoded so this keeps
# working across the Default, Hosted and Singleton deploy modes. In Hosted mode
# the agent namespace is the klusterlet's own name (e2e-universal-klusterlet),
# not an open-cluster-management-* one, so match those too.
namespaces=$(
  kube get namespaces -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null |
    grep -E '^(open-cluster-management|e2e-|klusterlet)' || true
)

for ns in ${namespaces}; do
  run "describe/${ns}-pods.txt" describe pods -n "${ns}"
  mkdir -p "${OUTPUT_DIR}/logs/${ns}"

  # Discovery errors go into the artifact too: an empty logs/ directory with no
  # explanation is the thing this script exists to avoid.
  if ! pods=$(kube get pods -n "${ns}" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>"${OUTPUT_DIR}/logs/${ns}/.pod-discovery-error"); then
    echo "failed to list pods in ${ns}, see .pod-discovery-error"
    pods=""
  fi

  for pod in ${pods}; do
    # One call per container rather than --all-containers: a container whose log
    # is unavailable would cut the whole pod's output short. That is routine
    # with --previous, where usually only one container has restarted.
    if ! containers=$(kube get pod "${pod}" -n "${ns}" -o jsonpath='{range .spec.initContainers[*]}{.name}{"\n"}{end}{range .spec.containers[*]}{.name}{"\n"}{end}' \
      2>"${OUTPUT_DIR}/logs/${ns}/${pod}.container-discovery-error"); then
      echo "failed to list containers of ${ns}/${pod}, see ${pod}.container-discovery-error"
      containers=""
    fi

    for container in ${containers}; do
      run "logs/${ns}/${pod}/${container}.log" \
        logs -n "${ns}" "${pod}" -c "${container}" --tail=-1
      # A restarted pod is itself a likely cause, so keep the previous
      # container's log. Absent a restart this just records "not found".
      run "logs/${ns}/${pod}/${container}-previous.log" \
        logs -n "${ns}" "${pod}" -c "${container}" --tail=-1 --previous
    done
  done
done

# Discovery succeeded wherever the error file stayed empty; drop those so only
# real errors show up in the artifact.
find "${OUTPUT_DIR}" -type f -name "*discovery-error" -empty -delete
find "${OUTPUT_DIR}" -type d -empty -delete

echo "Collected:"
find "${OUTPUT_DIR}" -type f | sort

exit 0
