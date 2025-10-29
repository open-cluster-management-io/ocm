# OCM Argo CD Basic Pull Model
The [Open Cluster Management (OCM)](https://open-cluster-management.io/)
[Argo CD](https://argo-cd.readthedocs.io/en/stable/) add-on uses the hub-spoke pattern
or pull model mechanism for decentralized resource delivery to remote clusters.
By using OCM APIs and components, 
the Argo CD Applications will be pulled from the multi-cluster control plane hub cluster down to the registered OCM managed clusters.
To try it out, check out the [Getting Started Guide](getting-started.md).

## Quick Start

See the [Getting Started](./getting-started.md) for a quick start guide.

## Overview
The current Argo CD resource delivery is primarily pushing resources from a centralized cluster to the remote/managed clusters.

![push model](./assets/push.png)

By using this OCM Argo CD add-on, users can have a pull model resource delivery mechanism.

![pull model](./assets/pull.png)

The pull model may offers some advantages over the existing push model:
- Scalability: hub-spoke pattern may offers better scalability.
- Security: cluster credentials doesn't have to be stored in a centralized environment may enhance security.
- It may reduce the impact of a single point of centralized failure.

This OCM Argo CD add-on on the Hub cluster will create
[ManifestWork](https://open-cluster-management.io/concepts/manifestwork/)
objects wrapping Application objects as payload.
The OCM agent on the Managed cluster will see the ManifestWork on the Hub cluster and pull the Application down.

The Managed cluster with the OCM Argo CD add-on enabled will automatically have an Argo CD instance installed.
The Argo CD application controller from the instance will be able to reconcile the Application CR on the managed cluster.

## OCM Argo CD Advanced Pull Model

See [argocd-pull-integration](https://github.com/open-cluster-management-io/argocd-pull-integration)
for more details.
