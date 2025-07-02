# Set up Multikueue with OCM Kueue Admission Check Controller

This guide demonstrates how to use the external OCM [Kueue Admission Check Controller](https://kueue.sigs.k8s.io/docs/concepts/admission_check/) which integrates OCM `Placement` results with [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/) for intelligent multi-cluster job scheduling. 
The controller reads OCM `Placement` decisions and generates corresponding `MultiKueueConfig` and `MultiKueueCluster` resources, streamlining the setup of the [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/) environment and enabling users to select clusters based on custom criteria.
We'll walk through different user stories that showcase the power and flexibility of this integration.

## Background

### Existing Components

1. **OCM Placement and AddonPlacementScore**:

- `Placement` is used to dynamically select a set of `managedClusters` in one or multiple `ManagedClusterSet` to achieve Multi-Cluster scheduling.
- `AddOnPlacementScore` is an API introduced by `Placement` to support scheduling based on customized scores.

2. **Kueue MultiKueue and AdmissionChecks**:

- [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/) is a feature of Kueue for job dispatching across multiple clusters.
- The [AdmissionChecks](https://kueue.sigs.k8s.io/docs/concepts/admission_check/) are a mechanism which manages Kueue and allows it to consider additional criteria before admitting a workload. Kueue only proceeds with a workload if all associated AdmissionChecks return a positive signal.

REF: [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/), [Admission Check](https://kueue.sigs.k8s.io/docs/concepts/admission_check/), [Placement](https://open-cluster-management.io/concepts/placement/).

## Motivation

- Setting up a [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/) environment for multiple clusters is a complex and manual process, often requiring users to create `MultiKueueCluster` and `MultiKueueConfig` resources for each worker cluster individually.

- Driven by the growing need for optimal compute resource utilization, particularly in AI/ML workloads, multi-cluster users increasingly seek to leverage the OCM framework with [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/) for intelligent cluster selection.

REF: [Setup a MultiKueue environment](https://kueue.sigs.k8s.io/docs/tasks/manage/setup_multikueue/#multikueue-specific-kubeconfig)

## Prerequisites

1. A Kubernetes environment with OCM installed on a hub cluster and at least three managed clusters.
2. [Kueue](https://kueue.sigs.k8s.io/docs/installation/) deployed across all clusters.
3. [Managed-serviceaccount](https://github.com/open-cluster-management-io/managed-serviceaccount), [cluster-permission](https://github.com/open-cluster-management-io/cluster-permission) and [resource-usage-collect-addon](https://github.com/open-cluster-management-io/addon-contrib/tree/main/resource-usage-collect-addon) installed on managed clusters.

- You can set up these above by running the command:
```bash
./setup-env.sh
```
**Notice**: Currently, this functionality relies on the support of `ClusterProfile` and the user's manual installation of the Admission Check Controller. 
OCM achieves this by replacing some OCM images in this `setup-env.sh`. In the future, we plan to address the items listed in the [TODO section](#todo).

After that, you can verify your setup.

- Check the managed clusters.

```bash
kubectl get mcl
NAME            HUB ACCEPTED   MANAGED CLUSTER URLS                       JOINED   AVAILABLE   AGE
cluster1        true           https://cluster1-control-plane:6443        True     True        11m
cluster2        true           https://cluster2-control-plane:6443        True     True        10m
cluster3        true           https://cluster3-control-plane:6443        True     True        10m
local-cluster   true           https://local-cluster-control-plane:6443   True     True        11m
```
- Verify the installed addons.
```bash
kubectl get mca -A
NAMESPACE       NAME                     AVAILABLE   DEGRADED   PROGRESSING
cluster1        kueue-addon              True                   False
cluster1        managed-serviceaccount   True                   False
cluster1        resource-usage-collect   True                   False
cluster2        kueue-addon              True                   False
cluster2        managed-serviceaccount   True                   False
cluster2        resource-usage-collect   True                   False
cluster3        kueue-addon              True                   False
cluster3        managed-serviceaccount   True                   False
cluster3        resource-usage-collect   True                   False
local-cluster   kueue-addon              True                   False
local-cluster   managed-serviceaccount   True                   False
local-cluster   resource-usage-collect   True                   False
```
- Confirm Kueue is running on the clusters.
```bash
kubectl get pods -n kueue-system --context kind-local-cluster   # Same for managed clusters.
NAME                                       READY   STATUS    RESTARTS   AGE
kueue-controller-manager-87bd7888b-gqk4g   2/2     Running   0          69s
```

- On hub cluster, Check secrets with `kubeconfig` for the managed cluster created under `kueue-system` namespace.
```bash
kubectl get secret -n kueue-system
NAME                        TYPE     DATA   AGE
kueue-webhook-server-cert   Opaque   4      5m12s
multikueue-cluster1         Opaque   1      3m38s
multikueue-cluster2         Opaque   1      3m38s
multikueue-cluster3         Opaque   1      3m38s
multikueue-local-cluster    Opaque   1      3m38s
```

## User Stories

#### Story 1

As an admin, I want to automate [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/) configuration across multiple clusters, so that I can streamline the setup process without manual intervention.

- With the help of the `ClusterProfile` API, we can easily set up MultiKueue environment.
```bash
kubectl apply -f ./multikueue-setup-demo1.yaml
```
- After that, check the status of `MultiKueueCluster`, `AdmissionChecks` and `Clusterqueues`

```bash
kubectl get multikueuecluster -A -ojson | jq '.items[] | .metadata.name, .status.conditions'
kubectl get admissionchecks -ojson | jq '.items[] | .metadata.name, .status.conditions'
kubectl get clusterqueues -ojson | jq '.items[] | .metadata.name, .status.conditions'
```
Success is indicated when "status": "True" and reasons like "Active" or "Ready" are present in the conditions.

```bash
"multikueue-config-demo1-cluster1"
[
  {
    "lastTransitionTime": "2025-05-29T11:23:17Z",
    "message": "Connected",
    "observedGeneration": 1,
    "reason": "Active",
    "status": "True",
    "type": "Active"
  }
]
"multikueue-config-demo1-cluster2"
[
  {
    "lastTransitionTime": "2025-05-29T11:23:17Z",
    "message": "Connected",
    "observedGeneration": 1,
    "reason": "Active",
    "status": "True",
    "type": "Active"
  }
]
"multikueue-demo1"
[
  {
    "lastTransitionTime": "2025-05-29T11:23:17Z",
    "message": "The admission check is active",
    "observedGeneration": 1,
    "reason": "Active",
    "status": "True",
    "type": "Active"
  }
]
"cluster-queue"
[
  {
    "lastTransitionTime": "2025-05-29T11:23:17Z",
    "message": "Can admit new workloads",
    "observedGeneration": 1,
    "reason": "Ready",
    "status": "True",
    "type": "Active"
  }
]
```
- Deploy a job to the MultiKueue.

```bash
kubectl create -f ./job-demo1.yaml
```
- Check the workload on the managed clusters. Here when the job’s Workload receives a QuotaReservation in the manager cluster, a copy of the Workload is created in all configured worker clusters. 
Once `kind-cluster1` admitted the workload, the manager removed the corresponding workloads from the other clusters(`kind-cluster2`).
```bash
kubectl get workload --context kind-cluster1
NAME                       QUEUE              RESERVED IN           ADMITTED   AGE
job-demo1-jobnktc6-6c5f3   user-queue-demo1   cluster-queue-demo1   True       5s

kubectl get workload --context kind-cluster2
No resources found in default namespace.   # After cluster1 admitted the workload, no workload should show up here.
```

#### Story 2

As an admin, I want to use OCM `Placement` results for scheduling, so that clusters with specific attributes, like those with the `nvidia-t4` GPU accelerator label, are automatically selected and converted into a [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/) for targeted workload deployment.

- Cleanup the resource from demo1.
```bash
kubectl delete -f ./multikueue-setup-demo1.yaml
```

- You can manually label the accelerators on the clusters.
```bash
kubectl label managedcluster cluster2 accelerator=nvidia-tesla-t4
kubectl label managedcluster cluster3 accelerator=nvidia-tesla-t4
```

- You can manually edit the GPU resources on the managed clusters for testing, for example on `kind-cluster2`, set 3 fake GPU resources on the `control-plane-node`.
```bash
kubectl edit-status node cluster2-control-plane --context kind-cluster2  # Same operation with other clusters/nodes.
```
- Edit the `status` of the node `cluster2-control-plane`:
```yaml
 allocatable:
   cpu: "8"
   ephemeral-storage: 61202244Ki
   hugepages-1Gi: "0"
   hugepages-2Mi: "0"
   hugepages-32Mi: "0"
   hugepages-64Ki: "0"
   memory: 8027168Ki
   nvidia.com/gpu: "3"   # Add 3 fake GPUs in allocatable
   pods: "110"
 capacity:
   cpu: "8"
   ephemeral-storage: 61202244Ki
   hugepages-1Gi: "0"
   hugepages-2Mi: "0"
   hugepages-32Mi: "0"
   hugepages-64Ki: "0"
   memory: 8027168Ki
   nvidia.com/gpu: "3"  # Add 3 fake GPUs in capacity
   pods: "110"
```

The `placememt-demo2-1.yaml` selects clusters with the `nvidia-tesla-t4` accelerator label.

```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: multikueue-config-demo2
  namespace: kueue-system
spec:
  clusterSets:
  - global
  tolerations:
  - key: cluster.open-cluster-management.io/unreachable
    operator: Exists
  - key: cluster.open-cluster-management.io/unavailable
    operator: Exists
  predicates:
    - requiredClusterSelector:
        labelSelector:
          matchLabels:
            accelerator: nvidia-tesla-t4
```
- Bind the cluster set to the Kueue namespace and verify the bindings.

```bash
clusteradm clusterset bind global --namespace kueue-system
clusteradm get clustersets
<ManagedClusterSet> 
└── <default> 
│   ├── <Status> 4 ManagedClusters selected
│   ├── <Clusters> [cluster1 cluster2 cluster3 local-cluster]
│   ├── <BoundNamespace> 
└── <global> 
    └── <BoundNamespace> kueue-system,open-cluster-management-addon
    └── <Status> 4 ManagedClusters selected
    └── <Clusters> [cluster1 cluster2 cluster3 local-cluster]
```

- Apply the placement policy.

```bash
kubectl apply -f placement-demo2-1.yaml
```

- Apply the MultiKueue setup configuration.

```bash
kubectl apply -f ./multikueue-setup-demo2.yaml
```

- Check the `MultikueueKonfig` and `MultikueueClusters`.

```bash
kubectl get multikueueconfig
NAME                      AGE
multikueue-config-demo2   10s

kubectl get multikueuecluster
NAME                               AGE
multikueue-config-demo2-cluster2   19s
multikueue-config-demo2-cluster3   19s
```
- After that, check the status of `MultiKueueCluster`, `AdmissionChecks` and `Clusterqueues`
```bash
kubectl get multikueuecluster -A -ojson | jq '.items[] | .metadata.name, .status.conditions'
kubectl get admissionchecks -ojson | jq '.items[] | .metadata.name, .status.conditions'
kubectl get clusterqueues -ojson | jq '.items[] | .metadata.name, .status.conditions'
```
If success, there should be "status": "True" and reasons like "Active" or "Ready" presented in the conditions.
```bash
"multikueue-config-demo2-cluster2"
[
  {
    "lastTransitionTime": "2025-05-29T11:28:34Z",
    "message": "Connected",
    "observedGeneration": 1,
    "reason": "Active",
    "status": "True",
    "type": "Active"
  }
]
"multikueue-config-demo2-cluster3"
[
  {
    "lastTransitionTime": "2025-05-29T11:28:34Z",
    "message": "Connected",
    "observedGeneration": 1,
    "reason": "Active",
    "status": "True",
    "type": "Active"
  }
]
"multikueue-config-demo2" # The status of the admissioncheck `multikueue-config-demo2`
[
  {
    "lastTransitionTime": "2025-05-29T11:28:34Z",
    "message": "MultiKueueConfig multikueue-config-demo2 and MultiKueueClusters are generated successfully",
    "reason": "Active",
    "status": "True",
    "type": "Active"
  }
]
"multikueue-demo2" # The status of the admissioncheck `multikueue-demo2`
[
  {
    "lastTransitionTime": "2025-05-29T11:28:34Z",
    "message": "The admission check is active",
    "observedGeneration": 1,
    "reason": "Active",
    "status": "True",
    "type": "Active"
  }
]
"cluster-queue"
[
  {
    "lastTransitionTime": "2025-05-29T11:28:34Z",
    "message": "Can admit new workloads",
    "observedGeneration": 1,
    "reason": "Ready",
    "status": "True",
    "type": "Active"
  }
]
```
- Create a job requesting GPU resources to the MultiKueue.
```bash
kubectl create -f ./job-demo2.yaml
```
- Check the workload on managed clusters. Like we explained in the case in story 1, once one cluster(here `kind-cluster3`) has admitted the workload, the manager removed the corresponding workloads from the other clusters(here `kind-cluster2`).
```bash
kubectl get workload --context kind-cluster2
No resources found in default namespace.

kubectl get workload --context kind-cluster3
NAME                       QUEUE        RESERVED IN     ADMITTED   FINISHED   AGE
job-demo2-jobfpf8q-58705   user-queue   cluster-queue   True                  5m24s
```
#### Story 3

As an admin, I want to leverage OCM's `AddonPlacementScore` for dynamic workload scheduling, so that clusters with higher GPU scores, indicating clusters with more GPU resources, are selected and converted into a [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/), which automatically adjusts by adding or removing clusters as scores change.

The `placememt-demo2-2.yaml` selects clusters with the `nvidia-tesla-t4` accelerator label, and select one cluster with the highest GPU-score, indicating having more GPU resources.

```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: multikueue-config-demo2
  namespace: kueue-system
spec:
  clusterSets:
  - global
  tolerations:
  - key: cluster.open-cluster-management.io/unreachable
    operator: Exists
  - key: cluster.open-cluster-management.io/unavailable
    operator: Exists
  predicates:
    - requiredClusterSelector:
        labelSelector:
          matchLabels:
            accelerator: nvidia-tesla-t4
  numberOfClusters: 1
  prioritizerPolicy:
    mode: Exact
    configurations:
      - scoreCoordinate:
          type: AddOn
          addOn:
            resourceName: resource-usage-score
            scoreName: gpuClusterAvailable
        weight: 1

```

- Here in this environment, cluster1 has no GPUs, while cluster2 and cluster3 each have 3 GPUs.
Check `AddonPlacementScore`, the range of the score is from -100 to 100, clusters with more resources available have higher scores. 
Here cluster1, which has no GPUs, should have a score of -100, and the cluster running the workload(here from story 2 we have one workload running on `kind-cluster3`) will have a lower score.
```bash
kubectl get addonplacementscore -A -ojson | jq '.items[] | .metadata.name, .status.scores[5]'
"resource-usage-score"
{
  "name": "gpuClusterAvailable",
  "value": -100
}
"resource-usage-score"  # kind-cluster2 has no workload.
{
  "name": "gpuClusterAvailable",
  "value": -70
}
"resource-usage-score"  # kind-cluster3 has a workload from story 2, so it has fewer GPU available, thus lower score.
{
  "name": "gpuClusterAvailable",
  "value": -80
}
```

- Apply the changes in the `Placement` to update MultiKueue dynamically.
```bash
kubectl apply -f ./placement-demo2-2.yaml
```

- Review the update in `MultikueueKonfig`. 
```bash
kubectl get multikueueconfig multikueue-config-demo2  -oyaml
apiVersion: kueue.x-k8s.io/v1beta1
kind: MultiKueueConfig
metadata:
  creationTimestamp: "2025-05-29T11:28:34Z"
  generation: 7
  name: multikueue-config-demo2
  resourceVersion: "11913"
  uid: da363d4c-c0e8-43b4-a335-a52dc5a3cabf
spec:
  clusters:
  - multikueue-config-demo2-cluster2   # cluster2 has a higher GPU score, so it got selected by the placement decision.
```
- Create a job for the updated MultiKueue and check the workload, this time the workload is admitted by `kind-cluster2`, in `kind-cluster3` can only find the old workload from Story 2.
```bash
kubectl create -f ./job-demo2.yaml
kubectl get workload --context kind-cluster2
NAME                       QUEUE        RESERVED IN     ADMITTED   FINISHED   AGE
job-demo2-jobfxmh7-f4c34   user-queue   cluster-queue   True                  8s

kubectl get workload --context kind-cluster3
NAME                       QUEUE        RESERVED IN     ADMITTED   FINISHED   AGE
job-demo2-jobfpf8q-58705   user-queue   cluster-queue   True                  5m24s
```

## Design Details

### OCM Admission Check Controller

The OCM Admission Check Controller will integrate OCM `Placement` results into MultiKueue by reading `Placement` decisions and generating the necessary `MultiKueueConfig` and `MultiKueueCluster` resources.

- `controllerName`: Identifies the controller that processes the Admission Check, currently set to `open-cluster-management.io/placement`
- `parameters`: Identifies a configuration with additional parameters for the check, here we add the existing OCM `Placement` component. Clusters specified in the `Placement` will be bound to the `kueue-system` namespace.

Example OCM Admission Check Controller design:

```yaml
# OCM implements an admissioncheck controller to automate the MultiKueue setup process.
# MultiKueueConfigs and MultiKueueClusters are generated dynamically based on OCM placement decisions.
apiVersion: kueue.x-k8s.io/v1beta1
kind: AdmissionCheck
metadata:
  name: multikueue-config-demo2
spec:
  controllerName: open-cluster-management.io/placement
  parameters:
    apiGroup: cluster.open-cluster-management.io
    kind: Placement
    name: multikueue-config-demo2
# Leverages OCM's placement mechanism to select clusters based on specific criteria. 
# For example `multikueue-config-demo2` selects clusters with the `nvidia-tesla-t4` accelerator label.
```

### Changes in the Configuration Process with OCM Admission Check Controller

Using the OCM Admission Check Controller significantly simplifies the configuration process for system administrators by automating several manual tasks.

#### Before Using OCM Admission Check Controller

In the traditional setup, administrators must manually configure both `MultiKueueConfig` and `MultiKueueCluster` resources:

- **MultiKueueConfig**: Defines which clusters are part of the [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/) environment. Admins need to specify each cluster manually.
- **MultiKueueCluster**: Each cluster requires a `MultiKueueCluster` resource, which includes a kubeconfig secret that administrators must create manually for secure communication.

```yaml
apiVersion: kueue.x-k8s.io/v1beta1
kind: MultiKueueConfig
metadata:
  name:  multikueue-config-demo1
spec:
  clusters:
  - multikueue-config-demo1-cluster1
  - multikueue-config-demo1-cluster2
---
apiVersion: kueue.x-k8s.io/v1beta1
kind: MultiKueueCluster
metadata:
  name: multikueue-config-demo1-cluster1
spec:
  kubeConfig:
    locationType: Secret
    location: multikueue-cluster1
---
apiVersion: kueue.x-k8s.io/v1beta1
kind: MultiKueueCluster
metadata:
  name: multikueue-config-demo1-cluster2
spec:
  kubeConfig:
    locationType: Secret
    location: multikueue-cluster2
```

#### After Using OCM Admission Check Controller

With the OCM Admission Check Controller, the need for manual configuration of `MultiKueueConfig` and `MultiKueueCluster` is eliminated. Instead, the administrator only needs to configure two additional admission checks in the ClusterQueue resource:
`multikueue-demo2` and `multikueue-config-demo2 ` (see in `multikueue-setup-demo2.yaml`) which leverage OCM's placement mechanism to select clusters based on specific criteria and automate the process of setting up `MultiKueueConfig` and `MultiKueueCluster`.

```yaml
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: "cluster-queue"
spec:
  namespaceSelector: {} # match all.
  resourceGroups:
  - coveredResources: ["cpu", "memory","nvidia.com/gpu"]
    flavors:
    - name: "default-flavor"
      resources:
      - name: "cpu"
        nominalQuota: 9
      - name: "memory"
        nominalQuota: 36Gi
      - name: "nvidia.com/gpu"
        nominalQuota: 3
  admissionChecks:
  - multikueue-demo2
  - multikueue-config-demo2 
---
apiVersion: kueue.x-k8s.io/v1beta1
kind: AdmissionCheck
metadata:
  name: multikueue-demo2
spec:
  controllerName: kueue.x-k8s.io/multikueue
  parameters:
    apiGroup: kueue.x-k8s.io
    kind: MultiKueueConfig
    name: multikueue-config-demo2
---
apiVersion: kueue.x-k8s.io/v1beta1
kind: AdmissionCheck
metadata:
  name: multikueue-config-demo2
spec:
  controllerName: open-cluster-management.io/placement
  parameters:
    apiGroup: cluster.open-cluster-management.io
    kind: Placement
    name: multikueue-config-demo2
```

#### OCM Admission Check Controller Workflow

- The OCM Admission Check Controller retrieves the OCM `Placement` associated with an AdmissionCheck in the `kueue-system` namespace.
- It uses a `PlacementDecisionTracker` to gather the selected clusters and retrieves their credentials under `kueue-system` namespace.
- The controller creates or updates `MultiKueueCluster` resources with the kubeconfig details for each cluster, and then lists these clusters in a `MultiKueueConfig` resource.
- Finally, it updates the AdmissionCheck condition to true, indicating successful generation of the `MultiKueueConfig` and `MultiKueueCluster`, readying the [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/) environment for job scheduling.