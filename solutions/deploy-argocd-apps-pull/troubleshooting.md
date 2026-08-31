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

#### `clusteradm install hub-addon --names argocd` fails with a `ClusterRoleBinding` ownership conflict

If the `argocd-agent` hub-addon was ever installed on this hub before (even if later
removed), installing this `argocd` (pull model) hub-addon can fail because both addons'
charts create a `ClusterRoleBinding` with the identical name
(`argocd-pull-integration-manager-rolebinding`), and Helm/server-side-apply refuses to let
one chart take over a resource owned by another. Delete the stale `ClusterRoleBinding`
left over from the previous install, or do a full clean uninstall of the other addon
first, before installing this one.
