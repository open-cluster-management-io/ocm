# Deploy manifests to a managed cluster

## Prerequisite

Set up the dev environment in your local machine following [setup dev environment](../setup-dev-environment).

## Run the clusteradm command to deploy an nginx deployment to a cluster

```
clusteradm create work nginx -f manifests/nginx.yaml --cluster cluster1
```

Now will see that nginx deployment is applied on cluster1. To update the existing nginx deployment on the cluster, run:

```
clusteradm create work nginx -f manifests/nginx.yaml --cluster cluster1 --overwrite
```

it will update the nginx deployment on the cluster1.
