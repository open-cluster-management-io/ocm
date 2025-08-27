![OCM logo](assets/ocm-logo.png)

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/5376/badge)](https://bestpractices.coreinfrastructure.org/projects/5376)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/open-cluster-management-io/ocm/badge)](https://api.securityscorecards.dev/projects/github.com/open-cluster-management-io/ocm)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fopen-cluster-management-io%2Focm.svg?type=shield&issueType=license)](https://app.fossa.com/projects/git%2Bgithub.com%2Fopen-cluster-management-io%2Focm?ref=badge_shield&issueType=license)
[![Artifact HUB Cluster Manager](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/cluster-manager)](https://artifacthub.io/packages/olm/community-operators/cluster-manager)
[![Artifact HUB Klusterlet](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/klusterlet)](https://artifacthub.io/packages/olm/community-operators/klusterlet)

Open Cluster Management (OCM) is a [Cloud Native Computing Foundation (CNCF) sandbox project](https://www.cncf.io/projects/open-cluster-management) focused on multicluster and multicloud scenarios for Kubernetes applications. At its core, OCM exists to make running, managing, and utilizing heterogeneous, multicluster environments simpler.

![CNCF logo](assets/cncf.png)

## Project Goals and Objectives

We believe that no one runs just one cluster, and making multicluster management as easy as possible encourages community growth across all projects, empowering our users and their customers. Our core goal is to "multicluster everything."

OCM provides a flexible, extensible, and open model that allows any software project to easily understand how to run in a multicluster way. We align with and contribute to the SIG-Multicluster APIs for multicluster management while developing and sharing key concepts and components that make it easier and more accessible for any project, large or small, to "do multicluster."

### Differentiation in the Cloud Native Landscape

Open Cluster Management differentiates itself in several key ways:

- **Vendor Neutral**: Avoid vendor lock-in by using APIs that are not tied to any cloud providers or proprietary platforms
- **Standards-Based**: We provide both a reference implementation of the APIs put forward by SIG-Multicluster as well as extended capabilities to accentuate them for numerous additional use cases
- **Plug-and-Play Architecture**: By standardizing on a single PlacementDecision API, any scheduler can publish its choices and any consumer can subscribe to them, eliminating friction and enabling true interoperability
- **Extensible Framework**: Our add-on structure allows projects to gain the benefits of simple, secure, and vendor-neutral multicloud management with minimal code changes

## What OCM Does

OCM utilizes open APIs and an easy-to-use add-on structure to provide comprehensive multicluster management capabilities. We do this because we believe that managing multiple clusters should be simple and easy. With the variety of both free and proprietary cloud and on-premises offerings, it is important to help the community utilize these heterogeneous environments to their fullest potential.

Our architecture is modular and extensible, built to always model Kubernetes principles. We utilize open declarative APIs and build code with DevOps and automation. OCM runs on a hub-spoke model that can survive failure and recovery of components without interrupting workloads. It's containerized, vendor-neutral, and focuses on declarative management style aligned with GitOps principles.

### Core Architecture

The OCM architecture uses a hub-spoke model where the hub centralizes control of all managed clusters. An agent, called the klusterlet, resides on each managed cluster to manage registration to the hub and run instructions from the hub.

![OCM Architecture](assets/ocm-arch.png)

## Core Components

### Cluster Inventory

Registration of multiple clusters to a hub cluster to place them for management. For comprehensive details on cluster inventory management, see the [cluster inventory documentation](https://open-cluster-management.io/docs/concepts/cluster-inventory/). OCM implements a secure "double opt-in handshaking" protocol that requires explicit consent from both hub cluster administrators and managed cluster administrators to establish the connection. 

The registration process creates an mTLS connection between the registration-agent on the managed cluster (Klusterlet) and the registration-controller on the hub (Cluster Manager). Once registered, each managed cluster is assigned a dedicated namespace on the hub for isolation and security. The registration-controller continuously monitors cluster health and manages cluster lifecycle operations including certificate rotation and permission management.

### Work Distribution

The [ManifestWork API](https://open-cluster-management.io/docs/concepts/work-distribution/) enables resources to be applied to managed clusters from a hub cluster. ManifestWork acts as a wrapper containing Kubernetes resource manifests that need to be distributed and applied to specific managed clusters.

The work-agent running on each managed cluster actively pulls ManifestWork resources from its dedicated namespace on the hub and applies them locally. This pull-based approach ensures scalability and eliminates the need to store managed cluster credentials on the hub. The API supports sophisticated features including update strategies, conflict resolution, resource ordering, and status feedback to provide comprehensive resource distribution capabilities.

### Content Placement

Dynamic placement of content and behavior across multiple clusters. The [Placement API](https://open-cluster-management.io/docs/concepts/content-placement/placement/) is a sophisticated scheduler for multicluster environments that operates on a Hub-Spoke model and is deeply integrated with the Work API.

The Placement API allows administrators to use vendor-neutral selectors to dynamically choose clusters based on labels, resource capacity, or custom criteria. The PlacementDecision resource provides a list of selected clusters that can be consumed by any scheduler or controller.

### Add-on Framework

[OCM Add-ons](https://open-cluster-management.io/docs/concepts/add-on-extensibility/addon/) provide a clear framework that allows developers to easily add their software to OCM and make it multicluster aware. Add-ons are simple to write and are fully documented and maintained according to specifications in the [addon-framework](https://github.com/open-cluster-management-io/addon-framework) repository.

The framework handles the complex aspects of multicluster deployment including registration, lifecycle management, configuration distribution, and status reporting. Add-ons are used by multiple projects such as Argo, Submariner, KubeVela, and more. They are also used extensively for internal components of OCM, ensuring the framework is actively developed and maintained. With add-ons, any project can plug into OCM with minimal code and almost instantaneously become multicluster aware.

## Built-in Extensions

These optional but valuable capabilities extend OCM's core functionality for specific use cases.

### Cluster Lifecycle Management

OCM integrates seamlessly with [Cluster API](https://cluster-api.sigs.k8s.io/) to provide comprehensive cluster lifecycle management. Cluster API is a Kubernetes sub-project focused on providing declarative APIs and tooling to simplify provisioning, upgrading, and operating multiple Kubernetes clusters.

The integration allows OCM to work alongside Cluster API management planes, where clusters provisioned through Cluster API can be automatically registered with OCM for multicluster management. OCM's hub cluster can run in the same cluster as the Cluster API management plane, enabling seamless workflows from cluster creation to workload deployment and policy enforcement. See the [Cluster API integration guide](solutions/cluster-api/) for detailed setup instructions.

### Application Lifecycle Management

Leverage the [Argo CD](https://argo-cd.readthedocs.io/en/stable/) add-on for OCM to enable decentralized, pull-based application deployment to managed clusters. The OCM Argo CD add-on uses a hub-spoke architecture to deliver Argo CD Applications from the OCM hub cluster to registered managed clusters.

Unlike traditional push-based deployment models, this pull mechanism provides several advantages:
- **Scalability**: Hub-spoke pattern offers better scalability
- **Security**: Cluster credentials don't have to be stored in a centralized environment
- **Resilience**: Reduces the impact of a single point of centralized failure

See [Argo CD OCM add-on](solutions/deploy-argocd-apps-pull/) for details on installing the add-on and deploying applications across multiple clusters.

### Governance, Risk, and Compliance (GRC)

Use prebuilt security and configuration controllers to enforce policies on Kubernetes configuration across your clusters. Policy controllers allow the declarative expression of desired conditions that can be audited or enforced against a set of managed clusters.

Related repositories:
- [config-policy-controller](https://github.com/open-cluster-management-io/config-policy-controller)
- [governance-policy-framework-addon](https://github.com/open-cluster-management-io/governance-policy-framework-addon)
- [governance-policy-propagator](https://github.com/open-cluster-management-io/governance-policy-propagator)

## Cloud Native Use Cases

Everything OCM does is Cloud Native by definition. Our comprehensive [User Scenarios](https://open-cluster-management.io/docs/scenarios/) section provides detailed examples of how OCM enables various multicluster and multicloud use cases.

## Getting Started

You can use the [clusteradm CLI](https://github.com/open-cluster-management-io/clusteradm) to bootstrap a control plane for multicluster management. To set up a multicluster environment with OCM enabled on your local machine, follow the instructions in [setup dev environment](solutions/setup-dev-environment).

For developers looking to contribute to OCM, see our comprehensive [Development Guide](development.md) which covers development environment setup, code standards, testing, and contribution workflows.

## External Integrations

We constantly work with other open-source projects to make multicluster management easier:

- **[Argo CD](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Cluster-Decision-Resource/#how-it-works)** (CNCF): OCM supplies Argo CD with ClusterDecision resources via Argo CD's Cluster Decision Resource Generator, enabling it to select target clusters for GitOps deployments
- **[Clusternet](https://github.com/clusternet/clusternet)**: (CNCF) Multicluster orchestration that can easily plug into OCM with [clusternet addon](https://github.com/open-cluster-management-io/addon-contrib/tree/main/clusternet-addon)
- **[KubeVela](https://kubevela.io/docs/platform-engineers/system-operation/working-with-ocm/)** (CNCF): KubeVela develops an integration addon to work with OCM supporting application deployment across multiple clusters
- **[KubeStellar](https://docs.kubestellar.io/latest/direct/start-from-ocm/)** (CNCF): KubeStellar uses OCM as the backend of multicluster management
- **[Kueue](https://kueue.sigs.k8s.io/docs/tasks/manage/setup_multikueue/#optional-setup-multikueue-with-open-cluster-management)** (Kubernetes SIGs): OCM supplies Kueue with a streamlined MultiKueue setup process, automated generation of MultiKueue specific Kubeconfig, and enhanced multicluster scheduling capabilities
- **[Submariner](https://submariner.io/)**: (CNCF) Provides multicluster networking connectivity with automated deployment and management

## Documentation and Resources

For comprehensive information about OCM, visit our [website](https://open-cluster-management.io/) with detailed sections on [key concepts](https://open-cluster-management.io/docs/concepts/).

## Community

Connect with the OCM community:

- [Website](https://open-cluster-management.io)
- [Slack](https://kubernetes.slack.com/channels/open-cluster-mgmt)
- [Mailing group](https://groups.google.com/g/open-cluster-management)
- [Community meetings](https://calendar.google.com/calendar/u/0/embed?src=openclustermanagement@gmail.com)
- [YouTube channel](https://www.youtube.com/c/OpenClusterManagement)

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
