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

**Symptom:** the `argocd-pull-integration` deployment in the hub's `argocd` namespace stays in
`ImagePullBackOff`, and `kubectl -n argocd describe pod -l app.kubernetes.io/name=argocd-pull-integration`
shows:
```text
Failed to pull image "quay.io/open-cluster-management/argocd-pull-integration:<tag>":
rpc error: code = NotFound desc = failed to pull and unpack image "...": no match for platform in manifest: not found
```

**Root cause:** this depends on the exact image tag `clusteradm install hub-addon --names argocd` pins in
your version of the chart, not on the solution itself — a future release may add `linux/arm64` support and
make this workaround unnecessary. Before doing anything else, find the tag actually being used and check it
yourself:
```shell
# find the tag your install actually pulled
kubectl get deployment argocd-pull-integration -n argocd -o jsonpath='{.spec.template.spec.containers[0].image}'

# check whether THAT tag has a linux/arm64 variant published (not just any arm64 platform)
docker manifest inspect quay.io/open-cluster-management/argocd-pull-integration:<the-tag-from-above> | \
  jq -e '.manifests[]? | select(.platform.os=="linux" and .platform.architecture=="arm64")'
# no jq? grep -B1 '"architecture": "arm64"' on the same output and check the line above it says "os": "linux"
```
If that doesn't return a match, the pod will `ImagePullBackOff` on Apple Silicon / arm64 hosts.
`argocd-pull-integration-controller` is the controller that watches for `Application`s carrying the
pull-model labels and wraps them into `ManifestWork`; without it running, no `Application` ever gets
delivered to any managed cluster, on any architecture where this image can't run.

**Workaround:** build a native `arm64` image from source and load it into your KinD nodes so `kubelet`'s
`IfNotPresent` pull policy uses the local image instead of pulling from the registry. Replace `<tag>` below
with the exact tag you found above (do **not** build from `main`, which can be ahead of the last release
and would then get mislabeled as that release):
```shell
git clone https://github.com/open-cluster-management-io/argocd-pull-integration.git
cd argocd-pull-integration
git checkout <tag>   # must be the same tag you checked above and use below
docker build --platform linux/arm64 -t quay.io/open-cluster-management/argocd-pull-integration:<tag> .

# Load into every KinD node that runs a copy of this image (hub, and any managed cluster
# using the argocd addon), matching the tag your addon chart expects:
kind load docker-image quay.io/open-cluster-management/argocd-pull-integration:<tag> --name <cluster-name>
```
Then restart the affected pod(s) so they pick up the locally-loaded image.

#### `clusteradm install hub-addon --names argocd` fails with a `ClusterRoleBinding` ownership conflict

If the `argocd-agent` hub-addon was ever installed on this hub before (even if later
removed), installing this `argocd` (pull model) hub-addon can fail because both addons'
charts create a `ClusterRoleBinding` with the identical name
(`argocd-pull-integration-manager-rolebinding`), and Helm/server-side-apply refuses to let
one chart take over a resource owned by another. Delete the stale `ClusterRoleBinding`
left over from the previous install, or do a full clean uninstall of the other addon
first, before installing this one.
