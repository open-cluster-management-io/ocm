# Manage a cluster with multiple agents in hosted mode

The scripts provided in this doc help you to setup an Open Cluster Management (OCM) environment with Kind clusters where a single cluster is managed by two hubs via two agents installed on the managed cluster. The agents of the second hub runs in the hosted mode on a hosting cluster.

## Prerequisite

- [kind](https://kind.sigs.k8s.io) must be installed on your local machine. The Kubernetes version must be >= 1.19, see [kind user guide](https://kind.sigs.k8s.io/docs/user/quick-start/#creating-a-cluster) for more details.

- Download and install [clusteradm](https://github.com/open-cluster-management-io/clusteradm/releases). For Linux OS, run the following commands:

    ```
    wget -qO- https://github.com/open-cluster-management-io/clusteradm/releases/latest/download/clusteradm_linux_amd64.tar.gz | sudo tar -xvz -C /usr/local/bin/

    sudo chmod +x /usr/local/bin/clusteradm
    ```
    Note: In order to run the scripts provided in this doc successfully, the clusteradm version must be > 0.7.1. You can also build it from the latest [source code](https://github.com/open-cluster-management-io/clusteradm) that contains the desired bug fixes.

## Setup the first hub cluster

Run `./setup-hub1.sh`. It creates two kind clusters:
- `hub1`. It is initialized as a OCM hub cluster.
- `cluster1`. It joins the `hub1` as a managed cluster in the default mode.<br/>

Once it is done, you can then list the managed clusters on `hub1` with command below.
```bash
kubectl get managedclusters
NAME       HUB ACCEPTED   MANAGED CLUSTER URLS                  JOINED   AVAILABLE   AGE
cluster1   true           https://cluster1-control-plane:6443                        29s
```
The managed cluster `cluster1` joins `hub1` in the default mode and its Klusterlet resource is created on the managed cluster.
```bash
kubectl get klusterlet
NAME         AGE
klusterlet   1m
```
And the OCM agent of `hub1` is installed under namespace `open-cluster-management-agent` on the managed cluster. Another namespace `open-cluster-management-agent-addon` is created too for add-ons which might be installed in future for this managed cluster.
```bash
kubectl  get ns
NAME                                  STATUS   AGE
default                               Active   7m34s
kube-node-lease                       Active   7m34s
kube-public                           Active   7m34s
kube-system                           Active   7m34s
local-path-storage                    Active   7m30s
open-cluster-management               Active   7m27s
open-cluster-management-agent         Active   7m27s
open-cluster-management-agent-addon   Active   7m11s
```
## Setup the second hub cluster
Run `./setup-hub2.sh`. It creates two more Kind clusters:
- `hub2`. It is initialized as the second OCM hub cluster.
- `hosting`. It is used as the hosting cluster on which the OCM agent of `hub2` is running.

The script also joins the previously created Kind cluster `cluster1` to `hub2` as a managed cluster in the hosted mode. Once it is done, you can list the managed clusters on `hub2` with command below.
```bash
kubectl get managedclusters
NAME       HUB ACCEPTED   MANAGED CLUSTER URLS                  JOINED   AVAILABLE   AGE
cluster1   true           https://cluster1-control-plane:6443   True     True        38s
```
Since the managed cluster `cluster1` joins in the hosted mode, its Klusterlet resource is created on the hosting cluster instead of the managed cluster.
```bash
kubectl get klusterlet
NAME                       AGE
klusterlet-hosted-cuz6ia   53s
```
The OCM agent of `hub2` is installed under namespace `klusterlet-hosted-cuz6ia` on the hosting cluster as well.
```bash
kubectl  get ns
NAME                       STATUS   AGE
default                    Active   77s
klusterlet-hosted-cuz6ia   Active   57s
kube-node-lease            Active   77s
kube-public                Active   77s
kube-system                Active   77s
local-path-storage         Active   72s
open-cluster-management    Active   57s
```
Some namespaces, like `open-cluster-management-klusterlet-hosted-cuz6ia` and `open-cluster-management-klusterlet-hosted-cuz6ia-addon`, are created on the managed cluster `cluster1` which are required by the OCM agent of `hub2` running in the hosted mode.
```bash
kubectl  get ns
NAME                                                     STATUS   AGE
default                                                  Active   4m25s
kube-node-lease                                          Active   4m25s
kube-public                                              Active   4m25s
kube-system                                              Active   4m25s
local-path-storage                                       Active   4m21s
open-cluster-management                                  Active   4m12s
open-cluster-management-agent                            Active   4m12s
open-cluster-management-agent-addon                      Active   4m3s
open-cluster-management-klusterlet-hosted-cuz6ia         Active   78s
open-cluster-management-klusterlet-hosted-cuz6ia-addon   Active   78s
```