# Install the Managed Service Account

## Prerequisite

Set up the dev environment in your local machine following [setup dev environment](../setup-dev-environment).

You have already installed [Helm](https://helm.sh/docs/intro/install/)

## Install managed-serviceaccount

Run `./deploy.sh` to install

To confirm the installation status

```
$ kubectl get managedclusteraddon -A | grep managed-serviceaccount
NAMESPACE        NAME                     AVAILABLE   DEGRADED   PROGRESSING
<your cluster>   managed-serviceaccount   True
```

## Usage

Apply a sample "ManagedServiceAccount" resource to try the functionality:

```
kubectl apply -f example/managedserviceaccount.yaml
```


Run the following command to check the status, the addon agent is supposed to process the "ManagedServiceAccount" and report the status:
```
$ kubectl describe ManagedServiceAccount my-sample -n <cluster-name>

...
status:
    conditions:
    - lastTransitionTime: "2021-12-09T09:08:15Z"
      message: ""
      reason: TokenReported
      status: "True"
      type: TokenReported
    - lastTransitionTime: "2021-12-09T09:08:15Z"
      message: ""
      reason: SecretCreated
      status: "True"
      type: SecretCreated
    expirationTimestamp: "2022-12-04T09:08:15Z"
    tokenSecretRef:
      lastRefreshTimestamp: "2021-12-09T09:08:15Z"
      name: my-sample
```

Corresponding secret containing the service account token should be persisted under the same namespace where the "ManagedServiceAccount" resource at:

```
$ kubectl -n <your cluster> get secret my-sample  
NAME        TYPE     DATA   AGE
my-sample   Opaque   2      2m23s
```