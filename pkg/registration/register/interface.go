package register

import (
	"context"
	"os"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
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

// A function to update the condition of the corresponding object.
type StatusUpdateFunc func(ctx context.Context, cond metav1.Condition) error

// Register is to start a process that maintains a secret defined in the secretOption.
type Register interface {
	// Start starts the bootstrap/creds rotate process for the agent.
	Start(ctx context.Context, name string, statusUpdater StatusUpdateFunc, recorder events.Recorder, secretOption SecretOption, option any)

	// IsHubKubeConfigValidFunc returns a func to check if the current hube-kubeconfig is valid. It is called before
	// and after bootstrap to confirm if the bootstrap is finished.
	IsHubKubeConfigValidFunc(secretOption SecretOption) wait.ConditionWithContextFunc
}

// RegisterDriver is the interface that each driver should implement
type RegisterDriver interface {
	// IsHubKubeConfigValidFunc returns a func to check if the current hube-kubeconfig is valid. It is called before
	// and after bootstrap to confirm if the bootstrap is finished.
	IsHubKubeConfigValid(ctx context.Context, secretOption SecretOption) (bool, error)

	// BuildKubeConfigFromBootstrap builds the kubeconfig from the bootstrap kubeconfig
	BuildKubeConfigFromBootstrap(config *clientcmdapi.Config) (*clientcmdapi.Config, error)

	// Process update secret with credentials
	Process(
		ctx context.Context,
		name string,
		secret *corev1.Secret,
		additionalSecretData map[string][]byte,
		recorder events.Recorder, opt any) (*corev1.Secret, *metav1.Condition, error)

	// InformerHandler returns informer related object
	InformerHandler(option any) (cache.SharedIndexInformer, factory.EventFilterFunc)
}

type registerImpl struct {
	driver RegisterDriver
}

func NewRegister(driver RegisterDriver) Register {
	return &registerImpl{
		driver: driver,
	}
}

func (r *registerImpl) IsHubKubeConfigValidFunc(secretOption SecretOption) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (bool, error) {
		logger := klog.FromContext(ctx)
		if _, err := os.Stat(secretOption.HubKubeconfigFile); os.IsNotExist(err) {
			logger.V(4).Info("Kubeconfig file not found", "kubeconfigPath", secretOption.HubKubeconfigFile)
			return false, nil
		}

		// create a kubeconfig with references to the key/cert files in the same secret
		hubKubeconfig, err := clientcmd.LoadFromFile(secretOption.HubKubeconfigFile)
		if err != nil {
			return false, err
		}

		if valid, err := IsHubKubeconfigValid(secretOption.BootStrapKubeConfig, hubKubeconfig); !valid || err != nil {
			return valid, err
		}

		return r.driver.IsHubKubeConfigValid(ctx, secretOption)
	}
}

func (r *registerImpl) Start(
	ctx context.Context,
	name string,
	statusUpdater StatusUpdateFunc,
	recorder events.Recorder,
	secretOption SecretOption, option any) {
	secretController := newSecretController(secretOption, option, r.driver, statusUpdater, recorder, name)
	secretController.Run(ctx, 1)
}
