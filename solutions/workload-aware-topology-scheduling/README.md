# Multicluster Topology-Aware Gang Scheduling

This solution combines OCM [Placement](https://open-cluster-management.io/docs/concepts/content-placement/placement/)
with Kubernetes v1.37 [Workload Aware Scheduling](https://kubernetes.io/docs/concepts/workloads/workload-api/)
(WAS), for [JobSet](https://jobset.sigs.k8s.io/) v0.13, plain `batch/v1` Jobs and
[LeaderWorkerSet](https://lws.sigs.k8s.io/) (LWS) v1.0. Each decision is made at the level that has the information for it:

| Layer | Decision | Mechanism |
|-------|----------|-----------|
| Hub (OCM) | *Which cluster* has the topology the workload needs | `Placement` that selects on ManagedCluster topology labels |
| Spoke (kube-scheduler) | *Which nodes*: every pod in one topology domain, all or nothing | `PodGroup` with a `gang` policy and a `schedulingConstraints.topology` key |
| Spoke (JobSet) | Turns a JobSet into a `Workload` plus `PodGroup`s | `spec.scheduling` (`JobSetWorkloadAwareSchedulingAPI` gate) |
| Spoke (Job controller) | Turns a plain `batch/v1` Job into a `Workload` plus `PodGroup` | `Job.spec.scheduling` (`WorkloadWithJob` gate) |
| Spoke (LWS) | Turns a LeaderWorkerSet into a `Workload` plus one gang `PodGroup` per replica | `spec.scheduling` (`kubernetes` scheduler provider + `WorkloadAwareScheduling` gate) |

A gang with a topology constraint has to go to a cluster whose nodes carry that topology label.
If it goes anywhere else its pods stay `Pending` forever.
The hub catches this up front: each spoke's node topology is published as ManagedCluster labels,
and the Placement only considers clusters that can satisfy the constraint.

```text
                         hub (was-hub)
   ManifestWorkReplicaSet ──► Placement "rack-topology"
   (JobSet w/ gang + rack)      requires: scheduling.example.io/workload-aware=true
                                          topology.example.io/rack exists
                 │
                 ▼ ManifestWork
   was-spoke1  (labels: topology.example.io/rack=2, topology.example.io/block=1)
     JobSet ─► Workload + PodGroup(gang minCount=4, topology key=topology.example.io/rack)
     kube-scheduler binds all 4 pods to the nodes of ONE rack

   was-spoke2  (labels: topology.kubernetes.io/zone=2)  ← not selected for rack gangs
```

## Environment

| Cluster | Kubernetes | Nodes | Node topology labels |
|---------|------------|-------|----------------------|
| `was-hub` | v1.37.0 | 1 | none (OCM hub only) |
| `was-spoke1` | v1.37.0 | 1 control plane + 4 workers | `topology.example.io/block=block-1`; `topology.example.io/rack=rack-a` on two workers and `rack-b` on two |
| `was-spoke2` | v1.37.0 | 1 control plane + 2 workers | `topology.kubernetes.io/zone=zone-a` / `zone-b` |

Each spoke runs with:

- Feature gates `GenericWorkload` (beta, off by default; it covers the Workload/PodGroup API and gang scheduling)
  and `TopologyAwareWorkloadScheduling` (alpha), on every component.
- Feature gate `WorkloadWithJob` (alpha, depends on `GenericWorkload`). It adds `spec.scheduling` to `batch/v1` Jobs,
  and the in-tree Job controller compiles it into a Workload and PodGroup that it owns.
  JobSet's child Jobs are left alone, because JobSet already sets `schedulingGroup` on their pod template.
- `--runtime-config=scheduling.k8s.io/v1beta1=true`. Workload and PodGroup are served at v1beta1, which is disabled by default.
- JobSet v0.13.1, installed from the Helm chart `oci://registry.k8s.io/jobset/charts/jobset`. The values set the controller
  feature gate `JobSetWorkloadAwareSchedulingAPI: true` (see [manifests/jobset-values.yaml](manifests/jobset-values.yaml)).
- LWS v1.0.0, installed from the Helm chart `oci://registry.k8s.io/lws/charts/lws`. The values set
  `gangSchedulingManagement.schedulerProvider: kubernetes` and the controller feature gate `WorkloadAwareScheduling: true`
  (see [manifests/lws-values.yaml](manifests/lws-values.yaml)).
- Aggregated ClusterRoles that let the OCM work agent apply JobSets and LeaderWorkerSets
  ([manifests/jobset-work-agent-rbac.yaml](manifests/jobset-work-agent-rbac.yaml),
  [manifests/lws-work-agent-rbac.yaml](manifests/lws-work-agent-rbac.yaml)).

No scheduler configuration is needed. With these gates on, kube-scheduler adds the
`GangScheduling`, `TopologyPlacementGenerator` and `PodGroupPodsCount` plugins to its default profile.

## Prerequisites

- [kind](https://kind.sigs.k8s.io/) v0.33+ (it needs to be able to run `kindest/node:v1.37.0`)
- `kubectl`, `jq` and `yq` (only for the demo commands)
- [clusteradm](https://github.com/open-cluster-management-io/clusteradm)
- [Helm](https://helm.sh/) v3.8+ (OCI chart support), for JobSet and LWS
- About 8 CPUs and 12 GB of memory for the 8 kind nodes

## Setup

```bash
./setup.sh
```

The script:

1. Creates the three kind clusters ([kind/spoke1.yaml](kind/spoke1.yaml), [kind/spoke2.yaml](kind/spoke2.yaml)).
2. Initializes the OCM hub with the `ManifestWorkReplicaSet` feature gate (without it the hub work controller is not deployed), joins both spokes, and accepts them.
3. Installs JobSet and LWS from their Helm charts, with WAS enabled, on each spoke.
4. **Publishes each spoke's topology to the hub.** For every key in `TOPOLOGY_KEYS`
   (`topology.example.io/block`, `topology.example.io/rack` and `topology.kubernetes.io/zone` by default) that appears on the spoke's nodes,
   it sets the ManagedCluster label `<key>=<number of domains>`. A spoke that serves `workloads.scheduling.k8s.io`
   also gets `scheduling.example.io/workload-aware=true`. A spoke whose apiserver keeps `Job.spec.scheduling` on a
   server-side dry run (it drops the field when `WorkloadWithJob` is off) gets `scheduling.example.io/job-workload=true`.
   A spoke whose LWS webhook accepts a gang-scheduled LeaderWorkerSet on a server-side dry run gets
   `scheduling.example.io/lws-gang=true`.
5. Adds both spokes to the `was-demo` ManagedClusterSet and binds it to the `was-demo` namespace.

You can override the cluster names, the node image and the JobSet version with `HUB`, `SPOKE1`, `SPOKE2`,
`KIND_NODE_IMAGE`, `JOBSET_VERSION` and `LWS_VERSION`.

```bash
kubectl --context kind-was-hub get managedclusters -L topology.example.io/rack,topology.example.io/block,topology.kubernetes.io/zone,scheduling.example.io/workload-aware,scheduling.example.io/job-workload,scheduling.example.io/lws-gang
```

```text
NAME         HUB ACCEPTED   ...   RACK   BLOCK   ZONE   WORKLOAD-AWARE   JOB-WORKLOAD   LWS-GANG
was-spoke1   true           ...   2      1              true             true           true
was-spoke2   true           ...                  2      true             true           true
```

Clusters created before the Job and LWS demos were added don't have `WorkloadWithJob` or LWS, and `setup.sh` reuses
existing clusters. Recreate them with `./cleanup.sh && ./setup.sh`.

## Guided demo

```bash
./demo.sh                # pauses before each step
DEMO_AUTO=1 ./demo.sh    # runs straight through
```

`demo.sh` runs `setup.sh` first if the clusters are missing. It resets the demo objects at the start, so it can be re-run.
It walks through the steps below, and only needs `kind`, `kubectl`, `clusteradm` and `helm`.

## Demo 1: rack-colocated gang goes to the rack-capable cluster

[manifests/placement-rack.yaml](manifests/placement-rack.yaml) requires a WAS-capable cluster with a rack topology domain:

```yaml
predicates:
- requiredClusterSelector:
    labelSelector:
      matchExpressions:
      - {key: scheduling.example.io/workload-aware, operator: In, values: ["true"]}
      - {key: topology.example.io/rack, operator: Exists}
```

[manifests/jobset-rack-gang.yaml](manifests/jobset-rack-gang.yaml) is a `ManifestWorkReplicaSet` bound to that Placement.
It carries a JobSet with 2 replicated Jobs × 2 pods, gang-scheduled as one group of 4 that must share a rack:

```yaml
scheduling:
  schedulingPolicy:
    gang: {}             # minCount defaults to replicas x parallelism = 4
  disruptionMode:
    all: {}
  schedulingConstraints:
    topology:
    - key: topology.example.io/rack
```

```bash
kubectl --context kind-was-hub apply -f manifests/placement-rack.yaml -f manifests/jobset-rack-gang.yaml
kubectl --context kind-was-hub -n was-demo get placementdecisions -l cluster.open-cluster-management.io/placement=rack-topology \
  -o jsonpath='{.items[*].status.decisions[*].clusterName}{"\n"}'
```

```text
was-spoke1
```

On the selected spoke, JobSet creates the Workload and a gang PodGroup with the rack constraint:

```bash
kubectl --context kind-was-spoke1 get workloads.scheduling.k8s.io,podgroups.scheduling.k8s.io
kubectl --context kind-was-spoke1 get podgroups.scheduling.k8s.io -o jsonpath='{.items[0].spec}' | jq
```

```text
NAME                                              AGE
workload.scheduling.k8s.io/rack-gang-d954b36a40   16s

NAME                                              POLICY   WORKLOAD               STATUS      AGE
podgroup.scheduling.k8s.io/rack-gang-d954b36a40   Gang     rack-gang-d954b36a40   Scheduled   16s

{
  "disruptionMode": {"all": {}},
  "schedulingConstraints": {"topology": [{"key": "topology.example.io/rack"}]},
  "schedulingPolicy": {"gang": {"minCount": 4}},
  "workloadRef": {"templateName": "rack-gang-d954b36a40", "workloadName": "rack-gang-d954b36a40"},
  ...
}
```

All four pods are placed together, on nodes of a single rack:

```bash
kubectl --context kind-was-spoke1 get pods -l jobset.sigs.k8s.io/jobset-name=rack-gang \
  -o custom-columns=POD:.metadata.name,NODE:.spec.nodeName,GROUP:.spec.schedulingGroup.podGroupName
kubectl --context kind-was-spoke1 get nodes -L topology.example.io/rack
```

```text
POD                          NODE                 GROUP
rack-gang-worker-0-0-zdjlp   was-spoke1-worker    rack-gang-d954b36a40
rack-gang-worker-0-1-7sv79   was-spoke1-worker    rack-gang-d954b36a40
rack-gang-worker-1-0-gnk9r   was-spoke1-worker2   rack-gang-d954b36a40
rack-gang-worker-1-1-6n9mc   was-spoke1-worker2   rack-gang-d954b36a40

NAME                       STATUS   ROLES           AGE   VERSION   RACK
was-spoke1-control-plane   Ready    control-plane   13m   v1.37.0
was-spoke1-worker          Ready    <none>          13m   v1.37.0   rack-a
was-spoke1-worker2         Ready    <none>          13m   v1.37.0   rack-a
was-spoke1-worker3         Ready    <none>          13m   v1.37.0   rack-b
was-spoke1-worker4         Ready    <none>          13m   v1.37.0   rack-b
```

The JobSet's progress comes back to the hub through ManifestWork status feedback:

```bash
kubectl --context kind-was-hub get manifestworkreplicaset -n was-demo
kubectl --context kind-was-hub get manifestwork -n was-spoke1 -o json | jq '.items[0].status.resourceStatus.manifests[0].statusFeedback.values'
```

The work agent polls feedback every 60s by default, so the values can take up to a minute to show up.

## Demo 2: zone-colocated gang goes to the zone-capable cluster

[manifests/placement-zone.yaml](manifests/placement-zone.yaml) requires `topology.kubernetes.io/zone` instead.
Only `was-spoke2` has it.

```bash
kubectl --context kind-was-hub apply -f manifests/placement-zone.yaml -f manifests/jobset-zone-gang.yaml
kubectl --context kind-was-hub -n was-demo get placementdecisions -l cluster.open-cluster-management.io/placement=zone-topology \
  -o jsonpath='{.items[*].status.decisions[*].clusterName}{"\n"}'
kubectl --context kind-was-spoke2 get pods -l jobset.sigs.k8s.io/jobset-name=zone-gang -o custom-columns=POD:.metadata.name,STATUS:.status.phase,NODE:.spec.nodeName
```

```text
was-spoke2

NAME                         STATUS    NODE
zone-gang-worker-0-0-5h8tl   Running   was-spoke2-worker2
zone-gang-worker-0-1-kxknr   Running   was-spoke2-worker2
```

## Demo 3: a plain batch/v1 Job as a rack-colocated gang

With `WorkloadWithJob` you don't need JobSet for a single-template gang. A Job has a `spec.scheduling` block
with the same building blocks. [manifests/job-rack-gang.yaml](manifests/job-rack-gang.yaml) delivers this Job:

```yaml
apiVersion: batch/v1
kind: Job
spec:
  parallelism: 4
  completions: 4
  completionMode: Indexed
  scheduling:
    schedulingPolicy:
      gang: {}             # minCount defaults to parallelism = 4; it may not exceed parallelism
    disruptionMode:
      all: {}
    schedulingConstraints:
      topology:
      - key: topology.example.io/rack
```

[manifests/placement-rack-job.yaml](manifests/placement-rack-job.yaml) also requires
`scheduling.example.io/job-workload=true`. On a spoke without the gate, the apiserver drops `spec.scheduling`
and the Job runs as ordinary, non-gang pods, with no error. The hub catches that case the same way it catches a missing topology.

```bash
kubectl --context kind-was-hub apply -f manifests/placement-rack-job.yaml -f manifests/job-rack-gang.yaml
kubectl --context kind-was-hub -n was-demo get placementdecisions -l cluster.open-cluster-management.io/placement=rack-job-topology \
  -o jsonpath='{.items[*].status.decisions[*].clusterName}{"\n"}'
```

```text
was-spoke1
```

On the spoke, the Job controller (not JobSet) owns the Workload and PodGroup. These objects carry no labels, so you
find the PodGroup through a pod's `spec.schedulingGroup`:

```bash
kubectl --context kind-was-spoke1 get workloads.scheduling.k8s.io \
  -o custom-columns=WORKLOAD:.metadata.name,OWNER-KIND:.metadata.ownerReferences[0].kind,OWNER:.metadata.ownerReferences[0].name
pg=$(kubectl --context kind-was-spoke1 get pods -l batch.kubernetes.io/job-name=rack-job -o jsonpath='{.items[0].spec.schedulingGroup.podGroupName}')
kubectl --context kind-was-spoke1 get podgroups.scheduling.k8s.io "$pg" \
  -o custom-columns=PODGROUP:.metadata.name,MINCOUNT:.spec.schedulingPolicy.gang.minCount,TOPOLOGY:.spec.schedulingConstraints.topology[*].key
kubectl --context kind-was-spoke1 get pods -l batch.kubernetes.io/job-name=rack-job \
  -o custom-columns=POD:.metadata.name,NODE:.spec.nodeName,GROUP:.spec.schedulingGroup.podGroupName
```

All four pods land on the two nodes of a single rack, as they do in Demo 1. The Job's `ready`, `active` and `succeeded`
counts come back to the hub through ManifestWork status feedback.

In v1.37, the Job controller only creates these objects for a *root* Job, meaning one with no controller ownerRef.
The OCM work agent adds an ownerRef to the AppliedManifestWork without `controller: true`, so a Job delivered by
ManifestWork counts as a root Job. Jobs created by a CronJob or by another operator do have a controller ownerRef.
Those Jobs are skipped unless their parent hands over PodGroup management through the
`scheduling.k8s.io/group-template-name` annotation, and the CronJob controller does not do that.

## Demo 4: a LeaderWorkerSet with one rack-colocated gang per replica

LWS v1.0 can gang-schedule on the upstream Workload/PodGroup API. It needs the `kubernetes` scheduler provider and
the `WorkloadAwareScheduling` gate, both set through the Helm values. Without both, the LWS webhook rejects any LeaderWorkerSet
that sets `spec.scheduling`, so the ManifestWork would fail to apply.
[manifests/placement-rack-lws.yaml](manifests/placement-rack-lws.yaml) requires `scheduling.example.io/lws-gang=true`.

[manifests/lws-rack-gang.yaml](manifests/lws-rack-gang.yaml) uses *replica* mode. Each replica (one leader and its
workers) becomes its own gang PodGroup, with `minCount` equal to `size`:

```yaml
apiVersion: leaderworkerset.x-k8s.io/v1
kind: LeaderWorkerSet
spec:
  replicas: 2
  scheduling:
    replica:
      schedulingPolicy:
        gang: {}
      disruptionMode:
        all: {}
      schedulingConstraints:
        topology:
        - key: topology.example.io/rack
  leaderWorkerTemplate:
    size: 3              # 1 leader + 2 workers per gang
```

```bash
kubectl --context kind-was-hub apply -f manifests/placement-rack-lws.yaml -f manifests/lws-rack-gang.yaml
kubectl --context kind-was-spoke1 get podgroups.scheduling.k8s.io -l leaderworkerset.sigs.k8s.io/name=rack-lws \
  -o custom-columns=PODGROUP:.metadata.name,MINCOUNT:.spec.schedulingPolicy.gang.minCount,TOPOLOGY:.spec.schedulingConstraints.topology[*].key
kubectl --context kind-was-spoke1 get pods -l leaderworkerset.sigs.k8s.io/name=rack-lws \
  -o 'custom-columns=POD:.metadata.name,REPLICA:.metadata.labels.leaderworkerset\.sigs\.k8s\.io/group-index,NODE:.spec.nodeName,GROUP:.spec.schedulingGroup.podGroupName'
```

You get two PodGroups with `minCount=3` and the rack constraint. Each replica's three pods land in one rack, and the two
replicas can use the same rack or different ones. The LeaderWorkerSet's `replicas` and `readyReplicas` come back to the
hub through ManifestWork status feedback.

Other LWS modes use the same `spec.scheduling` block:

- Top-level `schedulingPolicy`/`schedulingConstraints` gives one gang for the whole LWS (`replicas × size` pods).
- `replica.leader` / `replica.worker` gives separate leader and worker PodGroups per replica.

Only one of these levels can be set at a time. All of them create `scheduling.k8s.io/v1beta1` objects, so none of them need `CompositePodGroup`.

## Why the Placement matters: what happens without it

If the rack-constrained JobSet is sent straight to `was-spoke2`, which has no rack labels, the gang never schedules.
The PodGroup reports it as unschedulable and no pod is bound:

```bash
yq '.spec.manifestWorkTemplate.workload.manifests[0]' manifests/jobset-rack-gang.yaml | kubectl --context kind-was-spoke2 apply -f -
kubectl --context kind-was-spoke2 get pods -l jobset.sigs.k8s.io/jobset-name=rack-gang
kubectl --context kind-was-spoke2 get podgroups.scheduling.k8s.io -o jsonpath='{.items[0].status.conditions}' | jq
kubectl --context kind-was-spoke2 delete jobset rack-gang
```

```text
POD                          STATUS    NODE
rack-gang-worker-0-0-6mhbb   Pending   <none>
rack-gang-worker-0-1-p6979   Pending   <none>
rack-gang-worker-1-0-b7k8m   Pending   <none>
rack-gang-worker-1-1-kdvkj   Pending   <none>

[
  {
    "message": "no feasible placements found, pod group preemption: No preemption victims found for incoming preemptor",
    "reason": "Unschedulable",
    "status": "False",
    "type": "PodGroupInitiallyScheduled"
  }
]
```

## Adapting to real clusters

- **Topology keys.** Set `TOPOLOGY_KEYS` to the node labels your fabric uses, for example
  `cloud.google.com/gke-tpu-slice`, `topology.k8s.aws/network-node-layer-1` or `nvidia.com/gpu.clique`.
  Use the same key in the JobSet's `schedulingConstraints.topology`.
- **Keeping labels current.** `setup.sh` labels the clusters once. In production, publish the same data from the spoke
  as [ClusterClaims](https://open-cluster-management.io/docs/concepts/cluster-inventory/clusterclaim/)
  and select on it with the Placement's `claimSelector`, or report free capacity per domain as an
  [AddOnPlacementScore](https://open-cluster-management.io/docs/concepts/content-placement/placement/#extensible-scheduling)
  and prioritize on it.
- **Capacity.** Label selection only answers "does this cluster have the domain type?". It does not answer
  "does some domain have room for the whole gang?". A gang can still wait on a selected cluster whose domains are all full.
- **Multi-level constraints** (for example "one block, then one rack per sub-group") need the `CompositePodGroup` alpha gate and `scheduling.k8s.io/v1alpha3`.

## Cleanup

```bash
./cleanup.sh
```
