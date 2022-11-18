# Get started 

## Prerequisites
1. Connect to an OpenShift cluster
2. Export HOST_POSTFIX environment variable to the route domain of your OpenShift cluster.
3. Install the latest [clusteradm](https://github.com/open-cluster-management-io/clusteradm#install-the-clusteradm-command-line)
4. Install the latest [kustomize](https://kubectl.docs.kubernetes.io/installation/kustomize/binaries/)

## Install

```bash
    ./next-generation.sh 2 (the number of control planes)
```

## Clean up

```bash
    ./next-generation.sh 2 clean
```