#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project

set -Eeuo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "${here}/../.." && pwd)
hub=${HUB:-sql-hub}
spoke1=${CLUSTER1:-sql-spoke1}
spoke2=${CLUSTER2:-sql-spoke2}
api_namespace=ocm-sql-api
api_port=7443
api_host_port=${OCM_API_HOST_PORT:-17443}
pg_image=${POSTGRES_IMAGE:-docker.io/library/postgres:16-alpine}
kine_image=${KINE_IMAGE:-docker.io/rancher/kine:v0.14.14}
kubernetes_version=${KUBERNETES_VERSION:-v1.36.1}
node_image=${KIND_NODE_IMAGE:-kindest/node:${kubernetes_version}}
runtime_dir=${RUNTIME_DIR:-/tmp/opencode/ocm-sql-hub-poc-${hub}}
pg_user=ocm_kine
pg_db=ocm_kine

die() { echo "ERROR: $*" >&2; exit 1; }
retry() {
    local attempts=$1 delay=$2
    shift 2
    for _ in $(seq 1 "${attempts}"); do
        if "$@"; then return 0; fi
        sleep "${delay}"
    done
    return 1
}
rewrite_spoke_bootstrap_endpoint() {
    local context=$1 secret_path="$runtime_dir/${1##kind-}-bootstrap.kubeconfig" cluster_name patch_file
    for _ in $(seq 1 60); do
        if kubectl --context "${context}" -n open-cluster-management-agent get secret bootstrap-hub-kubeconfig >/dev/null 2>&1; then
            break
        fi
        sleep 1
    done
    kubectl --context "${context}" -n open-cluster-management-agent get secret bootstrap-hub-kubeconfig \
        -o jsonpath='{.data.kubeconfig}' | base64 -d >"${secret_path}"
    cluster_name=$(kubectl config view --kubeconfig "${secret_path}" -o jsonpath='{.clusters[0].name}')
    [[ -n "${cluster_name}" ]] || die "bootstrap kubeconfig has no cluster entry for ${context}"
    kubectl config set-cluster "${cluster_name}" --server="${api_internal_url}" --kubeconfig="${secret_path}" >/dev/null
    patch_file=$(mktemp "${runtime_dir}/bootstrap-patch.XXXXXX")
    python3 - "${secret_path}" "${patch_file}" <<'PY'
import base64, json, sys
with open(sys.argv[1], "rb") as f:
    encoded = base64.b64encode(f.read()).decode("ascii")
with open(sys.argv[2], "w", encoding="utf-8") as f:
    json.dump({"data": {"kubeconfig": encoded}}, f)
PY
    kubectl --context "${context}" -n open-cluster-management-agent patch secret bootstrap-hub-kubeconfig \
        --type=merge --patch-file="${patch_file}" >/dev/null
    rm -f "${patch_file}" "${secret_path}"
}

for name in "${hub}" "${spoke1}" "${spoke2}"; do
    [[ "${name}" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || die "invalid kind cluster name '${name}'"
done
[[ "${hub}" != "${spoke1}" && "${hub}" != "${spoke2}" && "${spoke1}" != "${spoke2}" ]] || die "cluster names must be distinct"
for tool in kind kubectl helm clusteradm openssl python3; do
    command -v "${tool}" >/dev/null 2>&1 || die "'${tool}' is required"
done
if [[ -r /proc/sys/fs/inotify/max_user_instances ]] && (( $(cat /proc/sys/fs/inotify/max_user_instances) < 1024 )); then
    die "fs.inotify.max_user_instances is too low; set it to 8192 and retry"
fi
if [[ -r /proc/sys/kernel/keys/maxkeys ]] && (( $(cat /proc/sys/kernel/keys/maxkeys) < 1000 )); then
    die "kernel keyring quota is too low for kind containers; set kernel.keys.maxkeys=10000 and kernel.keys.maxbytes=1000000, then retry"
fi
if [[ -r /proc/sys/kernel/keys/maxbytes ]] && (( $(cat /proc/sys/kernel/keys/maxbytes) < 1000000 )); then
    die "kernel keyring byte quota is too low for kind containers; set kernel.keys.maxkeys=10000 and kernel.keys.maxbytes=1000000, then retry"
fi

if [[ -n "${CONTAINER_ENGINE:-}" ]]; then
    engine=${CONTAINER_ENGINE}
elif command -v docker >/dev/null 2>&1; then
    engine=docker
else
    engine=podman
fi
command -v "${engine}" >/dev/null 2>&1 || die "container engine '${engine}' is not installed"
case "${engine}" in
    podman) export KIND_EXPERIMENTAL_PROVIDER=podman ;;
    docker) export KIND_EXPERIMENTAL_PROVIDER=docker ;;
    *) die "CONTAINER_ENGINE must be docker or podman" ;;
