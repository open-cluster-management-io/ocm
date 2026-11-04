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
# Cap the whole run as well as each call. The per-call limit alone does not
# bound the total, and the job still has to upload the artifact afterwards.
TOTAL_BUDGET_SECONDS="${TOTAL_BUDGET_SECONDS:-480}"

deadline=$(($(date +%s) + TOTAL_BUDGET_SECONDS))

# budget_left reports whether there is still time to make another call.
budget_left() {
  [ "$(date +%s)" -lt "${deadline}" ]
}

# seconds_left reports the budget remaining, never below zero.
seconds_left() {
  local remaining=$((deadline - $(date +%s)))
  if [ "${remaining}" -lt 0 ]; then
    remaining=0
  fi
  echo "${remaining}"
}

mkdir -p "${OUTPUT_DIR}"

# --request-timeout bounds a single request but not kubectl's retry backoff on
# connection errors, so also cap the wall clock. `timeout` is coreutils and is
# present on the GitHub runners; fall back to running unwrapped without it.
# stdin is closed on every call: without credentials kubectl prompts for a
# username and would otherwise block forever waiting on it.
#
# Each call is capped at the shorter of CALL_TIMEOUT and the budget remaining,
# so no single call can run past the overall deadline.
if command -v timeout >/dev/null 2>&1; then
  kube() {
    local limit="${CALL_TIMEOUT%s}" remaining
    remaining=$(seconds_left)
    if [ "${remaining}" -lt "${limit}" ]; then
      limit="${remaining}"
    fi
    if [ "${limit}" -lt 1 ]; then
      return 124
    fi

    timeout --kill-after=5s "${limit}s" \
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
  if ! budget_left; then
    echo "skipped: collection budget of ${TOTAL_BUDGET_SECONDS}s exhausted" >"${file}"
    return
  fi
  kube "$@" >"${file}" 2>&1
}

# discover <error-file> <kubectl args...> prints the command's stdout. On
# failure it records why in <error-file>, including the exit status, since a
# call killed by `timeout` can fail without writing anything to stderr.
discover() {
  local errfile="${OUTPUT_DIR}/$1"
  shift
  mkdir -p "$(dirname "${errfile}")"

  if ! budget_left; then
    echo "skipped: collection budget of ${TOTAL_BUDGET_SECONDS}s exhausted" >"${errfile}"
    return 1
  fi

  # Keep stderr aside and only publish it if the command actually failed:
  # kubectl writes harmless warnings there on success, and a file named
  # ...-discovery-error.txt holding one would be misleading.
  local out status stderr_file
  stderr_file=$(mktemp)
  out=$(kube "$@" 2>"${stderr_file}")
  status=$?

  if [ "${status}" -ne 0 ]; then
    {
      cat "${stderr_file}"
      echo "command failed with exit status ${status}: kubectl $*"
    } >"${errfile}"
    rm -f "${stderr_file}"
    return 1
  fi

  rm -f "${stderr_file}"
  printf '%s\n' "${out}"
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
  discover "namespace-discovery-error.txt" get namespaces \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' |
    grep -E '^(open-cluster-management|e2e-|klusterlet)' || true
)

for ns in ${namespaces}; do
  run "describe/${ns}-pods.txt" describe pods -n "${ns}"
  mkdir -p "${OUTPUT_DIR}/logs/${ns}"

  # Discovery errors go into the artifact too: an empty logs/ directory with no
  # explanation is the thing this script exists to avoid. These names are not
  # dotfiles on purpose, since upload-artifact drops hidden files by default.
  if ! pods=$(discover "logs/${ns}/pod-discovery-error.txt" get pods -n "${ns}" \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'); then
    echo "failed to list pods in ${ns}, see logs/${ns}/pod-discovery-error.txt"
    pods=""
  fi

  for pod in ${pods}; do
    # One call per container rather than --all-containers: a container whose log
    # is unavailable would cut the whole pod's output short. That is routine
    # with --previous, where usually only one container has restarted.
    if ! containers=$(discover "logs/${ns}/${pod}-container-discovery-error.txt" \
      get pod "${pod}" -n "${ns}" \
      -o jsonpath='{range .spec.initContainers[*]}{.name}{"\n"}{end}{range .spec.containers[*]}{.name}{"\n"}{end}'); then
      echo "failed to list containers of ${ns}/${pod}, see logs/${ns}/${pod}-container-discovery-error.txt"
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
# real errors show up in the artifact. A failure always writes the exit status,
# so an empty file here really does mean success.
find "${OUTPUT_DIR}" -type f -name "*discovery-error.txt" -empty -delete
find "${OUTPUT_DIR}" -type d -empty -delete

if ! budget_left; then
  echo "WARNING: the ${TOTAL_BUDGET_SECONDS}s collection budget ran out; some files say so instead of holding output"
fi

echo "Collected:"
find "${OUTPUT_DIR}" -type f | sort

exit 0
