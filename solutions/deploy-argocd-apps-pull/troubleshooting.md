# Troubleshooting

#### For ArgoCD components, check the following containers for logs:
* argocd-pull-integration-* in the `open-cluster-management` namespace (only on the hub cluster)
* argocd-applicationset-controller
* argocd-application-controller (only on managed clusters)

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
Despite the type `ErrorOccurred`, the status is `"False"`, which means the ApplicationSet has been reconciled successfully. If the status is `"True"`, check the error message. If needed, check the `openshift-gitops-applicationset-controller` pod logs in the `openshift-gitops` namespace.

## Known Issues
* ArgoCD application only supports Git and Helm Repo. Object Storage is not supported.
* Resources are only deployed on the managed cluster(s).
* If a resource failed to be deployed, it won’t be included in the Multicluster ApplicationSet Report
* In the pull model, the local-cluster is excluded as target managed cluster
* For large environments with over 1000 managed clusters, ArgoCD applicationSets are deployed to hundreds of managed clusters. It may take several minutes for the ArgoCD application to be created on the hub cluster
  * Workaround is to set the `requeueAfterSeconds` field to a higher value in the `clusterDecisionResource` generator of the ApplicationSet:
    ```
    apiVersion: argoproj.io/v1alpha1
    kind: ApplicationSet
    metadata:
    name: cm-allclusters-app-set
    namespace: openshift-gitops
    spec:
    generators:
    - clusterDecisionResource:
        configMapRef: ocm-placement-generator
        labelSelector:
            matchLabels:
            cluster.open-cluster-management.io/placement: app-placement
        requeueAfterSeconds: 3600 # 1 hour
    ```

