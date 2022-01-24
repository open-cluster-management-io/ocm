# Deploy cluster-proxy addon
 
## Prerequisite

Set up the dev environment in your local machine following [setup dev environment](../setup-dev-environment).

## (Optional) Support provisioning LoadBalancer typed service for kind

Run `./support.sh` to support provision loanblancer typed service for kind.

Wait for metallb pods to have a status of `Running`
```bash
$ kubectl get pods -n metallb-system --watch
```

Set up address pool for loadbalancers.
```bash
$ kubectl apply -f https://kind.sigs.k8s.io/examples/loadbalancer/metallb-configmap.yaml
```

## Deploy cluster-proxy addon manager by helm

*prerequest: [install helm](https://helm.sh/docs/intro/install/)*

Run `./deploy.sh` to deploy the cluster-proxy.

Check the pods created in hub cluster.
```bash
$ kubectl -n open-cluster-management-addon get pod
NAME                                           READY   STATUS        RESTARTS   AGE
cluster-proxy-5d8db7ddf4-265tm                 1/1     Running       0          12s
cluster-proxy-addon-manager-778f6d679f-9pndv   1/1     Running       0          33s
...

```

The addon will be automatically installed to your registered clusters, verify the addon installation:
```bash
$ kubectl get managedclusteraddon -A | grep cluster-proxy
NAMESPACE   NAME            AVAILABLE   DEGRADED   PROGRESSING
cluster1    cluster-proxy   True
```