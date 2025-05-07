![image](assets/ocm-logo.png)

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/5376/badge)](https://bestpractices.coreinfrastructure.org/projects/5376)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/open-cluster-management-io/ocm/badge)](https://api.securityscorecards.dev/projects/github.com/open-cluster-management-io/ocm)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fopen-cluster-management-io%2Focm.svg?type=shield&issueType=license)](https://app.fossa.com/projects/git%2Bgithub.com%2Fopen-cluster-management-io%2Focm?ref=badge_shield&issueType=license)
[![Artifact HUB Cluster Manager](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/cluster-manager)](https://artifacthub.io/packages/olm/community-operators/cluster-manager)
[![Artifact HUB Klusterlet](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/klusterlet)](https://artifacthub.io/packages/olm/community-operators/klusterlet)

Welcome! The open-cluster-management.io project is focused on enabling end-to-end visibility and control across your Kubernetes clusters.

The Open Cluster Management (OCM) architecture uses a hub - agent model. The hub centralizes control of all the managed clusters. An agent, which we call the klusterlet, resides on each managed cluster to manage registration to the hub and run instructions from the hub.

![image](assets/cncf.png)

OCM is a [Cloud Native Computing Foundation (CNCF) sandbox project](https://www.cncf.io/projects/open-cluster-management).

You can use the [clusteradm CLI](https://github.com/open-cluster-management-io/clusteradm) to bootstrap a control plane for multicluster management. The following diagram illustrates the deployment architecture for OCM:

![image](assets/ocm-arch.png)

To setup a multicluster environment with OCM enabled on your local machine, follow the instructions in [setup dev environment](solutions/setup-dev-environment).

There are a number of key use cases that are enabled by this project, and are categorized to 3 sub projects.

### Cluster Lifecycle: Cluster registration and management

OCM has a group of [APIs](https://github.com/open-cluster-management-io/api) to provide the foundational functions
in multiple cluster management.

The journey of cluster management starts with [Cluster Registration](https://open-cluster-management.io/concepts/architecture/#cluster-registering-double-opt-in-handshaking) which follows a `double opt-in` protocol to establish a MTLS connection from the agent on the managed cluster (Klusterlet) to the hub (Cluster Manager). After this, users or operands on the hub can declare [ManifestWorks](https://open-cluster-management.io/concepts/manifestwork/) which contains a slice of Kubernetes resource manifests to be distributed and applied to a certain managed cluster. To schedule workloads to a certain set of clusters, users can also declare a [Placement](https://open-cluster-management.io/concepts/placement/) on the hub to dynamically select a set of clusters with certain criteria.

In addition, developers can leverage [Addon framework](https://github.com/open-cluster-management-io/addon-framework) to build their own management tools or integrate with other open source projects to extend the multicluster management capability. OCM maintaines two built-in addons for application lifecycle and security governance.

### Application Lifecycle: Delivery, upgrade, and configuration of applications on Kubernetes clusters

Leverage the [Argo CD](https://argo-cd.readthedocs.io/en/stable/)
add-on for OCM to enable decentralized, pull based application deployment to managed clusters.

The OCM Argo CD add-on uses a hub-spoke architecture to deliver Argo CD Applications from the OCM hub cluster to registered managed clusters. Unlike traditional push-based deployment models, this pull mechanism provides several advantages:

- Scalability: hub-spoke pattern may offers better scalability.
- Security: cluster credentials doesn't have to be stored in a centralized environment may enhance security.
- It may reduce the impact of a single point of centralized failure.

Using OCM APIs and components,
Argo CD Applications can be managed centrally while being pulled and applied locally at the managed cluster level.
See [Argo CD OCM add-on](https://github.com/open-cluster-management-io/ocm/tree/main/solutions/deploy-argocd-apps-pull/)
for details on installing the add-on and deploying applications across multiple clusters.

### GRC: Governance, Risk and Compliance across Kubernetes clusters

* Use prebuilt security and configuration controllers to enforce policies on Kubernetes configuration across your clusters.

Policy controllers allow the declarative expression of a desired condition that can be audited or enforced against a set of managed clusters. _Policies_ allow you to drive cross-cluster configuration or validate that a certain configuration explicitly does not exist.

The following repositories describe the underlying API and controllers for the GRC model:

* [config-policy-controller](https://github.com/open-cluster-management-io/config-policy-controller)
* [governance-policy-framework-addon](https://github.com/open-cluster-management-io/governance-policy-framework-addon)
* [governance-policy-propagator](https://github.com/open-cluster-management-io/governance-policy-propagator)

### More external integrations

We are constantly working with other open source projects to make multicluster management easier.

- [Submariner](https://submariner.io/) is a project that provides multicluster networking connectivity. Users can benefit from a [Submariner](https://submariner.io/) addon, which automates the deployment and management of multicluster networking.
- [Clusternet](http://github.com/clusternet/clusternet) is another project that provides multicluster orchestration, which can be easily plug into OCM with [clusternet addon](https://github.com/skeeey/clusternet-addon)
- [KubeVela](https://kubevela.io/) is a modern application delivery platform that makes deploying and operating applications across today's hybrid, multi-cloud environments easier, faster and more reliable. Note that OCM is also available as an [vela addon](https://github.com/kubevela/catalog/tree/master/addons/ocm-hub-control-plane) in KubeVela.

### Get connected

See the following options to connect with the community:

 - [Website](https://open-cluster-management.io)
 - [Slack](https://kubernetes.slack.com/archives/C01GE7YSUUF)
 - [Mailing group](https://groups.google.com/g/open-cluster-management)
 - [Community meetings](https://github.com/open-cluster-management-io/community/projects/1)
 - [YouTube channel](https://www.youtube.com/c/OpenClusterManagement)
