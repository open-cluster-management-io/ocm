# Manage a spoke installed with multiple agents connected to different hubs

The scripts provided in this document help you set up an Open Cluster Management (OCM) environment with Kind clusters, where a single cluster is managed by two hubs via two agents installed on the managed cluster. The agents of the two hubs are all running in the default mode on the managed cluster under different namespaces.

## Prerequisite

- [kind](https://kind.sigs.k8s.io) must be installed on your local machine. The Kubernetes version must be >= 1.19, see [kind user guide](https://kind.sigs.k8s.io/docs/user/quick-start/#creating-a-cluster) for more details.

- Download and install [clusteradm](https://github.com/open-cluster-management-io/clusteradm/releases). For Linux OS, run the following commands:

    ```
    wget -qO- https://github.com/open-cluster-management-io/clusteradm/releases/latest/download/clusteradm_linux_amd64.tar.gz | sudo tar -xvz -C /usr/local/bin/

    sudo chmod +x /usr/local/bin/clusteradm
    ```

## Setup the first hub cluster

Run `./setup-hub1.sh`. It creates two Kind clusters:
- `hub1`. It is initialized as a OCM hub cluster.
- `cluster1`. It joins the `hub1` as a managed cluster.<br/>

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
klusterlet   58s
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
Run `./setup-hub2.sh` to create one more Kind cluster `hub2` and initialize it as the second OCM hub cluster. The script also joins the previously created Kind cluster `cluster1` to `hub2` as a managed cluster as well. Once it is done, you can list the managed clusters on `hub2` with command below.
```bash
kubectl get managedclusters
NAME       HUB ACCEPTED   MANAGED CLUSTER URLS                  JOINED   AVAILABLE   AGE
cluster1   true           https://cluster1-control-plane:6443   True     True        30s
```
The managed cluster `cluster1` joins `hub2` in the default mode and its Klusterlet resource `klusterlet-hub2` is created on the managed cluster as well.
```bash
kubectl get klusterlet
NAME              AGE
klusterlet        16m
klusterlet-hub2   1m
```
And the OCM agent of `hub2` is installed under `open-cluster-management-agent-hub2` on the managed cluster.
```bash
kubectl  get ns
NAME                                       STATUS   AGE
default                                    Active   19m
kube-node-lease                            Active   19m
kube-public                                Active   19m
kube-system                                Active   19m
local-path-storage                         Active   18m
open-cluster-management                    Active   18m
open-cluster-management-agent              Active   18m
open-cluster-management-agent-addon        Active   18m
open-cluster-management-agent-hub2         Active   105s
open-cluster-management-agent-hub2-addon   Active   105s
```
