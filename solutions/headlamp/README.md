# Browse a fleet with Headlamp

[Headlamp](https://headlamp.dev/) is a Kubernetes web UI. Point it at an OCM hub and it
becomes a fleet UI: one page with a cluster chooser listing every managed cluster, no
per-cluster kubeconfig to build or rotate.

Nothing here is Headlamp-specific glue. Headlamp reads
[Cluster Inventory](https://multicluster.sigs.k8s.io/concepts/cluster-profile-api/)
`ClusterProfile` objects, and OCM produces them. The hub publishes a `ClusterProfile` per
managed cluster, `cluster-proxy` writes a reachable endpoint into its
`status.accessProviders`, and `managed-serviceaccount` mints and rotates the token behind
it. Headlamp calls OCM's `cp-creds` plugin to pick that token up. A cluster that joins the
fleet later appears in the UI as soon as it gets a `ManagedServiceAccount` — no Headlamp
restart, no kubeconfig edit.

```
hub cluster                                        managed cluster
├── registration  ClusterProfile=true
│     ManagedCluster ──► ClusterProfile (ns: headlamp)
├── cluster-proxy  clusterProfile=true
│     └─► status.accessProviders[].cluster.server
│           https://cluster-proxy-addon-user...:9092/cluster1
├── managed-serviceaccount  clusterProfile=true          ServiceAccount
│     └─► secret  cluster1-headlamp  ◄──── token ──────  headlamp
└── headlamp  (ns: headlamp)                             + read-only RBAC
      -enable-cluster-inventory                                 ▲
      exec /plugins/cp-creds --managed-serviceaccount=headlamp   │
            └──────── request via cluster-proxy ─────────────────┘
```

## Prerequisite

- Set up the dev environment following [setup dev environment](../setup-dev-environment).
- helm is installed.
- Add the ocm helm repo with `helm repo add ocm https://open-cluster-management.io/helm-charts`.
- A hub whose nodes support
  [image volumes](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/)
  — Kubernetes 1.33+ *and* containerd 2.1+ (or CRI-O 1.31+). Both halves matter: the
  `ImageVolume` feature gate is beta and on by default from 1.33, but containerd only
  gained the CRI implementation in 2.1, so a 1.33 node still running containerd 2.0 will
  fail to mount the plugin. On Kubernetes 1.31 or 1.32 the gate also has to be enabled
  explicitly. Headlamp runs on the hub and its chart has no other way to deliver the
  credentials plugin, so this is a hub-only requirement; the managed clusters can be any
  supported version. Verified here on a kind hub running Kubernetes 1.36.1 with
  containerd 2.3.1.

## Layout

```
manifests/
  clusterset.yaml             # namespace, ManagedClusterSet, ManagedClusterSetBinding
  managedserviceaccount.yaml  # ManagedServiceAccount per cluster (deploy.sh generates these)
  spoke-rbac/                 # read-only RBAC for the spoke SA, delivered via ManifestWork
headlamp-values.yaml          # Headlamp chart values: cluster inventory + the cp-creds provider
deploy.sh                     # all of the below, for every cluster in the set
cleanup.sh                    # tears it back down
```

## 1. Enable the ClusterProfile feature gate on the hub

The hub only maintains `ClusterProfile` objects when the alpha `ClusterProfile`
[feature gate](https://open-cluster-management.io/docs/getting-started/administration/featuregates/)
is on. On a hub you are creating from scratch:

```shell
clusteradm init --feature-gates=ClusterProfile=true
```

On the hub you already have, patch the `ClusterManager` instead of re-running `init`:

```shell
kubectl patch clustermanager cluster-manager --type=json \
  -p='[{"op":"add","path":"/spec/registrationConfiguration/featureGates/-","value":{"feature":"ClusterProfile","mode":"Enable"}}]'
```

The operator rolls the registration controller and installs the CRD. Give it a minute —
only the elected operator replica does the work, so nothing happens for a few seconds:

```shell
$ kubectl wait --for=condition=Established crd/clusterprofiles.multicluster.x-k8s.io --timeout=120s
customresourcedefinition.apiextensions.k8s.io/clusterprofiles.multicluster.x-k8s.io condition met
```

## 2. Choose which clusters Headlamp can see

`ClusterProfile` objects are not created for every managed cluster. The controller creates
them for clusters in a `ManagedClusterSet`, in the namespace of each
`ManagedClusterSetBinding` for that set. That namespace is the unit of isolation: a
different hub application binds a different set in its own namespace and sees only those
clusters.

```shell
kubectl apply -f manifests/clusterset.yaml
```

The profiles appear right away:

```shell
$ kubectl -n headlamp get clusterprofiles
NAME            AGE
cluster1        9s
cluster2        9s
local-cluster   9s
```

## 3. Install cluster-proxy and managed-serviceaccount

`cluster-proxy` gives the hub a route to each managed cluster's API server and writes it
into the profile. `managed-serviceaccount` creates a service account on each spoke and
rotates its token back to the hub.

```shell
helm install cluster-proxy ocm/cluster-proxy --version 0.12.0 \
    -n open-cluster-management-addon --create-namespace \
    --set userServer.enabled=true \
    --set enableServiceProxy=true \
    --set featureGates.clusterProfile=true

helm install managed-serviceaccount ocm/managed-serviceaccount --version 0.11.0 \
    -n open-cluster-management-addon --create-namespace \
    --set featureGates.clusterProfile=true \
    --take-ownership
```

`featureGates.clusterProfile` is what makes each addon participate in ClusterProfile; both
charts default it to `false` and helm will not warn you about a misspelt `--set` key, so a
typo here produces a fleet that installs cleanly and never populates `accessProviders`.

`--take-ownership` is needed because both charts ship a `ManagedClusterSetBinding` named
`global`; this is the same pre-existing chart overlap described in
[access-remote-api](../access-remote-api). Wait for the agents:

```shell
$ kubectl get managedclusteraddon -A
NAMESPACE       NAME                     AVAILABLE   DEGRADED   PROGRESSING
cluster1        cluster-proxy            True                   False
cluster1        managed-serviceaccount   True                   False
cluster2        cluster-proxy            True                   False
cluster2        managed-serviceaccount   True                   False
```

## 4. Create the service accounts and grant them read access

The `sync-to-clusterprofile` label is the part that matters: it tells the addon to copy the
token into the ClusterProfile namespace, where `cp-creds` will look for it.

```shell
kubectl apply -f manifests/managedserviceaccount.yaml
```

Then give each spoke service account read-only access, delivered as a `ManifestWork`:

```shell
clusteradm create work headlamp-rbac -f manifests/spoke-rbac --cluster cluster1
clusteradm create work headlamp-rbac -f manifests/spoke-rbac --cluster cluster2
```

The tokens land in the `headlamp` namespace as `<cluster>-headlamp`, and the profile picks
up its endpoint:

```shell
$ kubectl -n headlamp get secrets
NAME                     TYPE     DATA   AGE
cluster1-headlamp        Opaque   2      20s
cluster2-headlamp        Opaque   2      20s

$ kubectl -n headlamp get clusterprofile cluster1 -o jsonpath='{.status.accessProviders[0]}' | jq '{name, server: .cluster.server, extensions: .cluster.extensions}'
{
  "name": "open-cluster-management",
  "server": "https://cluster-proxy-addon-user.open-cluster-management-addon:9092/cluster1",
  "extensions": [
    {
      "extension": { "clusterName": "cluster1" },
      "name": "client.authentication.k8s.io/exec"
    }
  ]
}
```

If `accessProviders` stays empty, the feature gates in step 3 are the first thing to check.

## 5. Install Headlamp

```shell
helm repo add headlamp https://kubernetes-sigs.github.io/headlamp/
helm install headlamp headlamp/headlamp --version 0.45.0 \
    -n headlamp -f headlamp-values.yaml
```

Headlamp must run in the same namespace as the ClusterProfiles. `cp-creds` reads the token
secret from its own pod's namespace, so a Headlamp in `kube-system` — the namespace
Headlamp's own install guide uses — finds no credentials.

The log shows it picking up each profile:

```shell
$ kubectl -n headlamp logs deploy/headlamp | grep cluster-proxy-addon-user
"context":"cluster-inventory-in-cluster--headlamp--cluster1--5f12856f5fed","clusterURL":"https://cluster-proxy-addon-user.open-cluster-management-addon:9092/cluster1","message":"Proxy setup"
"context":"cluster-inventory-in-cluster--headlamp--cluster2--3b43efa03239","clusterURL":"https://cluster-proxy-addon-user.open-cluster-management-addon:9092/cluster2","message":"Proxy setup"
```

## 6. Browse the fleet

```shell
kubectl -n headlamp port-forward deploy/headlamp 8080:4466
kubectl -n headlamp create token headlamp
```

Open <http://127.0.0.1:8080>, sign in with the token, and the cluster chooser lists the hub
alongside every managed cluster. Each one is served through `cluster-proxy` with its own
rotating token.

The same thing from the command line, if you want to check it without a browser:

```shell
$ curl -s localhost:8080/config | jq -r '.clusters[] | "\(.name)  \(.server)"'
hub                                                        https://10.96.0.1:443
cluster-inventory-in-cluster--headlamp--cluster1--5f12...  https://cluster-proxy-addon-user.open-cluster-management-addon:9092/cluster1
cluster-inventory-in-cluster--headlamp--cluster2--3b43...  https://cluster-proxy-addon-user.open-cluster-management-addon:9092/cluster2
```

## Or just run the script

```shell
./deploy.sh <hub-context> [<cluster>...]
```

With no cluster names it resolves the `ManagedClusterSet`'s own selector, then waits until
every cluster the set selects has a `ClusterProfile` before going on. Profiles appear one
at a time, so stopping as soon as the list looks stable can leave a cluster in the chooser
without a `ManagedServiceAccount` behind it. If a profile never arrives the script names
the clusters it is still waiting for and exits non-zero rather than configuring a subset.

## Cleanup

```shell
./cleanup.sh <hub-context> [<cluster>...]
```

This keeps the `cluster-proxy` and `managed-serviceaccount` addons, since other
ClusterProfile consumers on the hub may be using them. Pass `REMOVE_ADDONS=true` to
uninstall those too.

## Notes

* Everything here runs from published, public, multi-arch (amd64/arm64) images and
  released charts — nothing is cloned, compiled or vendored into this repo.
  [Headlamp](https://github.com/kubernetes-sigs/headlamp) is a CNCF Sandbox project under
  `kubernetes-sigs`, and `cp-creds` is built and published by OCM's own
  [managed-serviceaccount](https://github.com/open-cluster-management-io/managed-serviceaccount)
  addon. The integration uses stock upstream OCM with no vendor-specific pieces.
* Keep the `cp-creds` image tag and the `managed-serviceaccount` chart version in lockstep.
  Both come from the same release of that repo — chart `0.11.0` pairs with
  `cp-creds:v0.11.0` — so bump them together.
* The `headlamp` namespace enforces the
  [restricted](https://kubernetes.io/docs/concepts/security/pod-security-standards/) Pod
  Security Standard, and `headlamp-values.yaml` sets the security context to match. The
  chart only fills in `allowPrivilegeEscalation`, `seccompProfile` and the capability drop
  when `securityContext` is left empty, and its own defaults set `runAsUser`/`runAsGroup`,
  so the stock install is rejected under `restricted` until you spell those out.
  `readOnlyRootFilesystem` is left off on purpose: Headlamp writes a kubeconfig for
  dynamically added clusters under `/home/headlamp/.config`, which the chart's automatic
  `/tmp` volume does not cover.
* The credentials plugin has to be delivered as an
  [image volume](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/).
  The Headlamp chart validates that the provider's `command` sits under one of
  `config.clusterInventory.plugins[].mountPath`, and those mount paths are always rendered
  as image volumes, so the initContainer-plus-`emptyDir` pattern that the OCM
  [ClusterProfile access providers](https://open-cluster-management.io/docs/scenarios/clusterprofile-access-providers/)
  guide shows for other consumers fails at template time here. That puts a floor of
  Kubernetes 1.33 and containerd 2.1 on the cluster running Headlamp — see the
  prerequisite above. The mount is a whole-image mount with no `subPath`, so the
  containerd 2.2 `subPath` support is not needed.
* Pin the plugin image by tag. Image volumes only accept a digest reference when the
  `ImageVolumeWithDigest` gate is on, and it is alpha and off by default.
* The provider name must be `open-cluster-management`. It is matched against the name OCM
  writes into `ClusterProfile.status.accessProviders`, not chosen freely.
* `--managed-serviceaccount=headlamp` in the values has to match the
  `ManagedServiceAccount` name on the hub. `cp-creds` builds the secret name as
  `<cluster>-<that value>`; get it wrong and every cluster fails to authenticate with a
  missing-secret error.
* The spoke RBAC is read-only on purpose: Headlamp renders the cluster but cannot change
  it, and `view` excludes secrets. Writes come back as
  `namespaces is forbidden: User "system:serviceaccount:open-cluster-management-agent-addon:headlamp" cannot create resource`.
  Swap in a broader role if you want Headlamp's editing and terminal features, or manage
  the RBAC declaratively with the
  [cluster-permission](https://github.com/open-cluster-management-io/cluster-permission)
  addon instead of a `ManifestWork`.
* The Headlamp chart binds its own service account to `cluster-admin` on the hub by
  default, which is what lets it read ClusterProfiles and the synced token secrets. Scope
  it down with `clusterRoleBinding.clusterRoleName` before using this outside a demo.
* The addons report `Available` before their proxy tunnels finish establishing, so the
  first requests after an install can come back as
  `502 proxy to anp-proxy-server failed because No agent available`. Give the spoke
  `cluster-proxy-proxy-agent` pods a minute to reach `Running`.
* Avoid uninstalling and reinstalling `cluster-proxy` repeatedly. Each cycle makes every
  managed cluster re-register its addon agent, and a few cycles in quick succession trip
  the spoke registration agent's CSR rate limit —
  `Stop creating csr since there are too many csr created already on hub` — which leaves
  the agent stuck in `ContainerCreating` waiting for a hub kubeconfig that never arrives.
  This is why `cleanup.sh` keeps the addons by default.
* Give every cluster in the set a `ManagedServiceAccount`. `cluster-proxy` writes an
  endpoint into the profile for all of them, so a cluster without one is still offered in
  the cluster chooser and then fails on use:

  ```
  HTTP 502
  [open-cluster-management] failed to get synced credential secret headlamp/local-cluster-headlamp: secrets "local-cluster-headlamp" not found
  ```

  `deploy.sh` covers every `ClusterProfile` in the namespace for this reason. Narrow the
  `ManagedClusterSet` selector, or label the profile `headlamp.dev/ignore`, to drop a
  cluster from the UI instead.
* `local-cluster` appears twice when the hub self-manages: once as Headlamp's in-cluster
  `hub` context and once as a managed cluster through the proxy. Label the profile with
  `headlamp.dev/ignore` to hide the duplicate.
* Cluster Inventory support is alpha on both sides — the OCM `ClusterProfile` gate and
  Headlamp's `clusterInventory` values — so expect the field names here to move.
