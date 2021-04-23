package agent

import (
	"fmt"

	addonapiv1alpha1 "github.com/open-cluster-management/api/addon/v1alpha1"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// AgentAddon defines manifests of agent deployed on managed cluster
type AgentAddon interface {
	// Manifests returns a list of manifest resources to be deployed on the managed cluster for this addon
	Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error)

	// GetAgentAddonOptions returns the agent options.
	GetAgentAddonOptions() AgentAddonOptions
}

// AgentAddonOptions are the argumet for creating an addon agent.
type AgentAddonOptions struct {
	// AddonName is the name of the addon
	AddonName string

	// Registration is the registration config for the addon
	Registration *RegistrationOption
}

type RegistrationOption struct {
	// CSRConfigurations returns a list of csr configuration for the adddon agent in a managed cluster.
	// A csr will be created from the managed cluster for addon agent with each CSRConfiguration.
	CSRConfigurations func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig

	// CSRApproveCheck checks whether the addon agent registration should be approved by the hub.
	// Addon hub controller can implment this func to auto-approve the CSR. A possible check should include
	// the validity of requster and request payload.
	// If the function is not set, the registration and certificate renewal of addon agent needs to be approved manually on hub.
	// +optional
	CSRApproveCheck func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool

	// PermissionConfig defines the function for an addon to setup rbac permission.
	PermissionConfig func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error

	// CSRSign signs a csr and returns a certificate. It is used when the addon has its own customized signer.
	// +optional
	CSRSign func(csr *certificatesv1.CertificateSigningRequest) []byte
}

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
