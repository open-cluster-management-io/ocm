package agent

import (
	"fmt"

	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

// AgentAddon is a mandatory interface for implementing a custom addon.
// The addon is expected to be registered into an AddonManager so the manager will be invoking the addon
// implementation below as callbacks upon:
//   - receiving valid watch events from ManagedClusterAddon.
//   - receiving valid watch events from ManifestWork.
type AgentAddon interface {

	// Manifests returns a list of manifest resources to be deployed on the managed cluster for this addon.
	// The resources in this list are required to explicitly clarify their TypeMta (i.e. apiVersion, kind)
	// otherwise the addon deploying will fail constantly. A recommended set of addon component returned
	// here will be:
	//   - the hosting namespace of the addon-agents.
	//   - a deployment of the addon agents.
	//   - the configurations (e.g. configmaps) mounted by the deployment.
	//   - the RBAC permission bond to the addon agents *in the managed cluster*. (the hub cluster's RBAC
	//     setup shall be done at GetAgentAddonOptions below.)
	// NB for dispatching namespaced resources, it's recommended to include the namespace in the list.
	Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error)

	// GetAgentAddonOptions returns the agent options for advanced agent customization.
	// A minimal option is merely setting a unique addon name in the AgentAddonOptions.
	GetAgentAddonOptions() AgentAddonOptions
}

// AgentAddonOptions prescribes the future customization for the addon.
type AgentAddonOptions struct {
	// AddonName is the name of the addon.
	// Should be globally unique.
	// +required
	AddonName string

	// Registration prescribes the custom behavior during CSR applying, approval and signing.
	// +optional
	Registration *RegistrationOption

	// InstallStrategy defines that addon should be created in which clusters.
	// Addon will not be installed automatically until a ManagedClusterAddon is applied to the cluster's
	// namespace if InstallStrategy is nil.
	// Deprecated: use installStrategy config in ClusterManagementAddOn API instead
	// +optional
	InstallStrategy *InstallStrategy

	// Updaters select a set of resources and define the strategies to update them.
	// UpdateStrategy is Update if no Updater is defined for a resource.
	// +optional
	Updaters []Updater

	// HealthProber defines how is the healthiness status of the ManagedClusterAddon probed.
	// Note that the prescribed prober type here only applies to the automatically installed
	// addons configured via InstallStrategy.
	// If nil, will be defaulted to "Lease" type.
	// +optional
	HealthProber *HealthProber

	// HostedModeEnabled defines whether the Hosted deploying mode for the addon agent is enabled
	// If not set, will be defaulted to false.
	// +optional
	HostedModeEnabled bool

	// SupportedConfigGVRs is a list of addon supported configuration GroupVersionResource
	// each configuration GroupVersionResource should be unique
	SupportedConfigGVRs []schema.GroupVersionResource

	// AgentDeployTriggerClusterFilter defines the filter func to trigger the agent deploy/redploy when cluster info is
	// changed. Addons that need information from the ManagedCluster resource when deploying the agent should use this
	// field to set what information they need, otherwise the expected/up-to-date agent may be deployed delayed since
	// the default filter func returns false when the ManagedCluster resource is updated.
	//
	// For example, the agentAddon needs information from the ManagedCluster annotation, it can set the filter function
	// like:
	//
	//	AgentDeployTriggerClusterFilter: func(old, new *clusterv1.ManagedCluster) bool {
	//	 return !equality.Semantic.DeepEqual(old.Annotations, new.Annotations)
	//	}
	// +optional
	AgentDeployTriggerClusterFilter func(old, new *clusterv1.ManagedCluster) bool

	// ManifestConfigs represents the configurations of manifests defined in workload field. It will:
	// - override the update strategy set by the "Updaters" field
	// - merge the feedback rules set by the "HealthProber" field if they have the same resource identifier,
	//   when merging the feedback rules, if the rule configured here is json path type, will ignore the
	//   json path which is already in the existing rules, compare by the path name.
	// +optional
	ManifestConfigs []workapiv1.ManifestConfigOption
}

