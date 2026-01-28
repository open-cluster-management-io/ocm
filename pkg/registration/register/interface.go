package register

import (
	"context"
	"crypto/x509/pkix"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

const (
	DefaultKubeConfigContext = "default-context"
	DefaultKubeConfigAuth    = "default-auth"

	ClusterNameFile = "cluster-name"
	AgentNameFile   = "agent-name"
	// KubeconfigFile is the name of the kubeconfig file in kubeconfigSecret
	KubeconfigFile = "kubeconfig"
)

// CSRConfiguration provides configuration for CSR-based authentication.
type CSRConfiguration interface {
	// GetExpirationSeconds returns the requested duration of validity of the issued certificate in seconds
	GetExpirationSeconds() int32
}

// TokenConfiguration provides configuration for token-based authentication.
type TokenConfiguration interface {
	// GetExpirationSeconds returns the requested duration of validity of the token in seconds
	GetExpirationSeconds() int64
}

type SecretOption struct {
	// SecretNamespace is the namespace of the secret containing client certificate.
	SecretNamespace string
	// SecretName is the name of the secret containing client certificate. The secret will be created if
	// it does not exist.
	SecretName string

	// BootStrapKubeConfig is the kubeconfig to generate hubkubeconfig, if set, create kubeconfig value
	// in the secret.
	BootStrapKubeConfigFile string

	// ClusterName is the cluster name, and it is set as a secret value if it is set.
	ClusterName string
	// AgentName is the agent name and it is set as a secret value if it is set.
	AgentName string

	HubKubeconfigFile string
	HubKubeconfigDir  string

	// subject of the agent, only used for addon
	Subject *pkix.Name
	// csr signer for the addon
	Signer string
}

// StatusUpdateFunc is A function to update the condition of the corresponding object.
type StatusUpdateFunc func(ctx context.Context, cond metav1.Condition) error

// RegisterDriver is the interface that each driver should implement for the agent. The agent
// uses the driver to build the kubeconfig or other crendential to connect to the hub cluster.
type RegisterDriver interface {
	// IsHubKubeConfigValid is to check if the current hube-kubeconfig is valid. It is called before
	// and after bootstrap to confirm if the bootstrap is finished.
	IsHubKubeConfigValid(ctx context.Context, secretOption SecretOption) (bool, error)

	// BuildKubeConfigFromTemplate builds the kubeconfig from the template kubeconfig
	BuildKubeConfigFromTemplate(template *clientcmdapi.Config) *clientcmdapi.Config

	// Process update secret with credentials
	Process(
		ctx context.Context,
		name string,
		secret *corev1.Secret,
		additionalSecretData map[string][]byte,
		recorder events.Recorder) (*corev1.Secret, *metav1.Condition, error)

	// InformerHandler returns informer of the related object. If no object needs to be watched, the func could
	// return nil, nil.
	InformerHandler() (cache.SharedIndexInformer, factory.EventFilterFunc)

	// ManagedClusterDecorator is to change managed cluster metadata or spec during registration process.
	ManagedClusterDecorator(cluster *clusterv1.ManagedCluster) *clusterv1.ManagedCluster

	// BuildClients setup clients for the driver based on the secretOption and return
	BuildClients(ctx context.Context, secretOption SecretOption, bootstrap bool) (*Clients, error)
}

// AddonAuthConfig provides complete configuration for addon registration,
// including authentication method and access to driver options.
type AddonAuthConfig interface {
	// GetKubeClientAuth returns the authentication method for addons with registration type KubeClient.
	// Possible values are "csr" (default) and "token".
	GetKubeClientAuth() string

	// GetCSRConfiguration returns the CSR driver configuration interface
	GetCSRConfiguration() CSRConfiguration

	// GetTokenConfiguration returns the token driver configuration interface
	GetTokenConfiguration() TokenConfiguration
}

// AddonDriverFactory is an interface for creating RegisterDriver instances for addon registration.
// It acts as a factory that creates (forks) driver instances for specific addons.
type AddonDriverFactory interface {
	// Fork creates a RegisterDriver instance for a specific addon.
	// Parameters:
	//   - addonName: the name of the addon
	//   - authConfig: authentication configuration including type and authentication method
	//   - secretOption: configuration for the secret that will store credentials
	// Returns the driver instance and an error if the driver cannot be created
	Fork(addonName string, authConfig AddonAuthConfig, secretOption SecretOption) (RegisterDriver, error)
}

// HubDriver interface is used to implement operations required to complete aws-irsa registration and csr registration.
// The Approver interface above is used to implement operations related to approving the CSR request, and permission
// creation is not related to CSR approval. Hence, created CreatePermissions under a separate interface.
type HubDriver interface {
	// CreatePermissions is executed when hubAcceptClient in ManagedCluster is set to true. The hub controller creates the
	// required permissions for the spoke to be able to access resources on the hub cluster.
	CreatePermissions(ctx context.Context, cluster *clusterv1.ManagedCluster) error

	// Run starts a reconciler on the hub side to monitor the registration request and approve the request
	// if necessary. This is a blocking call.
	Run(ctx context.Context, workers int)

	// Cleanup is executed when hubAcceptClient in ManagedCluster is set false or cluster is deleting. The hub controller
	// deletes rolebindings for the agent, and then this is the additional operation a driver should process.
	Cleanup(ctx context.Context, cluster *clusterv1.ManagedCluster) error

	// Accept is executed when autoapproval is enabled to verify if the managedcluster uses csr or awsirsa and
	// setting hubAcceptClient based on autoApprovedIdentities. If the cluster is not managed by a HubDriver
	// implementation, this method should return true
	Accept(cluster *clusterv1.ManagedCluster) bool
}
