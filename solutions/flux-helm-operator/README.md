# Deploy Helm Releases via a Flux Operator on Managed Clusters

Install the Flux Helm operator (`source-controller` + `helm-controller`) on a
managed cluster using `ManifestWork`, then deliver `HelmRelease`/`HelmRepository`
objects the same way. The operator running on the managed cluster does the
actual chart install locally — the hub only distributes objects.

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
  operator/   # flux-system namespace, Flux CRDs, RBAC, controllers, target namespace
  release/    # HelmRepository + HelmRelease (podinfo demo chart)
deploy.sh     # ships both ManifestWorks, in order, and waits for each
cleanup.sh    # tears down in the required order (see Cleanup below)
```

## 1. Install the operator on a managed cluster

```shell
clusteradm create work flux-helm-operator -f manifests/operator --cluster cluster1
kubectl wait manifestwork/flux-helm-operator -n cluster1 --for=condition=Available --timeout=120s
```

Verify the operator is running on the managed cluster:

```shell
kubectl --context <managed-cluster-context> -n flux-system get pods
```

## 2. Deliver a HelmRelease to the managed cluster

```shell
clusteradm create work flux-helmrelease-demo -f manifests/release --cluster cluster1
kubectl wait manifestwork/flux-helmrelease-demo -n cluster1 --for=condition=Available --timeout=120s
```

Verify the chart was reconciled and installed **locally by the managed
cluster's own operator**:

```shell
kubectl --context <managed-cluster-context> -n flux-system get helmrepository,helmrelease
kubectl --context <managed-cluster-context> -n podinfo get all
```

## Or run both steps with the script

```shell
./deploy.sh <hub-context> <managed-cluster-name>
```

## Cleanup

Delete the release before the operator, and let each finish before deleting
the next:

```shell
./cleanup.sh <hub-context> <managed-cluster-name>
```

* `helm-controller` needs to be running to process the `HelmRelease`
  finalizer (it runs `helm uninstall` first). Deleting both `ManifestWork`s
  together removes the controller before it can do that, and the finalizer
  is left orphaned, which then blocks the CRDs and the `ManifestWork` itself
  from finishing deletion.

## Notes

* `source-controller` requires all of its source CRDs
  (`GitRepository`/`Bucket`/`OCIRepository`/`HelmRepository`/`HelmChart`/`ExternalArtifact`)
  to be registered at startup, even if only `HelmRepository`/`HelmChart` are used.
* The managed cluster's work-agent (`klusterlet-work-sa`) does not get access
  to Flux's custom resources automatically, even with a broad `admin`
  ClusterRoleBinding. `manifests/operator/02-rbac.yaml` includes a `ClusterRole`
  labeled `open-cluster-management.io/aggregate-to-work: "true"` to grant it —
  see [ManifestWork: permission setting for work agent](https://open-cluster-management.io/docs/concepts/work-distribution/manifestwork/).
* The target namespace (`podinfo`) is created as part of the operator bundle,
  not the release bundle, so it already exists before `helm-controller`
  attempts the install.
