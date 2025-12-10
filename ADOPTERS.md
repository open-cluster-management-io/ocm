# Adopters

## End users

Open Cluster Management are being used by numerous other companies, both large and small.
The list includes but is not limited to:

| Type | Name | Since | Website | Use Case | 
|--|--|--|--|--|
| Vendor | Alibaba Cloud | 2021 | [Link](https://www.alibabacloud.com/) | Alibaba Cloud uses OCM  for multi-cluster management in their ACK One distributed cloud container platform. The Fleet management feature of ACK One is a solution for the centralized management of multiple clusters based on Open Cluster Management (OCM) from the open-source community. Each Fleet instance is managed by ACK. |
| End user | Ant Group | 2021 | [Link](https://www.antgroup.com) | Ant manages dozens of Kubernetes clusters spread around the globe with thousands of nodes (servers) in each cluster. With OCM they are able to add clusters with very actions. This makes it easy for admins to rapidly deploy and manage clusters for promotions and other high traffic events using the OCM hub and spoke model. |
| End user, Contributor | AppsCode | 2022 | [Link](https://appscode.com/) | AppsCode uses Open Cluster Management (OCM) as the foundation for multi-cluster management within AppsCode Container Engine (ACE) platform, which provides Kubernetes-native DBaaS via KubeDB. OCM's open architecture facilitates the integration of existing addons and the development of custom addons for managing databases in Kubernetes. |
| End user | eBay | 2024 | [Link](https://www.ebay.com) | We use OCM to manage application deployment across 190+ clusters. We deployed an OCM hub cluster and installed OCM agents and Argo CD instances in each managed cluster. OCM distributes application configurations via its ManifestWork and Placement CRDs, wrapping Argo CD Project and Application manifests, ensuring desired-state consistency across clusters. Argo CD handles the actual deployment processes within each cluster.|
| Integration | RamenDR | 2024 | [Link](https://github.com/RamenDR/ramen) | RamenDR is an Open Cluster Management Placement extension that provides recovery and relocation services for workloads, and their persistent data, across a set of OCM managed clusters. It also serves as a use case for the Placement API in DR scenarios. |
| Vendor | Red Hat | 2021 | [Link](https://www.redhat.com) | Red Hat is a founding member of the OCM project and uses OCM as a foundational component in Red Hat Advanced Cluster Management for Kubernetes (RHACM). RHACM is used in multiple production installations for large scale Kubernetes deployments. |
| Vendor | Spectro Cloud | 2025 | [Link](https://www.spectrocloud.com/) | Spectro Cloud uses OCM in their platform-building platform, Mural. Mural uses OCM for intelligent federation of resources across multiple Kubernetes clusters, simplifying application lifecycle management at scale. |
| End user | Xiao Hong Shu | 2022 | [Link](xiaohongshu.com) | Xiao Hong Shu (also known as Xiaohongshu or Little Red Book) is a Chinese social media and e-commerce platform. They were very early adopters of OCM and have contributed to the growth of the multi-cluster space. |
| End user | Guidewire Software Inc. | 2025 | [Link](https://www.guidewire.com/) | Guidewire Software Inc. uses OCM in their Cloud platform for their P&C Insurance customers. To utilize OCM on their platform, they have contributed to the OCM project with implementation of enabling AWS IAM-based registration mechanism on EKS clusters. 

Additional non-public adopters exist as well.

## Ecosystem

Open Cluster Management have integrations available with a number of open-source projects.
The list includes but is not limited to:

- [Argo CD](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Cluster-Decision-Resource/#how-it-works)
- [Argo CD Agent](https://argocd-agent.readthedocs.io/latest/getting-started/ocm-io/)
- [Argo Workflows](https://github.com/argoproj/argo-workflows/issues/3523#issuecomment-1307610573)
- [Cluster API](https://github.com/open-cluster-management-io/ocm/tree/main/solutions/cluster-api)
- [Clusternet](https://github.com/open-cluster-management-io/addon-contrib/tree/main/clusternet-addon)
- [Fluid](https://github.com/open-cluster-management-io/addon-contrib/tree/main/fluid-addon)
- [Helm](https://v0-16.open-cluster-management.io/docs/getting-started/integration/app-lifecycle/)
- [ICOS Meta OS](https://www.icos-project.eu/docs/Administration/ICOS%20Agent/Orchestrators/controlplane/)
- [Istio](https://github.com/open-cluster-management-io/multicluster-mesh)
- [Janus](https://www.npmjs.com/package/@janus-idp/backstage-plugin-ocm-backend)
- [KubeStellar](https://docs.kubestellar.io/latest/direct/start-from-ocm/)
- [KubeVela](https://kubevela.io/docs/platform-engineers/system-operation/working-with-ocm/)
- [Kueue](https://kueue.sigs.k8s.io/docs/tasks/manage/setup_multikueue/#optional-setup-multikueue-with-open-cluster-management)
- [Meshery](https://docs.meshery.io/extensibility/integrations/open-cluster-management)
- [OpenTelemetry](https://github.com/open-cluster-management-io/addon-contrib/tree/main/open-telemetry-addon)
- [Submariner](https://github.com/stolostron/submariner-addon)

## Adding a name

If you have been using OCM and your name is not on this list, send us a PR!
