# Deliver Helm Releases via the FluxCD Add-on

This enables [`fluxcd-addon`](https://github.com/kluster-manager/fluxcd-addon), an existing
OCM add-on that ships Flux (`source-controller`, `helm-controller`, `kustomize-controller`,
etc.) to managed clusters. Once it's enabled, you deliver `HelmRelease`/`HelmRepository`
objects to it via `ManifestWork`, and the add-on's controllers on the managed cluster do the
actual chart install locally. The hub only distributes objects.

## Prerequisite

Set up the dev environment following [setup dev environment](../setup-dev-environment).

## Layout

```
manifests/
  manager/    # fluxcd-addon manager: namespace, RBAC, Deployment, ClusterManagementAddOn (hub-side)
  release/    # HelmRepository + HelmRelease (podinfo demo chart, delivered via ManifestWork)
deploy.sh     # installs the add-on, enables it on a cluster, then ships the demo release
cleanup.sh    # tears everything down in the right order (see Cleanup below)
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

Check that Flux is running on the managed cluster:

```shell
kubectl --context <managed-cluster-context> -n flux-system get pods
```

## 3. Deliver a HelmRelease to the managed cluster

```shell
clusteradm create work flux-helmrelease-demo -f manifests/release --cluster cluster1
kubectl wait manifestwork/flux-helmrelease-demo -n cluster1 --for=condition=Available --timeout=180s
```

Check that the chart got reconciled and installed locally by the managed cluster's own Flux
controllers:

```shell
$ kubectl --context <managed-cluster-context> -n flux-system get helmrepository,helmrelease,pods
NAME                                              URL                                      AGE   READY   STATUS
helmrepository.source.toolkit.fluxcd.io/podinfo   https://stefanprodan.github.io/podinfo   16s   True    stored artifact: revision 'sha256:e7dc68a4...'

NAME                                         AGE   READY   STATUS
helmrelease.helm.toolkit.fluxcd.io/podinfo   16s   True    Helm install succeeded for release flux-system/podinfo.v1 with chart podinfo@6.15.0

NAME                                          READY   STATUS      RESTARTS   AGE
fluxcd-addon-flux-check-k694v                 0/1     Completed   0          105s
helm-controller-5cd495db64-n2wvv              1/1     Running     0          105s
image-automation-controller-996f6f87c-wlln8   1/1     Running     0          105s
image-reflector-controller-5697d54567-bw498   1/1     Running     0          105s
kustomize-controller-6c5cb76cd7-dblqg         1/1     Running     0          105s
notification-controller-5dff57c596-s7gc4      1/1     Running     0          105s
podinfo-6596c8f79c-nfjj8                      1/1     Running     0          14s
source-controller-68bbb7669d-tm89f            1/1     Running     0          105s
```

## Or just run the script

```shell
./deploy.sh <hub-context> <managed-cluster-name>
```

## Cleanup

Remove the release before disabling the add-on:

```shell
./cleanup.sh <hub-context> <managed-cluster-name>
```

`helm-controller` needs to be running to process the `HelmRelease` finalizer (it runs
`helm uninstall` first). If you disable the add-on before removing the release, that
finalizer gets orphaned and blocks the `HelmRelease`/`ManifestWork` from finishing deletion.

## Notes

* `fluxcd-addon` is built and published by [AppsCode](https://github.com/open-cluster-management-io/ocm/blob/main/ADOPTERS.md?plain=1#L12),
  an OCM adopter, at [kluster-manager/fluxcd-addon](https://github.com/kluster-manager/fluxcd-addon).
  This solution just enables their published image and CRD. The controller code itself isn't
  vendored into this repo.
* `manifests/manager/01-rbac.yaml` binds the manager's ServiceAccount to `cluster-admin` for
  simplicity, since the add-on manages `ClusterManagementAddOn`/`ManagedClusterAddOn`/
  `ManifestWork`/CSR objects across the hub. Scope this down before using it outside a demo
  or dev environment.
* The image tag in `manifests/manager/02-deployment.yaml` and the CRD URL in
  `deploy.sh`/`cleanup.sh` are both pinned to the same `fluxcd-addon` release. Bump them
  together.
* The demo `HelmRelease` installs into `flux-system` (already created by the add-on) instead
  of its own namespace. This avoids a race between namespace creation and `helm-controller`'s
  first reconcile inside a single `ManifestWork`. If you want a different target namespace,
  add it as its own object in an earlier `ManifestWork`, wait for it to become `Available`,
  then deliver the `HelmRelease` in a follow-up `ManifestWork`.
* The managed cluster's work-agent (`klusterlet-work-sa`) gets access to Flux's custom
  resources through the add-on's own aggregated `ClusterRole`s, so no extra RBAC is needed on
  top of what `fluxcd-addon` ships.
