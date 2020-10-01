# Adding a New Addon to RedHat Advanced Cluster Management

For adding a new addon in Advanced Cluster Management, the addon controller on hub have to create a custome resource [ClusterManagementAddOn](https://github.com/open-cluster-management/addon-framework/blob/add-doc/api/v1alpha1/types_clustermanagementaddon.go).
This Resource is a cluster scoped and it helps ACM to identify which addon is installed on the hub and are available to install on a managed cluster.

Example of ClusterManagementAddOn CR

```shell
apiVersion: addon.open-cluster-management.io/v1alpha1
kind: ClusterManagementAddOn
metadata:
  name: application-manager
spec:
  addOnConfiguration:
    crName: ""
    crdName: klusterletaddonconfigs.agent.open-cluster-management.io
  addOnMeta:
    description: Processes events and other requests to managed resources.
    displayName: Application Manager
```

# Reporting addon status 

For status reporting of addon on hub, the addon controller have to create a custome resource [ManagedClusterAddOn](https://github.com/open-cluster-management/addon-framework/blob/add-doc/api/v1alpha1/types_managedclusteraddon.go).
This resource is a namespaced scoped and it provide interface for addon to report status.
ClusterManagementAddOn name and ManagedClusterAddOn name should be same.

Example of ManagedClusterAddOn CR

```shell
apiVersion: addon.open-cluster-management.io/v1alpha1
kind: ManagedClusterAddOn
metadata:
  name: application-manager
  namespace: local-cluster
spec: {}
status:
  addOnConfiguration:
    crName: local-cluster
    crdName: klusterletaddonconfigs.agent.open-cluster-management.io
  addOnMeta:
    description: Processes events and other requests to managed resources.
    displayName: Application Manager
  conditions:
  - lastTransitionTime: "2020-09-23T15:15:50Z"
    message: Addon is terminating
    reason: AddonTerminating
    status: "True"
    type: Progressing
  - lastTransitionTime: "2020-09-21T20:16:06Z"
    message: Addon is available
    reason: AddonAvailable
    status: "True"
    type: Available
  relatedObjects:
  - group: agent.open-cluster-management.io
    name: local-cluster
    resource: klusterletaddonconfigs
```


- Addon status conditions

    | Type | Description |
    | ------------ | ------- |
    | Progressing | when addon is installing/updating/teminating |
    | Available | when addon is installed and can connect to hub |
    | Degraded | when addon is not in desired state |


# Managed Cluster Addon availability check

This describes the mechanism on how hub cluster check the availability of addon running on managed cluster. The `Available` condition in status of ManagedClusterAddOn resource describes whether addon on managed cluster is available.

One way to update Addon status

- When the managed cluster is imported and available, the addons installation starts.

- After the addon is installed on the managed cluster, the addon agent on managed cluster creates [`Lease`](https://github.com/kubernetes/api/blob/master/coordination/v1/types.go#L27) on cluster namespace on hub. The addon agent will periodically (every 60sec) updates `RenewTime` of `Lease` to keep the addon available.

- The addon controller on hub watches the created `Lease` in cluster namespace and check whether `Lease` is constantly updated in a duration of 5min, if not the addon controller updates the status of this addon `Available` condition `unknown`.