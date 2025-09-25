# Kueue Integration with Open Cluster Management

## Overview

This solution demonstrates the integration of [Kueue's MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/) capabilities with [Open Cluster Management (OCM)](https://open-cluster-management.io/) to streamline the entire process by automating the generation of MultiKueue kubeconfig Secrets, centralizing queue resource management from a single hub, and enhancing multi‑cluster scheduling for more intelligent workload placement.

- **Simplified MultiKueue Setup**: Automates generation of MultiKueue specific Kubeconfig, streamlines configuration of MultiKueue resources, and eliminates manual secret management
- **Centralized Resource Management**: Manage spoke resources (ResourceFlavor, ClusterQueue, LocalQueue) from a single hub using template-based deployment
+- **Enhanced Multicluster Scheduling**: Integrates OCM Placement with MultiKueue via an AdmissionCheck controller, generates MultiKueueConfig dynamically based on Placement decisions, and supports advanced placement strategies
- **Flexible Installation Options**: Standard installation for existing Kueue setups, operator-based installation for OpenShift/OLM environments, and cluster proxy support for enhanced connectivity

**For comprehensive design documentation and technical workflows, refer to the [kueue-addon](https://github.com/open-cluster-management-io/addon-contrib/blob/main/kueue-addon/README.md).**

## Prerequisites

- Open Cluster Management (OCM) installed with the following addons:
  - [Cluster Permission Addon](https://github.com/open-cluster-management-io/cluster-permission)
  - [Managed Service Account Addon](https://github.com/open-cluster-management-io/managed-serviceaccount)
  - [Cluster Proxy Addon](https://github.com/open-cluster-management-io/cluster-proxy) (Optional) Enables hub-to-spoke connectivity for enhanced networking.
- Kueue installed:
  - Hub Cluster with [Kueue](https://kueue.sigs.k8s.io/docs/installation/) installed and MultiKueue enabled.
  - Spoke Clusters with [Kueue](https://kueue.sigs.k8s.io/docs/installation/) pre-installed, or let this addon install Kueue via [operator](https://github.com/openshift/kueue-operator) (OpenShift/OLM environments).

### Quick Setup

For automated environment setup with all prerequisites on Kind clusters, execute the following command (requires [clusteradm](https://github.com/open-cluster-management-io/clusteradm) to be pre-installed):

```bash
curl -sL https://raw.githubusercontent.com/open-cluster-management-io/addon-contrib/main/kueue-addon/build/setup-env.sh | bash
```

After setup completion, verify the environment configuration using the following commands:

- Check the managed clusters.

```bash
kubectl get mcl
NAME            HUB ACCEPTED   MANAGED CLUSTER URLS                       JOINED   AVAILABLE   AGE
cluster1        true           https://cluster1-control-plane:6443        True     True        3m55s
cluster2        true           https://cluster2-control-plane:6443        True     True        3m37s
cluster3        true           https://cluster3-control-plane:6443        True     True        3m24s
local-cluster   true           https://local-cluster-control-plane:6443   True     True        4m8s
```

- Verify the installed addons.

```bash
kubectl get mca -A
NAMESPACE       NAME                         AVAILABLE   DEGRADED   PROGRESSING
cluster1        cluster-proxy                True                   False
cluster1        managed-serviceaccount       True                   False
cluster1        multicluster-kueue-manager   True                   False
cluster1        resource-usage-collect       True                   False
[...additional managed clusters with same pattern...]
```

- Confirm Kueue is running on the clusters.

```bash
kubectl get pods -n kueue-system --context kind-local-cluster   # Same for managed clusters.
NAME                                        READY   STATUS    RESTARTS   AGE
kueue-controller-manager-6bf45486cb-hg8f7   1/1     Running   0          4m58s
```

- On the hub cluster, check `MultiKueueCluster` and kubeconfig `Secrets` created for each managed cluster.

```bash
kubectl get secret -n kueue-system
NAME                        TYPE     DATA   AGE
kueue-webhook-server-cert   Opaque   4      5m8s
multikueue-cluster1         Opaque   1      3m10s
multikueue-cluster2         Opaque   1      3m10s
multikueue-cluster3         Opaque   1      3m10s
multikueue-local-cluster    Opaque   1      3m10s
```

```bash
kubectl get multikueuecluster
NAME            CONNECTED   AGE
cluster1        True        14m
cluster2        True        14m
cluster3        True        14m
local-cluster   False       14m
```

> **Note**: OCM automatically generates MultiKueueCluster resources for all managed clusters. However, notice that the local-cluster shows a CONNECTED status of false. This is expected behavior because MultiKueue currently doesn't support submitting jobs to the management cluster. For technical details, see the [MultiKueue design documentation](https://github.com/kubernetes-sigs/kueue/tree/main/keps/693-multikueue).

## Usage Scenarios

### Scenario 1: Basic MultiKueue Setup

As an admin, I want to use the default `Placement` to setup [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/) to connect to all the spoke clusters.

#### Implementation

- With the `multikueueconfig` auto‑created, you can easily set up the MultiKueue environment.

```bash
kubectl apply -f ./multikueue-setup-demo1.yaml
```

#### Validation

- After that, check the status of the `MultiKueueConfig`, `AdmissionCheck`, and `ClusterQueue` resources.

```bash
kubectl get multikueueconfig -ojson | jq '.items[] | .metadata.name, .spec.clusters'
kubectl get admissionchecks -ojson | jq '.items[] | .metadata.name, .status.conditions'
kubectl get clusterqueues -ojson | jq '.items[] | .metadata.name, .status.conditions'
```

Success is indicated when "status": "True" and reasons like "Active" or "Ready" are present in the conditions.

```bash
"default"
[
  "cluster1",
  "cluster2",
  "cluster3"
]
"multikueue-config-demo1"
[
  {
    "lastTransitionTime": "2025-09-03T08:42:54Z",
    "message": "MultiKueueConfig default is generated successfully",
    "reason": "Active",
    "status": "True",
    "type": "Active"
  }
]
"multikueue-demo1"
[
  {
    "lastTransitionTime": "2025-09-03T08:42:54Z",
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
    "lastTransitionTime": "2025-09-03T08:42:54Z",
    "message": "Can admit new workloads",
    "observedGeneration": 1,
    "reason": "Ready",
    "status": "True",
    "type": "Active"
  }
]
```

#### Workload Deployment

- Deploy a job to the MultiKueue.

```bash
kubectl create -f ./job-demo1.yaml
```

- Check the workload on the managed clusters. Here, when the job's Workload receives a QuotaReservation in the manager cluster, a copy of the Workload is created in all configured worker clusters. Once `kind-cluster1` admits the workload, the manager removes the corresponding workloads from the other clusters (e.g., `kind-cluster2`).

```bash
kubectl get workload --context kind-cluster1
NAME                       QUEUE              RESERVED IN           ADMITTED   AGE
job-demo1-jobnktc6-6c5f3   user-queue-demo1   cluster-queue-demo1   True       5s

kubectl get workload --context kind-cluster2
No resources found in default namespace.   # After cluster1 admitted the workload, no workload should show up here.
```

### Scenario 2: Label-Based MultiKueue Setup

As an admin, I want to use OCM `Placement` results for scheduling, so that clusters with specific attributes, like those with the `nvidia-t4` GPU accelerator label, are automatically selected and converted into a [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/) for targeted workload deployment.

If your environment is set up by `setup-env.sh`, you will see cluster2 and cluster3 with the label `accelerator=nvidia-tesla-t4` and 3 fake GPU resources.

```bash
kubectl get mcl -l accelerator=nvidia-tesla-t4
NAME       HUB ACCEPTED   MANAGED CLUSTER URLS                  JOINED   AVAILABLE   AGE
cluster2   true           https://cluster2-control-plane:6443   True     True        37m
cluster3   true           https://cluster3-control-plane:6443   True     True        37m

kubectl get node -ojson --context kind-cluster2 | jq '.items[] | .status.capacity, .status.allocatable' | grep gpu
  "nvidia.com/gpu": "3",
  "nvidia.com/gpu": "3",
kubectl get node -ojson --context kind-cluster3 | jq '.items[] | .status.capacity, .status.allocatable' | grep gpu
  "nvidia.com/gpu": "3",
  "nvidia.com/gpu": "3",
```

#### Implementation

- Cleanup the resource from demo1.

```bash
kubectl delete -f ./multikueue-setup-demo1.yaml
```

- The `placement-demo2-1.yaml` selects clusters with the `nvidia-tesla-t4` accelerator label.
  Apply the placement.

```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: multikueue-config-demo2
  namespace: kueue-system
spec:
...
  predicates:
    - requiredClusterSelector:
        labelSelector:
          matchLabels:
            accelerator: nvidia-tesla-t4
```


```bash
kubectl apply -f placement-demo2-1.yaml
```

- Apply the MultiKueue setup configuration.

```bash
kubectl apply -f ./multikueue-setup-demo2.yaml
```

#### Validation

- After that, check the status of the `MultiKueueConfig`, `AdmissionCheck`, and `ClusterQueue` resources.

```bash
kubectl get multikueueconfig -ojson | jq '.items[] | .metadata.name, .spec.clusters'
kubectl get admissionchecks -ojson | jq '.items[] | .metadata.name, .status.conditions'
kubectl get clusterqueues -ojson | jq '.items[] | .metadata.name, .status.conditions'
```

If successful, conditions should show `"status": "True"` with reasons like `"Active"` or `"Ready"`.

```bash
"multikueue-config-demo2"
[
  "cluster2",
  "cluster3"
]
"multikueue-config-demo2"
[
  {
    "lastTransitionTime": "2025-09-03T08:51:34Z",
    "message": "MultiKueueConfig multikueue-config-demo2 is generated successfully",
    "reason": "Active",
    "status": "True",
    "type": "Active"
  }
]
"multikueue-demo2"
[
  {
    "lastTransitionTime": "2025-09-03T08:51:34Z",
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
    "lastTransitionTime": "2025-09-03T08:51:35Z",
    "message": "Can admit new workloads",
    "observedGeneration": 1,
    "reason": "Ready",
    "status": "True",
    "type": "Active"
  }
]
```

#### Workload Deployment

- Create a job requesting GPU resources to the MultiKueue.

```bash
kubectl create -f ./job-demo2.yaml
```

- Check the workload on managed clusters. As explained in Story 1, once one cluster (here `kind-cluster3`) has admitted the workload, the manager removes the corresponding workloads from the other clusters (here `kind-cluster2`).

```bash
kubectl get workload --context kind-cluster2
No resources found in default namespace.

kubectl get workload --context kind-cluster3
NAME                       QUEUE        RESERVED IN     ADMITTED   FINISHED   AGE
job-demo2-jobfpf8q-58705   user-queue   cluster-queue   True                  5m24s
```

### Scenario 3: Dynamic Score-Based MultiKueue Setup

As an admin, I want to leverage OCM's `AddonPlacementScore` for dynamic workload scheduling, so that clusters with higher GPU scores, indicating clusters with more GPU resources, are selected and converted into a [MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/), which automatically adjusts by adding or removing clusters as scores change.

Here in this environment, cluster1 has no GPUs, while cluster2 and cluster3 each have 3 GPUs. Check `AddonPlacementScore`—the score ranges from -100 to 100, with clusters having more resources available receiving higher scores. Here, cluster1, which has no GPUs, should have a score of -100, and the cluster running the workload (from Story 2, `kind-cluster3`) will have a lower score.

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

#### Implementation

- The `placement-demo2-2.yaml` selects clusters with the `nvidia-tesla-t4` accelerator label, and select one cluster with the highest GPU-score, indicating having more GPU resources. Apply the changes in the `Placement` to update MultiKueue dynamically.

```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: multikueue-config-demo2
  namespace: kueue-system
spec:
...
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

```bash
kubectl apply -f ./placement-demo2-2.yaml
```

#### Validation

- Review the update in `MultikueueConfig`.

```bash
kubectl get multikueueconfig -ojson | jq '.items[] | .metadata.name, .spec.clusters'
"multikueue-config-demo2"
[
  "cluster2" # cluster2 has a higher GPU score, so it got selected by the placement decision.
]   
```

#### Workload Deployment

- Create a job for the updated MultiKueue and check the workload, this time the workload is admitted by `kind-cluster2`. In `kind-cluster3`, you can only find the old workload from Story 2.

```bash
kubectl create -f ./job-demo2.yaml
kubectl get workload --context kind-cluster2
NAME                       QUEUE        RESERVED IN     ADMITTED   FINISHED   AGE
job-demo2-jobfxmh7-f4c34   user-queue   cluster-queue   True                  8s
```

```bash
kubectl get workload --context kind-cluster3
NAME                       QUEUE        RESERVED IN     ADMITTED   FINISHED   AGE
job-demo2-jobfpf8q-58705   user-queue   cluster-queue   True                  5m24s
```