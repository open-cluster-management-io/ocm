# Work with cluster-api

[Cluster API](https://cluster-api.sigs.k8s.io/) is a Kubernetes sub-project focused on providing declarative APIs and
tooling to simplify provisioning, upgrading, and operating multiple Kubernetes clusters. This doc is a guideline on how
to use `cluster-api` and `open-cluster-management` project together

## Prerequisite
Download and install [clusteradm](https://github.com/open-cluster-management-io/clusteradm/releases). For Linux OS, run the following commands:

```shell
wget -qO- https://github.com/open-cluster-management-io/clusteradm/releases/latest/download/clusteradm_linux_amd64.tar.gz | sudo tar -xvz -C /usr/local/bin/

sudo chmod +x /usr/local/bin/clusteradm
```

Follow the instruction [here](https://cluster-api.sigs.k8s.io/user/quick-start.html) to install the `clusterctl`

## Initialize the cluster api management plane and create cluster

Before initilize the management plane, some feature gates should be enabled:

```shell
export CLUSTER_TOPOLOGY=true
export EXP_CLUSTER_RESOURCE_SET=true
```

Next initiate the cluster api management plane by following [this](https://cluster-api.sigs.k8s.io/user/quick-start.html#initialize-the-management-cluster)

Then you can create a cluster on any cloud using cluster-api. Suppose the name of the cluster created is `capi-cluster`

## Initialize the ocm hub and register the cluster created by cluster api

ocm hub can run in the same cluster of cluster api management plane

```
clusteradm init --use-bootstrap-token
```

then run

```
./capi-join.sh capi-cluster
```