type CSRSignerFunc func(csr *certificatesv1.CertificateSigningRequest) []byte

type CSRApproveFunc func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool

type PermissionConfigFunc func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error

// RegistrationOption defines how agent is registered to the hub cluster. It needs to define:
// 1. csr with what subject/signer should be created
// 2. how csr is approved
// 3. the RBAC setting of agent on the hub
// 4. how csr is signed if the customized signer is used.
type RegistrationOption struct {
	// CSRConfigurations returns a list of csr configuration for the adddon agent in a managed cluster.
	// A csr will be created from the managed cluster for addon agent with each CSRConfiguration.
	// +required
	CSRConfigurations func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig

	// Namespace is the namespace where registraiton credential will be put on the managed cluster. It
	// will be overridden by installNamespace on ManagedClusterAddon spec if set
	// Deprecated: use AgentInstallNamespace instead
	Namespace string

	// AgentInstallNamespace returns the namespace where registration credential will be put on the managed cluster.
	// It will override the installNamespace on ManagedClusterAddon spec if set and the returned value is not empty.
	// Note: Set this very carefully. If this is set, the addon agent must be deployed in the same namespace, which
	// means when implementing Manifests function in AgentAddon interface, the namespace of the addon agent manifest
	// must be set to the same value returned by this function.
	AgentInstallNamespace func(addon *addonapiv1alpha1.ManagedClusterAddOn) string

	// CSRApproveCheck checks whether the addon agent registration should be approved by the hub.
	// Addon hub controller can implement this func to auto-approve all the CSRs. A better CSR check is
	// recommended to include (1) the validity of requester's requesting identity and (2) the other request
	// payload such as key-usages.
	// If the function is not set, the registration and certificate renewal of addon agent needs to be approved
	// manually on hub.
	// NB auto-approving csr requires the addon manager to have sufficient RBAC permission for the target
	// signer, e.g.:
	// >>  {    "apiGroups":["certificates.k8s.io"],
	// >>		"resources":["signers"],
	// >>		"resourceNames":["kubernetes.io/kube-apiserver-client"]...}
	// +optional
	CSRApproveCheck CSRApproveFunc

	// PermissionConfig defines the function for an addon to setup rbac permission. This callback doesn't
	// couple with any concrete RBAC Api so the implementation is expected to ensure the RBAC in the hub
	// cluster by calling the kubernetes api explicitly. Additionally we can also extend arbitrary third-party
	// permission setup in this callback.
	// +optional
	PermissionConfig PermissionConfigFunc

	// CSRSign signs a csr and returns a certificate. It is used when the addon has its own customized signer.
	// The returned byte array shall be a valid non-nil PEM encoded x509 certificate.
	// +optional
	CSRSign CSRSignerFunc
}

// InstallStrategy is the installation strategy of the manifests prescribed by Manifests(..).
type InstallStrategy struct {
	*installStrategy
}

type installStrategy struct {
	// InstallNamespace is target deploying namespace in the managed cluster upon automatic addon installation.
	InstallNamespace string

	// managedClusterFilter will filter the clusters to install the addon to.
	managedClusterFilter func(cluster *clusterv1.ManagedCluster) bool
}

func (s *InstallStrategy) GetManagedClusterFilter() func(cluster *clusterv1.ManagedCluster) bool {
	return s.managedClusterFilter
}

type Updater struct {
	// ResourceIdentifier sets what resources the strategy applies to
	ResourceIdentifier workapiv1.ResourceIdentifier

	// UpdateStrategy defines the strategy used to update the manifests.
	UpdateStrategy workapiv1.UpdateStrategy
}

type HealthProber struct {
	Type HealthProberType

	WorkProber *WorkHealthProber
}

type AddonHealthCheckFunc func(workapiv1.ResourceIdentifier, workapiv1.StatusFeedbackResult) error

type WorkHealthProber struct {
	// ProbeFields tells addon framework what field to probe
	ProbeFields []ProbeField

	// HealthCheck check status of the addon based on probe result.
	HealthCheck AddonHealthCheckFunc
}

