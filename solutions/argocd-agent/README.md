# OCM and Argo CD Agent Integration for Highly Scalable Application Deployment


## Table of Contents
- [Overview](#overview)
- [Benefits of Using the OCM Argo CD Agent AddOn](#benefits-of-using-the-ocm-argo-cd-agent-addon)
- [Prerequisites](#prerequisites)
- [Setup Guide](#setup-guide)
- [Deploying Applications](#deploying-applications)
- [Additional Resources](#additional-resources)


## Overview

[Open Cluster Management (OCM)](https://open-cluster-management.io/) is a robust, modular,
and extensible platform for orchestrating multiple Kubernetes clusters.
It features an addon framework that allows other projects to develop extensions for managing clusters in custom scenarios.

[Argo CD Agent](https://github.com/argoproj-labs/argocd-agent/) enables a scalable "hub and spokes" GitOps architecture
by offloading compute intensive parts of [Argo CD](https://argoproj.github.io/cd/) (application controller, repository server)
to workload/spoke/managed clusters while maintaining centralized/hub control and observability.

This guide provides instructions for setting up the Argo CD Agent environment within an OCM ecosystem,
leveraging [OCM Addons](https://open-cluster-management.io/docs/concepts/addon/) designed for Argo CD Agent to simplify deployment,
and automate lifecycle management of its components.
Once set up, it will also guide you through deploying applications using the configured environment.

![OCM with Argo CD Agent Architecture](./assets/argocd-agent-ocm-architecture.drawio.png)

## Benefits of Using the OCM Argo CD Agent AddOn

- **Centralized Deployment:** 
With OCM spoke clusters already registered to the OCM hub cluster
 the Argo CD Agent can be deployed to all workload/spoke/managed clusters from a centralized hub.
 Additionally, newly registered OCM spoke clusters will automatically receive the Argo CD Agent deployment,
 eliminating the need for manual deployment.

- **Centralized Lifecycle Management:**
Manage the entire lifecycle of Argo CD Agent instances from the hub cluster.
Easily revoke access to a compromised or malicious agent in a centralized location.

- **Advanced Placement and Rollout:**
Leverage the OCM [Placement API](https://open-cluster-management.io/docs/concepts/placement/)
for advanced placement strategies and controlled rollout of the Argo CD Agent to spoke clusters.

- **Fleet-wide Health Visibility:**
Gain centralized health insights and status views of all Argo CD Agent instances across the entire cluster fleet.

- **Simplified Maintenance:**
Streamline the lifecycle management, upgrades,
and rollbacks of the Argo CD Agent and its components across multiple spoke clusters.

- **Secure Communication:**
The AddOn [Custom Signer](https://open-cluster-management.io/docs/concepts/addon/#custom-signers)
registration type ensures that the Argo CD Agent agent's
client certificates on spoke clusters are automatically signed, enabling secure authentication.
This supports mTLS connections between the agents on spoke clusters and the hub's Argo CD Agent principal component.
Additionally, the AddOn framework handles automatic certificate rotation,
ensuring connections remain secure and free from expiration related disruptions.

- **Flexible Configuration:**
Easily customize the Argo CD Agent deployment using the OCM
[AddOnTemplate](https://open-cluster-management.io/docs/developer-guides/addon/#build-an-addon-with-addon-template).
This eliminates the need for additional coding or maintaining binary compilation pipelines,
enabling efficient templating for deployment modifications.


## Prerequisites

- Setup an OCM environment with at least two clusters (one hub and at least one managed).
Refer to the [Quick Start guide](https://open-cluster-management.io/docs/getting-started/quick-start/) for more details.

- The Hub cluster must have a load balancer.
Refer to the [Additional Resources](#additional-resources) for more details.

- Generate the necessary cryptographic keys and certificates (CA, TLS, and JWT)
to secure communication and authentication between the Argo CD Agent components (hub principal and spoke agents).
Refer to the [Additional Resources](#additional-resources) for more details.

- [Helm CLI](https://helm.sh/).


## Setup Guide

### Deploy Argo CD on the Hub Cluster

Deploy an Argo CD instance on the hub cluster,
excluding compute intensive components like the application controller.

```shell
# kubectl config use-context <hub-cluster>
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml

kubectl scale -n argocd statefulset argocd-application-controller --replicas=0
```

See the
[Argo CD website](https://argo-cd.readthedocs.io/en/stable/getting_started/#1-install-argo-cd)
for more details.

Validate that the Argo CD pods are running:

```shell
kubectl -n argocd get pod

NAME                                                READY   STATUS    RESTARTS   AGE
argocd-applicationset-controller-5985fcc8f9-99qkh   1/1     Running   0          31s
argocd-dex-server-58f697b95f-xx7ld                  1/1     Running   0          31s
argocd-redis-66d85c4b6d-hmcdg                       1/1     Running   0          31s
argocd-repo-server-7fcd864f4c-vpfst                 1/1     Running   0          31s
argocd-server-85db89dd5-qbgsm                       1/1     Running   0          31s
```

This may take a few minutes to complete.

### Deploy OCM Argo CD AddOn on the Hub Cluster

Clone the `addon-contrib` repo:

```shell
git clone git@github.com:open-cluster-management-io/addon-contrib.git
cd addon-contrib/argocd-agent-addon
```

Deploy the OCM Argo CD AddOn on the hub cluster.
This will deploy opinionated Argo CD instances to all managed clusters,
including compute intensive components like the application controller.

```shell
# kubectl config use-context <hub-cluster>
helm -n argocd install argocd-addon charts/argocd-addon
```

Validate that the Argo CD AddOn is successfully deployed and available:

```shell
kubectl get managedclusteraddon --all-namespaces

NAMESPACE   NAME           AVAILABLE   DEGRADED   PROGRESSING
cluster1    argocd         True                   False
```

This may take a few minutes to complete.

### Deploy OCM Argo CD Agent AddOn on the Hub Cluster

To deploy the OCM Argo CD Agent AddOn on the hub cluster, follow the steps below. This process deploys:
- The **Argo CD Agent principal component** on the hub cluster.
- The **Argo CD Agent agent component** on all managed clusters.

Run the following `helm` command:

```shell
helm -n argocd install argocd-agent-addon charts/argocd-agent-addon \
  --set-file agent.secrets.cacrt=/tmp/ca.crt \
  --set-file agent.secrets.cakey=/tmp/ca.key \
  --set-file agent.secrets.tlscrt=/tmp/tls.crt \
  --set-file agent.secrets.tlskey=/tmp/tls.key \
  --set-file agent.secrets.jwtkey=/tmp/jwt.key \
  --set agent.principal.server.address="172.18.255.200" \
  --set agent.mode="managed" # or "autonomous" for autonomous mode
```

Validate that the Argo CD Agent principal pod is running:

```shell
kubectl -n argocd get pod

NAME                                                       READY   STATUS    RESTARTS   AGE
argocd-agent-principal-5c47c7c6d5-mpts4                    1/1     Running   0          88s
```

Validate that the Argo CD Agent Addon is successfully deployed and available:

```shell
kubectl get managedclusteraddon --all-namespaces

NAMESPACE   NAME           AVAILABLE   DEGRADED   PROGRESSING
cluster1    argocd         True                   False
cluster1    argocd-agent   True                   False
```

This may take a few minutes to complete.

**Notes:** 

1. Refer to the [Additional Resources](#additional-resources)
section for examples on generating the necessary cryptographic keys and certificates.

2. The `agent.principal.server.address` value must correspond to the external IP of the `argocd-agent-principal` service.
Use the following command to retrieve it:

```shell
kubectl -n argocd get svc argocd-agent-principal

Example output:
NAME                     TYPE           CLUSTER-IP      EXTERNAL-IP      PORT(S)         AGE
argocd-agent-principal   LoadBalancer   10.96.149.226   172.18.255.200   443:32104/TCP   37h
```   

3. For details on operational modes and guidance on selecting the appropriate `agent.mode` (e.g., `managed` or `autonomous`),
refer to the [Argo CD Agent website](https://argocd-agent.readthedocs.io/latest/concepts/agent-modes/).

## Deploying Applications

### Managed Mode

Refer to the [Argo CD Agent website](https://argocd-agent.readthedocs.io/latest/concepts/agent-modes/)
for more details about the `managed` mode.

To deploy an Argo CD Application in `managed` mode using the Argo CD Agent,
create the application on the `hub` cluster:

```shell
# kubectl config use-context <hub-cluster>
kubectl apply -f - <<EOF
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: guestbook
  namespace: cluster1 # replace with your managed cluster name
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: guestbook
  syncPolicy:
    syncOptions:
    - CreateNamespace=true
    automated:
      prune: true
EOF
```

Validate that the Argo CD Application has been successfully propagated to the managed cluster:

```shell
# kubectl config use-context <managed-cluster>
kubectl -n argocd get app

NAME        SYNC STATUS   HEALTH STATUS
guestbook   Synced        Healthy
```

Validate that the application has been successfully synchronized back to the hub cluster:

```shell
# kubectl config use-context <hub-cluster>
kubectl -n cluster1 get app

NAME        SYNC STATUS   HEALTH STATUS
guestbook   Synced        Healthy
```

### Autonomous Mode

Refer to the [Argo CD Agent website](https://argocd-agent.readthedocs.io/latest/concepts/agent-modes/)
for more details about the `autonomous` mode.

To deploy an Argo CD Application in `autonomous` mode using the Argo CD Agent,
create the application on the `managed` cluster:

```shell
# kubectl config use-context <managed-cluster>
kubectl apply -f - <<EOF
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: guestbook
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: guestbook
  syncPolicy:
    syncOptions:
    - CreateNamespace=true
    automated:
      prune: true
EOF
```

Validate that the application has been successfully synchronized back to the hub cluster:

```shell
# kubectl config use-context <hub-cluster>
kubectl -n cluster1 get app

NAME        SYNC STATUS   HEALTH STATUS
guestbook   Synced        Healthy
```


## Additional Resources

### Deploy MetalLB on a KinD Cluster

Run the following commands to install MetalLB on a KinD cluster:

```shell
# kubectl config use-context <hub-cluster>
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/main/config/manifests/metallb-native.yaml
kubectl wait --namespace metallb-system \
  --for=condition=Ready pods \
  --selector=app=metallb \
  --timeout=120s
cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: kind-address-pool
  namespace: metallb-system
spec:
  addresses:
  - 172.18.255.200-172.18.255.250 # Replace with the IP range of your choice
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: kind-l2-advertisement
  namespace: metallb-system
spec:
  ipAddressPools:
  - kind-address-pool
EOF
```

### Generate Keys and Certificates (CA, TLS, and JWT)

Run the following commands to generate the necessary cryptographic keys and certificates (CA, TLS, and JWT):

```shell
openssl genrsa -out /tmp/jwt.key 2048
openssl genpkey -algorithm RSA -out /tmp/ca.key
openssl req -new -x509 -key /tmp/ca.key -out /tmp/ca.crt -days 365 -subj "/C=/ST=/L=/O=/OU=/CN=CA"
openssl genpkey -algorithm RSA -out /tmp/tls.key
openssl req -new -key /tmp/tls.key -out /tmp/tls.csr -subj "/C=/ST=/L=/O=/OU=/CN=principal"
cat <<EOF > /tmp/openssl_ext.cnf
[ req ]
distinguished_name = req_distinguished_name
x509_extensions = v3_req
prompt = no

[ req_distinguished_name ]
CN = principal

[ v3_req ]
subjectAltName = IP:172.18.255.200 # Replace with the intented Argo CD Agent principal IP
EOF
openssl x509 -req -in /tmp/tls.csr -CA /tmp/ca.crt -CAkey /tmp/ca.key -CAcreateserial -out /tmp/tls.crt -days 365 -extfile /tmp/openssl_ext.cnf -extensions v3_req
```
