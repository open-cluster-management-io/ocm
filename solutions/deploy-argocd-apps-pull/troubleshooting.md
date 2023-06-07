# Troubleshooting

#### For ArgoCD components, check the following containers for logs:
* argocd-pull-integration-* in the `open-cluster-management` namespace (only on the hub cluster)
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