// ProbeField defines the field of a resource to be probed
type ProbeField struct {
	// ResourceIdentifier sets what resource should be probed
	ResourceIdentifier workapiv1.ResourceIdentifier

	// ProbeRules sets the rules to probe the field
	ProbeRules []workapiv1.FeedbackRule
}

type HealthProberType string

const (
	// HealthProberTypeNone indicates the healthiness status will be refreshed, which is
	// leaving the healthiness of ManagedClusterAddon to an empty string.
	HealthProberTypeNone HealthProberType = "None"
	// HealthProberTypeLease indicates the healthiness of the addon is connected with the
	// corresponding lease resource in the cluster namespace with the same name as the addon.
	// Note that the lease object is expected to periodically refresh by a local agent
	// deployed in the managed cluster implementing lease.LeaseUpdater interface.
	HealthProberTypeLease HealthProberType = "Lease"
	// HealthProberTypeWork indicates the healthiness of the addon is equal to the overall
	// dispatching status of the corresponding ManifestWork resource.
	// It's applicable to those addons that don't have a local agent instance in the managed
	// clusters. The addon framework will check if the work is Available on the spoke. In addition
	// user can define a prober to check more detailed status based on status feedback from work.
	HealthProberTypeWork HealthProberType = "Work"
	// HealthProberTypeDeploymentAvailability indicates the healthiness of the addon is connected
	// with the availability of the corresponding agent deployment resources on the managed cluster.
	// It's a special case of HealthProberTypeWork.
	HealthProberTypeDeploymentAvailability HealthProberType = "DeploymentAvailability"
)

func KubeClientSignerConfigurations(addonName, agentName string) func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {
	return func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {
		return []addonapiv1alpha1.RegistrationConfig{
			{
				SignerName: certificatesv1.KubeAPIServerClientSignerName,
				Subject: addonapiv1alpha1.Subject{
					User:   DefaultUser(cluster.Name, addonName, agentName),
					Groups: DefaultGroups(cluster.Name, addonName),
				},
			},
		}
	}
}

// DefaultUser returns the default User
func DefaultUser(clusterName, addonName, agentName string) string {
	return fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s:agent:%s", clusterName, addonName, agentName)
}

// DefaultGroups returns the default groups
func DefaultGroups(clusterName, addonName string) []string {
	return []string{
		fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s", clusterName, addonName),
		fmt.Sprintf("system:open-cluster-management:addon:%s", addonName),
		"system:authenticated",
	}
}

// InstallAllStrategy indicate to install addon to all clusters
func InstallAllStrategy(installNamespace string) *InstallStrategy {
	return &InstallStrategy{
		&installStrategy{
			InstallNamespace: installNamespace,
			managedClusterFilter: func(cluster *clusterv1.ManagedCluster) bool {
				return true
			},
		},
	}
}

// InstallByLabelStrategy indicate to install addon based on clusters' label
func InstallByLabelStrategy(installNamespace string, selector metav1.LabelSelector) *InstallStrategy {
	return &InstallStrategy{
		&installStrategy{
			InstallNamespace: installNamespace,
			managedClusterFilter: func(cluster *clusterv1.ManagedCluster) bool {
				selector, err := metav1.LabelSelectorAsSelector(&selector)
				if err != nil {
					klog.Warningf("labels selector is not correct: %v", err)
					return false
				}

				if !selector.Matches(labels.Set(cluster.Labels)) {
					return false
				}
				return true
			},
		},
	}
}

// InstallByFilterFunctionStrategy indicate to install addon based on a filter function, and it will also install addons if the filter function is nil.
func InstallByFilterFunctionStrategy(installNamespace string, f func(cluster *clusterv1.ManagedCluster) bool) *InstallStrategy {
	if f == nil {
		f = func(cluster *clusterv1.ManagedCluster) bool {
			return true
		}
	}
	return &InstallStrategy{
		&installStrategy{
			InstallNamespace:     installNamespace,
			managedClusterFilter: f,
		},
	}
}

// ApprovalAllCSRs returns true for all csrs.
func ApprovalAllCSRs(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
	return true
}
