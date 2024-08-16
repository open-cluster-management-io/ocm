package register

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	DefaultKubeConfigContext = "default-context"
	DefaultKubeConfigAuth    = "default-auth"

	ClusterNameFile = "cluster-name"
	AgentNameFile   = "agent-name"
	// KubeconfigFile is the name of the kubeconfig file in kubeconfigSecret
	KubeconfigFile = "kubeconfig"
)

type SecretOption struct {
	// SecretNamespace is the namespace of the secret containing client certificate.
	SecretNamespace string
	// SecretName is the name of the secret containing client certificate. The secret will be created if
	// it does not exist.
	SecretName string

	// BootStrapKubeConfig is the kubeconfig to generate hubkubeconfig, if set, create kubeconfig value
	// in the secret.
	BootStrapKubeConfig *clientcmdapi.Config

	// ClusterName is the cluster name, and it is set as a secret value if it is set.
	ClusterName string
	// AgentName is the agent name and it is set as a secret value if it is set.
	AgentName string

	HubKubeconfigFile string
	HubKubeconfigDir  string

	ManagementSecretInformer cache.SharedIndexInformer
	ManagementCoreClient     corev1client.CoreV1Interface
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
		recorder events.Recorder, opt any) (*corev1.Secret, *metav1.Condition, error)

	// InformerHandler returns informer of the related object. If no object needs to be watched, the func could
	// return nil, nil.
	InformerHandler(option any) (cache.SharedIndexInformer, factory.EventFilterFunc)
}

// Approvers is the inteface that each driver should implement on hub side. The hub controller will use this driver
// to check the registration request from the agent and cleanup.
type Approver interface {
	// Run starts a reconciler on the hub side to monitor the registration request and approve the request
	// if necessary. This is a blocking call.
	Run(ctx context.Context, workers int)

	// Cleanup is executed when hubAcceptClient in ManagedCluster is set false or cluster is deleting. The hub controller
	// deletes rolebindings for the agent, and then this is the additional operation a driver should process.
	Cleanup(ctx context.Context, cluster *clusterv1.ManagedCluster) error
}