esac
existing_clusters=$(kind get clusters 2>/dev/null || true)
for name in "${hub}" "${spoke1}" "${spoke2}"; do
    grep -qx "${name}" <<<"${existing_clusters}" && die "kind cluster '${name}' already exists; choose another name"
done
if [[ -e "${runtime_dir}" ]] && find "${runtime_dir}" -mindepth 1 -print -quit | grep -q .; then
    die "runtime directory '${runtime_dir}' is not empty; inspect it or choose another RUNTIME_DIR"
fi

if [[ "${engine}" == podman ]]; then
    podman network exists kind || podman network create kind >/dev/null
else
    docker network inspect kind >/dev/null 2>&1 || docker network create kind >/dev/null
fi
umask 077
mkdir -p "${runtime_dir}/pki"
chmod 700 "${runtime_dir}" "${runtime_dir}/pki"
touch "${runtime_dir}/.ocm-sql-hosted-poc"
printf '%s\n' "${engine}" >"${runtime_dir}/container-engine"
db_password=$(openssl rand -hex 20)

echo "=== creating ordinary etcd-backed hosting cluster ${hub} ==="
if ! python3 - "${api_host_port}" <<'PY'
import socket, sys
s = socket.socket()
try:
    s.bind(("127.0.0.1", int(sys.argv[1])))
except OSError:
    sys.exit(1)
finally:
    s.close()
PY
then
    die "localhost port ${api_host_port} is already in use; set OCM_API_HOST_PORT"
fi
cat >"${runtime_dir}/host-kind.yaml" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: ${hub}
nodes:
- role: control-plane
  image: ${node_image}
  extraPortMappings:
  - containerPort: ${api_port}
    hostPort: ${api_host_port}
    listenAddress: "127.0.0.1"
    protocol: TCP
