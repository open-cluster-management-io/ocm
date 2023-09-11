# Set up a Multicluster Service Mesh on OCM

This scripts is to setup an multicluster service mesh on top of OCM. The guide will bootstrap 3 kind clusters(hub, cluster1, and cluster2) on your local machine and then deploy the [multicluster mesh addon](https://github.com/open-cluster-management-io/multicluster-mesh). After that, creating service meshes from the hub to the managed clusters and finally federating the service meshes so that microservices deployed into different managed clusters can be access each other.

## Prerequisite

Set up the dev environment in your local machine following [setup dev environment](../setup-dev-environment).

## Install Multicluster Service Mesh Addon on OCM

1. Install the multicluster-mesh addon with helm chart:

```bash
$ helm repo add ocm https://openclustermanagement.blob.core.windows.net/releases/
$ helm repo update
$ helm search repo ocm/multicluster-mesh
NAME                 	CHART VERSION	APP VERSION	DESCRIPTION
ocm/multicluster-mesh	0.0.1        	1.0.0      	A Helm chart for Multicluster Service Mesh OCM ...
$ helm install \
    -n open-cluster-management-addon --create-namespace \
    multicluster-mesh ocm/multicluster-mesh
```

2. You will see that all `managedclusteraddon` are available after waiting a while:

```bash
$ kubectl get managedclusteraddon --all-namespaces
NAMESPACE   NAME                  AVAILABLE   DEGRADED   PROGRESSING
cluster1    multicluster-mesh     True
cluster2    multicluster-mesh     True
```

## Deploy service meshes from Hub

1. Deploy the service meshes from Hub to managed clusters:

```bash
kubectl apply -f ./manifests/meshdeployment.yaml
```

2. You will see that the control plane for the service meshes are up and running in managed clusters after a while:

```bash
# kubectl config use-context kind-cluster1
Switched to context "kind-cluster1".
# kubectl -n istio-system get pod
NAME                                     READY   STATUS    RESTARTS   AGE
istio-ingressgateway-6f87f4f86c-gt4tr    1/1     Running   0          19s
istio-operator-1-16-7-858b59bdb8-zpj5g   1/1     Running   0          53s
istiod-1-16-7-67b8bf75f8-28lrl           1/1     Running   0          28s
# kubectl config use-context kind-cluster2
Switched to context "kind-cluster2".
# kubectl -n istio-system get pod
NAME                                     READY   STATUS    RESTARTS   AGE
istio-ingressgateway-6f87f4f86c-jrs9k    1/1     Running   0          21s
istio-operator-1-16-7-858b59bdb8-d9xgs   1/1     Running   0          53s
istiod-1-16-7-67b8bf75f8-qk4rd           1/1     Running   0          32s
```

## Federate Service Meshes from Hub

From the hub cluster, federate the serivce meshes created in last step by creating meshfederation resource:

```bash
kubectl config use-context kind-hub
kubectl apply -f ./manifests/meshfederation.yaml
```

## Verify Mesh Federtion with Bookinfo Application

1. Deploy part(productpage,details,reviews-v1,reviews-v2,ratings) of the bookinfo application in cluster1:

```bash
kubectl config use-context kind-cluster1
kubectl create ns bookinfo
kubectl label namespace bookinfo istio.io/rev=1-16-7
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.16/samples/bookinfo/platform/kube/bookinfo.yaml -l 'app,version notin (v3)'
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.16/samples/bookinfo/platform/kube/bookinfo.yaml -l 'account'
```

2. Deploy another part(reviews-v3, ratings) of bookinfo application in cluster2:

```bash
kubectl config use-context kind-cluster2
kubectl create ns bookinfo
kubectl label namespace bookinfo istio.io/rev=1-16-7
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.16/samples/bookinfo/platform/kube/bookinfo.yaml -l 'app,version in (v3)'
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.16/samples/bookinfo/platform/kube/bookinfo.yaml -l 'service=reviews'
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.16/samples/bookinfo/platform/kube/bookinfo.yaml -l 'account=reviews'
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.16/samples/bookinfo/platform/kube/bookinfo.yaml -l 'app=ratings'
kubectl apply -n bookinfo -f https://raw.githubusercontent.com/istio/istio/release-1.16/samples/bookinfo/platform/kube/bookinfo.yaml -l 'account=ratings'
```

3. Verify the microservices for the bookinfo application are up and running in cluster1 and cluster2:

```bash
# kubectl config use-context kind-cluster1
Switched to context "kind-cluster1".
# kubectl -n bookinfo get pod
NAME                              READY   STATUS        RESTARTS   AGE
details-v1-7f4669bdd9-6v4m2       2/2     Running       0          19s
productpage-v1-5586c4d4ff-nzn22   2/2     Running       0          19s
ratings-v1-6cf6bc7c85-zzxfj       2/2     Running       0          19s
reviews-v1-7598cc9867-pz2pj       2/2     Running       0          19s
reviews-v2-6bdd859457-bbkpq       2/2     Running       0          19s
# kubectl config use-context kind-cluster2
Switched to context "kind-cluster2".
# kubectl -n bookinfo get pod
NAME                          READY   STATUS        RESTARTS   AGE
ratings-v1-588b5477fc-mvgqz   2/2     Running       0          15s
reviews-v3-58cb55c99-dc594    2/2     Running       0          15s
```

4. Create the serviceentry in cluster2 to 'export' the remote service(reviews-v3):

```bash
kubectl config use-context kind-cluster2
export REVIEW_V3_IP=$(kubectl -n bookinfo get pod -l app=reviews -o jsonpath='{.items[0].status.podIP}')
cat ./manifests/serviceentry-export-cluster2.yaml | REVIEW_V3_IP=${REVIEW_V3_IP} envsubst | kubectl apply -f -
```

5. Create the serviceentry in cluster1 to to 'import' the remote service(reviews-v3):

```bash
kubectl config use-context kind-cluster2
export CLUSTER2_HOST_IP=$(docker inspect -f '{{range.NetworkSettings.Networks}}{{.IPAddress}}{{end}}' cluster2-control-plane)
export EASTWESTGW_NODEPORT=$(kubectl -n istio-system get svc istio-eastwestgateway -o jsonpath='{.spec.ports[?(@.name=="tls")].nodePort}')
kubectl config use-context kind-cluster1
cat ./manifests/serviceentry-import-cluster1.yaml | CLUSTER2_HOST_IP=${CLUSTER2_HOST_IP} EASTWESTGW_NODEPORT=${EASTWESTGW_NODEPORT} envsubst | kubectl apply -f -
```

6. Create the destinationrules and virtualservices for the cross-cluster traffic:

```bash
kubectl config use-context kind-cluster2
kubectl apply -f ./manifests/destinationrule-cluster2.yaml
kubectl config use-context kind-cluster1
kubectl apply -f ./manifests/virtualservice-cluster1.yaml
```

7. Forward port for the producppage service in cluster1 so that we can access it from browser:

```bash
kubectl config use-context kind-cluster1
kubectl -n bookinfo port-forward svc/productpage --address 0.0.0.0 9080:9080
```

Then access the bookinfo application with your browser via `http://localhost:9080/productpage/`. The expected result is that by refreshing the pruductpage several times, you should occasionally see traffic being routed to the `reviews-v3` service, which will produce red-colored stars on the product page, it means traffic from cluster1 be routed to cluster2.
