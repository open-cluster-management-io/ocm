# Migrating Kubefed to Open Cluster Management

This guide shows how to migrate from [Kubefed](https://github.com/kubernetes-sigs/kubefed) to [Open Cluster Management](https://open-cluster-management.io/) in a simple scenario.

## Prerequisite

- Kubefed environment
- [clusteradm](https://open-cluster-management.io/getting-started/quick-start/#bootstrap-via-clusteradm-cli-tool) CLI tool installed

## Overview of the migration process

Open Cluster Management control plane can be installed in the same cluster as the Kubefed control plane.
The goal of this guide is to have Open Cluster Management running side by side with Kubefed.
There is no need to remove any Kubefed related deployments, resources or agents until you are comfortable with your Open Cluster Management setup.

## Steps by steps

1. Install the Open Cluster Management control plane by logging into the Kubefed federation cluster and run the `clusteradm init` command.
See [deploy a cluster manager on your hub cluster](https://open-cluster-management.io/getting-started/quick-start/#deploy-a-cluster-manager-on-your-hub-cluster) for more details.

2. Register the Kubefed federated clusters by logging into each cluster and run the `clusteradm join` command.
See [deploy a klusterlet agent on your managed cluster](https://open-cluster-management.io/getting-started/quick-start/#deploy-a-klusterlet-agent-on-your-managed-cluster) for more details.

3. Accept the registration request on the Kubefed federation by logging into the federation cluster and run the `clusteradm accept` command.
See [accept the join request and verify](https://open-cluster-management.io/getting-started/quick-start/#accept-the-join-request-and-verify) for more details.

4. Using the [ManifestWork](https://open-cluster-management.io/concepts/manifestwork/) API, you can now deploy resources from the federation cluster to the federated clusters.
See [deploy kubernetes resources](https://open-cluster-management.io/scenarios/deploy-kubernetes-resources/) for more details.

## References

- [Open Cluster Management](https://open-cluster-management.io/)
- [Kubefed](https://github.com/kubernetes-sigs/kubefed)