EOF
kind create cluster --name "${hub}" --config "${runtime_dir}/host-kind.yaml" --wait 5m
hub_node="${hub}-control-plane"
hub_ip=$("${engine}" inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${hub_node}")
[[ -n "${hub_ip}" ]] || die "could not determine hosting control-plane IP"
api_internal_url="https://${hub_ip}:${api_port}"
api_host_url="https://127.0.0.1:${api_host_port}"

echo "=== generating standalone OCM API certificates ==="
openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -subj "/CN=ocm-sql-api-serving-ca" \
    -addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign,cRLSign" \
    -keyout "${runtime_dir}/pki/server-ca.key" -out "${runtime_dir}/pki/server-ca.crt" >/dev/null 2>&1
cat >"${runtime_dir}/pki/server.ext" <<EXT
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=IP:${hub_ip},IP:127.0.0.1,DNS:localhost
EXT
openssl req -new -newkey rsa:2048 -nodes -subj "/CN=ocm-sql-api" \
    -keyout "${runtime_dir}/pki/apiserver.key" -out "${runtime_dir}/pki/apiserver.csr" >/dev/null 2>&1
openssl x509 -req -days 825 -in "${runtime_dir}/pki/apiserver.csr" \
    -CA "${runtime_dir}/pki/server-ca.crt" -CAkey "${runtime_dir}/pki/server-ca.key" \
    -CAcreateserial -extfile "${runtime_dir}/pki/server.ext" -out "${runtime_dir}/pki/apiserver.crt" >/dev/null 2>&1
for ca in admin ocm-client; do
    openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -subj "/CN=ocm-sql-${ca}-ca" \
        -addext "basicConstraints=critical,CA:TRUE,pathlen:0" -addext "keyUsage=critical,keyCertSign,cRLSign" \
        -keyout "${runtime_dir}/pki/${ca}-ca.key" -out "${runtime_dir}/pki/${ca}-ca.crt" >/dev/null 2>&1
done
cat >"${runtime_dir}/pki/admin.ext" <<'EXT'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=clientAuth
EXT
openssl req -new -newkey rsa:2048 -nodes -subj "/CN=ocm-sql-poc-admin/O=system:masters" \
    -keyout "${runtime_dir}/pki/admin.key" -out "${runtime_dir}/pki/admin.csr" >/dev/null 2>&1
openssl x509 -req -days 825 -in "${runtime_dir}/pki/admin.csr" \
    -CA "${runtime_dir}/pki/admin-ca.crt" -CAkey "${runtime_dir}/pki/admin-ca.key" \
    -CAcreateserial -extfile "${runtime_dir}/pki/admin.ext" -out "${runtime_dir}/pki/admin.crt" >/dev/null 2>&1
openssl genrsa -out "${runtime_dir}/pki/sa.key" 2048 >/dev/null 2>&1
openssl rsa -in "${runtime_dir}/pki/sa.key" -pubout -out "${runtime_dir}/pki/sa.pub" >/dev/null 2>&1
cat "${runtime_dir}/pki/admin-ca.crt" "${runtime_dir}/pki/ocm-client-ca.crt" >"${runtime_dir}/pki/client-ca.crt"
for endpoint in internal host; do
    kubeconfig="${runtime_dir}/ocm-api-${endpoint}.kubeconfig"
    if [[ "${endpoint}" == internal ]]; then server=${api_internal_url}; else server=${api_host_url}; fi
    kubectl config set-cluster ocm-sql-api --server="${server}" --certificate-authority="${runtime_dir}/pki/server-ca.crt" \
        --embed-certs=true --kubeconfig="${kubeconfig}" >/dev/null
    kubectl config set-credentials ocm-sql-poc-admin --client-certificate="${runtime_dir}/pki/admin.crt" \
        --client-key="${runtime_dir}/pki/admin.key" --embed-certs=true --kubeconfig="${kubeconfig}" >/dev/null
    kubectl config set-context ocm-sql-api --cluster=ocm-sql-api --user=ocm-sql-poc-admin --kubeconfig="${kubeconfig}" >/dev/null
    kubectl config use-context ocm-sql-api --kubeconfig="${kubeconfig}" >/dev/null
done

echo "=== deploying PostgreSQL and API credentials into hosting kind ==="
kubectl --context "kind-${hub}" create namespace "${api_namespace}"
kubectl --context "kind-${hub}" -n "${api_namespace}" create secret generic ocm-sql-postgres \
    --from-literal=POSTGRES_USER="${pg_user}" --from-literal=POSTGRES_PASSWORD="${db_password}" --from-literal=POSTGRES_DB="${pg_db}"
kubectl --context "kind-${hub}" -n "${api_namespace}" create secret generic ocm-sql-kine \
    --from-literal=KINE_ENDPOINT="postgres://${pg_user}:${db_password}@ocm-sql-postgres.${api_namespace}.svc.cluster.local:5432/${pg_db}?sslmode=disable"
kubectl --context "kind-${hub}" -n "${api_namespace}" create secret generic ocm-sql-api-pki \
    --from-file=apiserver.crt="${runtime_dir}/pki/apiserver.crt" --from-file=apiserver.key="${runtime_dir}/pki/apiserver.key" \
    --from-file=apiserver-ca.crt="${runtime_dir}/pki/server-ca.crt" --from-file=client-ca.crt="${runtime_dir}/pki/client-ca.crt" \
    --from-file=ocm-client-ca.crt="${runtime_dir}/pki/ocm-client-ca.crt" \
    --from-file=ocm-client-ca.key="${runtime_dir}/pki/ocm-client-ca.key" \
    --from-file=sa.key="${runtime_dir}/pki/sa.key" --from-file=sa.pub="${runtime_dir}/pki/sa.pub" \
    --from-file=admin.kubeconfig="${runtime_dir}/ocm-api-internal.kubeconfig"

cat <<EOF | kubectl --context "kind-${hub}" -n "${api_namespace}" apply -f -
apiVersion: v1
kind: Service
metadata:
  name: ocm-sql-postgres
spec:
  ports:
  - name: postgres
    port: 5432
    targetPort: 5432
  selector:
    app: ocm-sql-postgres
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ocm-sql-postgres
spec:
  serviceName: ocm-sql-postgres
  replicas: 1
  selector:
    matchLabels:
      app: ocm-sql-postgres
  template:
    metadata:
      labels:
        app: ocm-sql-postgres
    spec:
      containers:
      - name: postgres
        image: ${pg_image}
        envFrom:
        - secretRef:
            name: ocm-sql-postgres
        ports:
        - name: postgres
          containerPort: 5432
        readinessProbe:
          exec:
            command: ["sh", "-c", "pg_isready -U \$POSTGRES_USER -d \$POSTGRES_DB"]
          periodSeconds: 3
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
        volumeMounts:
        - name: data
          mountPath: /var/lib/postgresql/data
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: [ReadWriteOnce]
      resources:
        requests:
          storage: 1Gi
EOF
kubectl --context "kind-${hub}" -n "${api_namespace}" rollout status statefulset/ocm-sql-postgres --timeout=180s

echo "=== deploying a separate Kine-backed OCM kube-apiserver ==="
cat <<EOF | kubectl --context "kind-${hub}" -n "${api_namespace}" apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ocm-sql-apiserver
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ocm-sql-apiserver
  template:
    metadata:
      labels:
        app: ocm-sql-apiserver
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      nodeSelector:
        kubernetes.io/hostname: ${hub_node}
      tolerations:
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
        effect: NoSchedule
      containers:
      - name: kine
        image: ${kine_image}
        command: ["/bin/kine"]
        args: ["--listen-address=127.0.0.1:12379", "--metrics-bind-address=0", "--log-format=json"]
        env:
        - name: KINE_ENDPOINT
          valueFrom:
            secretKeyRef:
              name: ocm-sql-kine
              key: KINE_ENDPOINT
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          runAsNonRoot: true
          runAsUser: 65534
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
      - name: kube-apiserver
        image: registry.k8s.io/kube-apiserver:${kubernetes_version}
        command: ["kube-apiserver"]
        args:
        - --advertise-address=${hub_ip}
        - --allow-privileged=true
        - --anonymous-auth=false
        - --api-audiences=https://kubernetes.default.svc.cluster.local
        - --authorization-mode=RBAC
        - --bind-address=0.0.0.0
        - --client-ca-file=/etc/ocm-api/client-ca.crt
        - --enable-admission-plugins=NamespaceLifecycle,LimitRanger,ServiceAccount,DefaultStorageClass,DefaultTolerationSeconds,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota
        - --enable-bootstrap-token-auth=true
        - --endpoint-reconciler-type=none
        - --etcd-servers=http://127.0.0.1:12379
        - --etcd-prefix=/registry
        - --service-cluster-ip-range=10.97.0.0/16
        - --service-account-issuer=https://kubernetes.default.svc.cluster.local
        - --service-account-key-file=/etc/ocm-api/sa.pub
        - --service-account-signing-key-file=/etc/ocm-api/sa.key
        - --secure-port=${api_port}
        - --tls-cert-file=/etc/ocm-api/apiserver.crt
        - --tls-private-key-file=/etc/ocm-api/apiserver.key
        - --v=2
        ports:
        - name: https
          containerPort: ${api_port}
          hostPort: ${api_port}
        readinessProbe:
          tcpSocket:
            port: ${api_port}
          periodSeconds: 3
          failureThreshold: 30
        livenessProbe:
          tcpSocket:
            port: ${api_port}
          initialDelaySeconds: 15
          periodSeconds: 10
        volumeMounts:
        - name: pki
          mountPath: /etc/ocm-api
          readOnly: true
        resources:
          requests:
            cpu: 250m
            memory: 512Mi
          limits:
            memory: 1Gi
      volumes:
      - name: pki
        secret:
          secretName: ocm-sql-api-pki
EOF

cat <<EOF | kubectl --context "kind-${hub}" -n "${api_namespace}" apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ocm-sql-controller-manager
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ocm-sql-controller-manager
  template:
    metadata:
      labels:
        app: ocm-sql-controller-manager
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      nodeSelector:
        kubernetes.io/hostname: ${hub_node}
      tolerations:
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
        effect: NoSchedule
      containers:
      - name: kube-controller-manager
        image: registry.k8s.io/kube-controller-manager:${kubernetes_version}
        command: ["kube-controller-manager"]
        args:
        - --kubeconfig=/etc/ocm-api/admin.kubeconfig
        - --authentication-kubeconfig=/etc/ocm-api/admin.kubeconfig
        - --authorization-kubeconfig=/etc/ocm-api/admin.kubeconfig
        - --controllers=namespace,serviceaccount,serviceaccount-token,garbagecollector,bootstrapsigner,tokencleaner,csrsigning
        - --leader-elect=false
        - --secure-port=10258
        - --root-ca-file=/etc/ocm-api/apiserver-ca.crt
        - --service-account-private-key-file=/etc/ocm-api/sa.key
        - --cluster-signing-cert-file=/etc/ocm-api/ocm-client-ca.crt
        - --cluster-signing-key-file=/etc/ocm-api/ocm-client-ca.key
        - --cluster-signing-duration=8760h
        - --v=2
        volumeMounts:
        - name: pki
          mountPath: /etc/ocm-api
          readOnly: true
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
          limits:
            memory: 512Mi
      volumes:
      - name: pki
        secret:
          secretName: ocm-sql-api-pki
EOF

echo "=== waiting for the standalone API and supporting controllers ==="
if ! kubectl --context "kind-${hub}" -n "${api_namespace}" rollout status deployment/ocm-sql-apiserver --timeout=300s; then
    kubectl --context "kind-${hub}" -n "${api_namespace}" logs deployment/ocm-sql-apiserver -c kube-apiserver --tail=100 >&2 || true
    kubectl --context "kind-${hub}" -n "${api_namespace}" logs deployment/ocm-sql-apiserver -c kine --tail=100 >&2 || true
    die "standalone OCM kube-apiserver failed to become ready"
fi
kubectl --context "kind-${hub}" -n "${api_namespace}" rollout status deployment/ocm-sql-controller-manager --timeout=300s
retry 30 2 kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" --request-timeout=5s \
    get --raw=/readyz >/dev/null 2>&1 || die "standalone OCM API endpoint is not responding from the host"

echo "=== preparing OCM namespaces and bootstrap identity ==="
for namespace in kube-system kube-public kube-node-lease open-cluster-management cluster-manager; do
    kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" create namespace "${namespace}" --dry-run=client -o yaml |
        kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" apply -f -
done
kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" -n kube-system create configmap extension-apiserver-authentication \
    --from-file=client-ca-file="${runtime_dir}/pki/client-ca.crt" \
    --from-file=requestheader-client-ca-file="${runtime_dir}/pki/admin-ca.crt" \
    --from-literal=requestheader-allowed-names='["ocm-sql-poc-admin"]' \
    --from-literal=requestheader-username-headers='["X-Remote-User"]' \
    --from-literal=requestheader-group-headers='["X-Remote-Group"]' \
    --from-literal=requestheader-extra-headers-prefix='["X-Remote-Extra-"]' \
    --dry-run=client -o yaml | kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" apply -f -
kubectl --context "kind-${hub}" create namespace open-cluster-management --dry-run=client -o yaml | kubectl --context "kind-${hub}" apply -f -
kubectl --context "kind-${hub}" create namespace cluster-manager --dry-run=client -o yaml | kubectl --context "kind-${hub}" apply -f -
kubectl --context "kind-${hub}" -n cluster-manager create secret tls signer-secret \
    --cert="${runtime_dir}/pki/ocm-client-ca.crt" --key="${runtime_dir}/pki/ocm-client-ca.key" --dry-run=client -o yaml |
    kubectl --context "kind-${hub}" apply -f -
kubectl --context "kind-${hub}" -n cluster-manager create secret generic external-hub-kubeconfig \
    --from-file=kubeconfig="${runtime_dir}/ocm-api-internal.kubeconfig" --dry-run=client -o yaml | kubectl --context "kind-${hub}" apply -f -

cat >"${runtime_dir}/hosted-values.yaml" <<EOF
clusterManager:
  create: true
  mode: Hosted
  resourceRequirement:
    type: Default
  hosted:
    registrationWebhookConfiguration:
      address: cluster-manager-registration-webhook.cluster-manager.svc
      port: 30443
      bindConfiguration: {port: 30443, healthProbePort: 0, metricsPort: 0, hostNetwork: true}
    workWebhookConfiguration:
      address: cluster-manager-work-webhook.cluster-manager.svc
      port: 31443
      bindConfiguration: {port: 31443, healthProbePort: 0, metricsPort: 0, hostNetwork: true}
  registrationConfiguration:
    autoApproveUsers:
    - system:serviceaccount:open-cluster-management:agent-registration-bootstrap
    featureGates:
    - feature: DefaultClusterSet
      mode: Enable
    - feature: ManagedClusterAutoApproval
      mode: Enable
    registrationDrivers:
    - authType: csr
  workConfiguration:
    workDriver: kube
  addOnManagerConfiguration:
    featureGates:
    - feature: AddonManagement
      mode: Disable
  placementConfiguration:
    featureGates:
    - feature: PlacementDebugServer
      mode: Disable
createBootstrapSA: true
EOF

cat >"${runtime_dir}/external-bootstrap-rbac.yaml" <<'BOOTSTRAP_RBAC'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: agent-registration-bootstrap
  namespace: open-cluster-management
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: open-cluster-management:bootstrap
rules:
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get"]
- apiGroups: ["certificates.k8s.io"]
  resources: ["certificatesigningrequests"]
  verbs: ["create", "get", "list", "watch"]
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["managedclusters"]
  verbs: ["get", "create", "list", "watch"]
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["managedclustersets/join"]
  verbs: ["create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: open-cluster-management:bootstrap:agent-registration
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: open-cluster-management:bootstrap
subjects:
- kind: ServiceAccount
  name: agent-registration-bootstrap
  namespace: open-cluster-management
BOOTSTRAP_RBAC
kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" apply -f "${runtime_dir}/external-bootstrap-rbac.yaml"

echo "=== installing Hosted OCM controllers into etcd-backed hosting kind ==="
helm upgrade --install cluster-manager "${repo_root}/deploy/cluster-manager/chart/cluster-manager" \
    --namespace open-cluster-management --create-namespace --values "${runtime_dir}/hosted-values.yaml" --wait --timeout 10m
kubectl --context "kind-${hub}" wait --for=condition=Applied clustermanager/cluster-manager --timeout=10m
for deployment in cluster-manager-registration-controller cluster-manager-registration-webhook \
    cluster-manager-work-webhook cluster-manager-placement-controller cluster-manager-addon-webhook; do
    kubectl --context "kind-${hub}" -n cluster-manager rollout status "deployment/${deployment}" --timeout=5m
done

echo "=== exposing hosted webhooks through hosting-cluster DNS ==="
cat <<EOF | kubectl --context "kind-${hub}" apply -f -
apiVersion: v1
kind: Service
metadata:
  name: cluster-manager-registration-webhook
  namespace: cluster-manager
spec:
  ports:
  - name: https
    port: 30443
    targetPort: 30443
  selector:
    app: cluster-manager-registration-webhook
---
apiVersion: v1
kind: Service
metadata:
  name: cluster-manager-work-webhook
  namespace: cluster-manager
spec:
  ports:
  - name: https
    port: 31443
    targetPort: 31443
  selector:
    app: cluster-manager-work-webhook
---
apiVersion: v1
kind: Service
metadata:
  name: cluster-manager-addon-webhook
  namespace: cluster-manager
spec:
  ports:
  - name: https
    port: 9443
    targetPort: 9443
  selector:
    app: cluster-manager-addon-webhook
EOF
cat <<'EOF' | kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" apply -f -
apiVersion: v1
kind: Service
metadata:
  name: cluster-manager-addon-webhook
  namespace: cluster-manager
spec:
  type: ExternalName
  externalName: cluster-manager-addon-webhook.cluster-manager.svc
  ports:
  - name: https
    port: 9443
EOF
kubectl --context "kind-${hub}" patch clustermanager cluster-manager --type=merge \
    -p '{"spec":{"addOnManagerConfiguration":{"featureGates":[{"feature":"AddonManagement","mode":"Enable"}]}}}'
for _ in $(seq 1 60); do
    if kubectl --context "kind-${hub}" -n cluster-manager get deployment/cluster-manager-addon-manager-controller >/dev/null 2>&1; then
        break
    fi
    sleep 2
done
kubectl --context "kind-${hub}" -n cluster-manager get deployment/cluster-manager-addon-manager-controller >/dev/null || \
    die "ClusterManager did not create the add-on manager deployment after enabling AddonManagement"
kubectl --context "kind-${hub}" -n cluster-manager rollout status deployment/cluster-manager-addon-manager-controller --timeout=5m

echo "=== creating the two kind spokes ==="
kind create cluster --name "${spoke1}" --image "${node_image}" --wait 5m
kind create cluster --name "${spoke2}" --image "${node_image}" --wait 5m
bootstrap_token=$(kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" \
    -n open-cluster-management create token agent-registration-bootstrap --duration=24h)
for spoke in "${spoke1}" "${spoke2}"; do
    clusteradm join --hub-token "${bootstrap_token}" --hub-apiserver "${api_host_url}" \
        --ca-file "${runtime_dir}/pki/server-ca.crt" --cluster-name "${spoke}" --context "kind-${spoke}"
    rewrite_spoke_bootstrap_endpoint "kind-${spoke}"
done
for spoke in "${spoke1}" "${spoke2}"; do
    for _ in $(seq 1 300); do
        if kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" \
            get "managedcluster/${spoke}" >/dev/null 2>&1; then
            break
        fi
        sleep 1
    done
    kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" get "managedcluster/${spoke}" >/dev/null || \
        die "registration agent did not create ManagedCluster ${spoke}"
done
clusteradm accept --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" --clusters "${spoke1},${spoke2}" --wait
for spoke in "${spoke1}" "${spoke2}"; do
    kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" wait --for=condition=ManagedClusterJoined \
        "managedcluster/${spoke}" --timeout=300s
    kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" wait --for=condition=ManagedClusterConditionAvailable \
        "managedcluster/${spoke}" --timeout=300s
done

echo "=== verifying ManifestWork delivery through the standalone OCM API ==="
for spoke in "${spoke1}" "${spoke2}"; do
    cat <<EOF | kubectl --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" apply --validate=false -f -
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata:
  name: sql-hosted-smoke
  namespace: ${spoke}
spec:
  workload:
    manifests:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: sql-hosted-smoke
        namespace: default
      data:
        stored-by: kine-postgresql
EOF
    delivered=false
    for _ in $(seq 1 90); do
        value=$(kubectl --context "kind-${spoke}" -n default get configmap sql-hosted-smoke -o jsonpath='{.data.stored-by}' 2>/dev/null || true)
        if [[ "${value}" == kine-postgresql ]]; then delivered=true; break; fi
        sleep 2
    done
    [[ "${delivered}" == true ]] || die "ManifestWork did not reach ${spoke}"
done

cat >"${runtime_dir}/addon-workload.yaml" <<'ADDON_WORKLOAD'
apiVersion: v1
kind: ConfigMap
metadata:
  name: sql-poc-addon
  namespace: default
data:
  stored-by: kine-postgresql-addon-manager
ADDON_WORKLOAD
clusteradm addon create sql-poc-addon --filename "${runtime_dir}/addon-workload.yaml" \
    --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig" --overwrite
clusteradm addon enable --names sql-poc-addon --clusters "${spoke1},${spoke2}" --kubeconfig "${runtime_dir}/ocm-api-host.kubeconfig"
for spoke in "${spoke1}" "${spoke2}"; do
    delivered=false
    for _ in $(seq 1 90); do
        value=$(kubectl --context "kind-${spoke}" -n default get configmap sql-poc-addon -o jsonpath='{.data.stored-by}' 2>/dev/null || true)
        if [[ "${value}" == kine-postgresql-addon-manager ]]; then delivered=true; break; fi
        sleep 2
    done
    [[ "${delivered}" == true ]] || die "Add-on workload did not reach ${spoke}"
done

echo "=== POC ready ==="
echo "Hosting API: kind-${hub} (native etcd)"
echo "OCM API:     ${api_internal_url} (Kine -> PostgreSQL)"
echo "Host access: ${api_host_url} (localhost port-forward through kind)"
echo "Spokes:      kind-${spoke1}, kind-${spoke2}"
echo "Kubeconfig:  ${runtime_dir}/ocm-api-host.kubeconfig"
echo "Teardown:    ${here}/local-down.sh"
