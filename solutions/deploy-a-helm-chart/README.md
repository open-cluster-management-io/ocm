# Deploy a Helm Chart

> **⚠️ Deprecated:** This solution depends on the `application-manager` addon, which is
> backed by the [`multicloud-operators-subscription`](https://github.com/open-cluster-management-io/multicloud-operators-subscription)
> repository. That repository was **archived by its owners on June 2, 2025**, ahead of the
> OCM v1.0.0 release, in favor of Argo CD-based GitOps integration (see the archived repo's
> own README for the announcement). It still installs and runs, but it is no longer
> maintained upstream and should not be used for new work.
>
> For deploying Helm charts (or any application) across a fleet of clusters today, use one
> of the Argo CD solutions instead: [`deploy-argocd-apps`](../deploy-argocd-apps) (push
> model), [`deploy-argocd-apps-pull`](../deploy-argocd-apps-pull) (basic pull model), or
> [`argocd-agent`](../argocd-agent) (advanced pull model).

## Prerequisite

Set up the dev environment in your local machine following [setup dev environment](../setup-dev-environment).

## Install application addon on OCM

Install application manager addon on the hub cluster

```
kubectl config use kind-hub
clusteradm install hub-addon --names application-manager
```

Install application manager agent on all the managed clusters

```
clusteradm addon enable --names application-manager --cluster cluster1,cluster2
```

You will see that all agents is available after waiting a while

```
$ kubectl get managedclusteraddon --all-namespaces
NAMESPACE   NAME                  AVAILABLE   DEGRADED   PROGRESSING
cluster1    application-manager   True
cluster2    application-manager   True
```

## Deploy a helm chart to cluster1

Run `./deploy.sh` to deploy the helm chart on cluster1

It will create a [channel](manifests/channel.yaml) which specifies a helm repo, a [placement](manifests/placement.yaml)
to select one or multiple clusters, and a [subscription](manifests/subscription.yaml) to deploy the helm chart. Try
update `placement` to see how the chart deployment is changed.
