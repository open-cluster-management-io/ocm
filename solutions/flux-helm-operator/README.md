# Deliver Helm Releases via the FluxCD Add-on

Enable [`fluxcd-addon`](https://github.com/kluster-manager/fluxcd-addon) — an
existing, production-used OCM add-on that ships Flux
(`source-controller`/`helm-controller`/`kustomize-controller`/etc.) to managed
clusters — then deliver `HelmRelease`/`HelmRepository` objects to it via
`ManifestWork`. The add-on's controllers running on the managed cluster do the
actual chart install locally; the hub only distributes objects.

This uses the add-on's published container image directly
(`ghcr.io/kluster-manager/fluxcd-addon`) — no cloning or compiling required.
Verified against upstream OCM (`open-cluster-management.io/{api,addon-framework}`,
no fork).

> Compared to [`deploy-a-helm-chart`](../deploy-a-helm-chart) (deprecated,
> depends on the archived `application-manager`/subscription controller) and
> the Argo CD solutions ([`deploy-argocd-apps`](../deploy-argocd-apps),
> [`deploy-argocd-apps-pull`](../deploy-argocd-apps-pull),
> [`argocd-agent`](../argocd-agent)), this solution has each managed cluster
> reconcile its own `HelmRelease` objects instead of the hub/Argo CD rendering
> and pushing the result.

## Prerequisite

Set up the dev environment following [setup dev environment](../setup-dev-environment).

## Layout

```
manifests/
  manager/    # fluxcd-addon manager: namespace, RBAC, Deployment, ClusterManagementAddOn (hub-side)
  release/    # HelmRepository + HelmRelease (podinfo demo chart, delivered via ManifestWork)
deploy.sh     # installs the add-on, enables it on a cluster, then ships the demo release
cleanup.sh    # tears down in the required order (see Cleanup below)
```

## 1. Install the fluxcd-addon manager on the hub

```shell
kubectl apply -f https://raw.githubusercontent.com/kluster-manager/fluxcd-addon/v0.0.10/crds/fluxcd.open-cluster-management.io_fluxcdconfigs.yaml
kubectl apply -f manifests/manager
kubectl -n fluxcd-addon rollout status deployment/fluxcd-addon-manager --timeout=120s
```

## 2. Enable the add-on on a managed cluster

```shell
clusteradm addon enable --names fluxcd-addon --namespace flux-system --clusters cluster1
kubectl wait managedclusteraddon/fluxcd-addon -n cluster1 --for=condition=Available --timeout=180s
```

Verify Flux is running on the managed cluster:

```shell
kubectl --context <managed-cluster-context> -n flux-system get pods
```

## 3. Deliver a HelmRelease to the managed cluster

```shell
clusteradm create work flux-helmrelease-demo -f manifests/release --cluster cluster1
kubectl wait manifestwork/flux-helmrelease-demo -n cluster1 --for=condition=Available --timeout=180s
```

Verify the chart was reconciled and installed **locally by the managed
cluster's own Flux controllers**:

```shell
kubectl --context <managed-cluster-context> -n flux-system get helmrepository,helmrelease,pods
```

## Or run all steps with the script

```shell
./deploy.sh <hub-context> <managed-cluster-name>
```

## Cleanup

Remove the release before disabling the add-on:

```shell
./cleanup.sh <hub-context> <managed-cluster-name>
```

* `helm-controller` needs to be running to process the `HelmRelease`
  finalizer (it runs `helm uninstall` first). Disabling the add-on before
  removing the release leaves that finalizer orphaned, which then blocks the
  `HelmRelease`/`ManifestWork` from finishing deletion.

## Notes

* `fluxcd-addon` is built and published by [AppsCode](https://github.com/open-cluster-management-io/ocm/blob/main/ADOPTERS.md?plain=1#L12)
  (an OCM adopter) at [kluster-manager/fluxcd-addon](https://github.com/kluster-manager/fluxcd-addon).
  This solution consumes their published image and CRD as-is — nothing here
  is vendored or forked.
* Its `go.mod` depends on the real, upstream `open-cluster-management.io/{api,addon-framework,sdk-go}`
  modules (no OCM fork), and it was validated end-to-end against a vanilla
  upstream hub for this solution.
* `manifests/manager/01-rbac.yaml` binds the manager's ServiceAccount to
  `cluster-admin` for simplicity, since the add-on manages
  `ClusterManagementAddOn`/`ManagedClusterAddOn`/`ManifestWork`/CSR objects
  across the hub. Scope this down before using outside a demo/dev environment.
* The image tag (`manifests/manager/02-deployment.yaml`) and the CRD URL
  (`deploy.sh`/`cleanup.sh`) are both pinned to the same `fluxcd-addon`
  release — bump them together.
* The demo `HelmRelease` installs into `flux-system` (already created by the
  add-on) instead of its own namespace, to avoid a race between namespace
  creation and `helm-controller`'s first reconcile within a single
  `ManifestWork`. To target a different namespace, add it as its own object
  in an earlier `ManifestWork`, confirm it's `Available`, then deliver the
  `HelmRelease` in a follow-up `ManifestWork`.
* The managed cluster's work-agent (`klusterlet-work-sa`) gets access to
  Flux's custom resources through the add-on's own aggregated `ClusterRole`s
  — no extra RBAC is needed on top of what `fluxcd-addon` ships.
