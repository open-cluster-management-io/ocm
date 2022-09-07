# Working with the Cluster API project

[Cluster API](https://cluster-api.sigs.k8s.io/) is a Kubernetes sub-project focused on providing declarative APIs and
tooling to simplify provisioning, upgrading, and operating multiple Kubernetes clusters. This doc is a guideline on how
to use the Cluster API project and the [Open Cluster Management (OCM)](https://open-cluster-management.io/) project together.

## Prerequisite
Download and install [clusteradm](https://github.com/open-cluster-management-io/clusteradm/releases). For Linux OS, run the following commands:

```shell
wget -qO- https://github.com/open-cluster-management-io/clusteradm/releases/latest/download/clusteradm_linux_amd64.tar.gz | sudo tar -xvz -C /usr/local/bin/

sudo chmod +x /usr/local/bin/clusteradm
```

Follow the instruction [here](https://cluster-api.sigs.k8s.io/user/quick-start.html) to install the `clusterctl`.

## Initialize the Cluster API management plane and create a cluster

Before initilize the management plane, some feature gates should be enabled:

```shell
export CLUSTER_TOPOLOGY=true
export EXP_CLUSTER_RESOURCE_SET=true
```

Next initiate the Cluster API management plane by following [this](https://cluster-api.sigs.k8s.io/user/quick-start.html#initialize-the-management-cluster)

Now create a cluster on any cloud provider by following the instruction [here](https://cluster-api.sigs.k8s.io/user/quick-start.html#create-your-first-workload-cluster).

## Initialize the OCM multicluster control plane hub cluster and register the newly created cluster

The OCM multicluster control plane hub can be run in the same cluster as the Cluster API management plane. Use the following command to initialize the hub cluster: 

```
clusteradm init --use-bootstrap-token
```

Register the newly created cluster by running the following command:

```
./register-join.sh <cluster-name> # ./register-join.sh capi-cluster
```

The registration process might take a while. After it's done, you can run the following command to verify the newly created cluster has successfully joined the hub cluster:

```
kubectl get managedcluster
```
