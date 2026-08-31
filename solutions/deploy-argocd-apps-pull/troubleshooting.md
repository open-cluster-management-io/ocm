# Troubleshooting

#### For Argo CD components, check the following containers for logs:
* argocd-pull-integration-* in the `argocd` namespace (only on the hub cluster)
* argocd-applicationset-controller in the `argocd` namespace
* argocd-application-controller (only on managed clusters) in the `argocd` namespace

#### If the ApplicationSet contains the following status:
```
status:
    conditions:
    -   lastTransitionTime: "2023-03-21T11:25:06Z"
        message: Successfully generated parameters for all Applications
        reason: ApplicationSetUpToDate
        status: "False"
        type: ErrorOccurred
```
Despite the type `ErrorOccurred`, the status is `"False"`, which means the ApplicationSet has been reconciled successfully. If the status is `"True"`, check the error message. If needed, check the `argocd-applicationset-controller` pod logs in the `argocd` namespace.

#### `ImagePullBackOff` on `argocd-pull-integration` (Apple Silicon / arm64)

**Historical issue, fixed upstream — no workaround needed on current versions.** Earlier releases of the
`argocd-pull-integration` image were only published for `linux/amd64`, which caused `ImagePullBackOff` on
Apple Silicon / arm64 hosts. This was fixed in
[argocd-pull-integration#179](https://github.com/open-cluster-management-io/argocd-pull-integration/issues/179):
as of `argocd-pull-integration` v0.29.0 (released 2026-08-31), the image is published for both `linux/amd64`
and `linux/arm64`, and the `ocm/argocd-pull-integration` chart that `clusteradm install hub-addon --names argocd`
installs by default already pins that tag. Confirmed on a clean arm64 (Apple Silicon) host: the
`argocd-pull-integration` deployment pulls and runs successfully with no manual image build required.

If you still see `ImagePullBackOff` on `argocd-pull-integration` on an arm64 host, check which tag your
install actually pulled and whether that specific tag has a `linux/arm64` manifest published:
```shell
# find the tag your install actually pulled
kubectl get deployment argocd-pull-integration -n argocd -o jsonpath='{.spec.template.spec.containers[0].image}'

# check whether THAT tag has a linux/arm64 variant published
docker manifest inspect quay.io/open-cluster-management/argocd-pull-integration:<the-tag-from-above> | \
  jq -e '.manifests[]? | select(.platform.os=="linux" and .platform.architecture=="arm64")'
```
If it's older than v0.29.0, upgrade the addon (`helm upgrade` the `ocm/argocd-pull-integration` chart, or
reinstall via `clusteradm install hub-addon --names argocd` to pick up the current chart default) rather than
building a local workaround image.

#### `clusteradm install hub-addon --names argocd` fails with a `ClusterRoleBinding` ownership conflict

If the `argocd-agent` hub-addon was ever installed on this hub before (even if later
removed), installing this `argocd` (pull model) hub-addon can fail because both addons'
charts create a `ClusterRoleBinding` with the identical name
(`argocd-pull-integration-manager-rolebinding`), and Helm/server-side-apply refuses to let
one chart take over a resource owned by another. Delete the stale `ClusterRoleBinding`
left over from the previous install, or do a full clean uninstall of the other addon
first, before installing this one.
