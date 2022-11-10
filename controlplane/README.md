# Get started 

## Option1: Deploy controlplane on Openshift Cluster

First set `API_HOST` environment variable:
```
$ export API_HOST=ocm-controlplane-<namespace>.<your ocp cluster to deploy>
```

Second run the deploy script to deploy:
```
$ cd controlplane
$ hack/deploy-ocm-controlplane.sh
```

Now start to use the controlplane:
```
$ clusteradm --kubeconfig=<file to kubeconfig> get token --use-bootstrap-token
```

--- 

## Option2: Run controlplane as a local binary

```
$ cd controlplane
$ make vendor
$ make build
$ make run
```
