# Setup dev environment by kind

This scripts is to setup an OCM developer environment in your local machine with 3 kind clusters. The script will bootstrap 3 kind clusters on your local machine: hub, cluster1, and cluster2. The hub is used as the control plane of the OCM, and the other two clusters are registered on the hub as the managed clusters.

## Prerequisite

[kind](https://kind.sigs.k8s.io) must be installed on your local machine. The Kubernetes version must be >= 1.19, see [kind user guide](https://kind.sigs.k8s.io/docs/user/quick-start/#creating-a-cluster) for more details.

Download and install [clusteradm](https://github.com/open-cluster-management-io/clusteradm/releases). For Linux OS, run the following commands:

```
wget -qO- https://github.com/open-cluster-management-io/clusteradm/releases/latest/download/clusteradm_linux_amd64.tar.gz | sudo tar -xvz -C /usr/local/bin/

sudo chmod +x /usr/local/bin/clusteradm
```

## Setup the clusters

Run `./local-up.sh`, you will see two clusters registered on the hub cluster.

```
NAME       HUB ACCEPTED   MANAGED CLUSTER URLS      JOINED   AVAILABLE   AGE
cluster1   true           https://127.0.0.1:45325   True     True        116s
cluster2   true           https://127.0.0.1:45325   True     True        98s
```
