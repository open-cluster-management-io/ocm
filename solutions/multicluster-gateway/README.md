# Set up a Multi-Cluster Gateway on OCM

This guide will walk you through setting up a multi-cluster gateway with Open Cluster Management (OCM) using the [Envoy Gateway](https://gateway.envoyproxy.io/). This guide quickly boots three Kind clusters (hub, cluster1, and cluster2) on your local machine. Then, it installs the Gateway API CRDs and Envoy Gateway on the hub cluster. As a prerequisite, we employ [Submariner](https://submariner.io/) to create the multicluster environment, facilitating service export from the hub cluster to managed clusters.

![multicluster-gateway](multicluster-gateway.svg)

## Set up OCM Dev Environment

Set up the dev environment with three Kind clusters (hub, cluster1, and cluster2) in your local machine following [setup dev environment](../setup-dev-environment).

## Connect Clusters with Submariner

Deploy multicluster service API and Submariner for cross-cluster traffic (from hub to managed clusters) using ServiceImport.

Correct the kubeconfig master IP address before deploying Submariner:

```bash
export HUB_MASTER_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' hub-control-plane)
export CLUSTER1_MASTER_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' cluster1-control-plane)
export CLUSTER2_MASTER_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' cluster2-control-plane)
kubectl config set-cluster kind-hub --server=https://${HUB_MASTER_IP}:6443
kubectl config set-cluster kind-cluster1 --server=https://${CLUSTER1_MASTER_IP}:6443
kubectl config set-cluster kind-cluster2 --server=https://${CLUSTER2_MASTER_IP}:6443
```

Deploy Submariner with globalnet enabled:

```bash
subctl deploy-broker --context kind-hub --globalnet
subctl join --context kind-hub broker-info.subm --clusterid hub --natt=false
subctl join --context kind-cluster1 broker-info.subm --clusterid cluster1 --natt=false
subctl join --context kind-cluster2 broker-info.subm --clusterid cluster2 --natt=false
```

## Install Envoy Gateway on Hub Cluster

Install the Gateway API CRDs and Envoy Gateway in hub cluster:

```bash
helm install eg oci://docker.io/envoyproxy/gateway-helm --version v1.0.1 -n envoy-gateway-system --create-namespace --kube-context kind-hub
```

Wait for Envoy Gateway to become available:

```bash
kubectl wait --timeout=5m -n envoy-gateway-system deployment/envoy-gateway --for=condition=Available --context kind-hub
```

## Deploy Application from Hub:

Deploy and enable the application addon:

```bash
clusteradm install hub-addon --names application-manager --context kind-hub
clusteradm addon enable --names application-manager --clusters cluster1,cluster2 --context kind-hub
kubectl get managedclusteraddon --all-namespaces --context kind-hub
```

Deploy the nginx application to managed clusters:

```bash
clusteradm clusterset bind default --namespace default --context kind-hub
kubectl label managedcluster cluster1 purpose=test --overwrite --context kind-hub
kubectl label managedcluster cluster2 purpose=test --overwrite --context kind-hub
kubectl apply -f manifests/nginx-application --context kind-hub
```

Export the nginx application with subctl command:

```bash
export NGINX_BACKEND_SERVICE_CLUSTER1=$(oc get svc -n default -l app=nginx-ingress,component=default-backend -o jsonpath='{.items[0].metadata.name}' --context kind-cluster1)
export NGINX_BACKEND_SERVICE_CLUSTER2=$(oc get svc -n default -l app=nginx-ingress,component=default-backend -o jsonpath='{.items[0].metadata.name}' --context kind-cluster2)
subctl export service ${NGINX_BACKEND_SERVICE_CLUSTER1} -n default --context kind-cluster1
subctl export service ${NGINX_BACKEND_SERVICE_CLUSTER2} -n default --context kind-cluster2
```

## Create Gateway API Objects

Create the Gateway API objects GatewayClass, Gateway and HTTPRoute in hub cluster to set up the routing:

```bash
sed -i "s|nginx-ingress-1-default-backend|${NGINX_BACKEND_SERVICE_CLUSTER1}|g" manifests/gateway/httproute.yaml
sed -i "s|nginx-ingress-2-default-backend|${NGINX_BACKEND_SERVICE_CLUSTER2}|g" manifests/gateway/httproute.yaml
kubectl apply -f manifests/gateway --context kind-hub
```

## Verify the Multi-Cluster Gateway

Get the name of the Envoy service created the by the example Gateway:

```bash
export ENVOY_SERVICE=$(kubectl get svc -n envoy-gateway-system --selector=gateway.envoyproxy.io/owning-gateway-namespace=default,gateway.envoyproxy.io/owning-gateway-name=eg -o jsonpath='{.items[0].metadata.name}' --context kind-hub)
```

Port forward to the Envoy service:

```bash
kubectl --context kind-hub -n envoy-gateway-system port-forward service/${ENVOY_SERVICE} 8888:80 &
```

Curl the example nginx default backend through Envoy proxy:

```bash
curl --verbose --header "Host: www.example.com" http://localhost:8888/healthz
```
