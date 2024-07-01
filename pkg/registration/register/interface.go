package register

import (
	"context"
	"os"

	"github.com/openshift/library-go/pkg/operator/events"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
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

	BuildKubeConfigFromBootstrap(config *clientcmdapi.Config) (*clientcmdapi.Config, error)

	Start(ctx context.Context,
		name string,
		statusUpdater StatusUpdateFunc,
		recorder events.Recorder,
		secretOpt SecretOption,
		opt any,
		additionalData map[string][]byte)
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
	additionalSercretData := map[string][]byte{}
	if secretOption.BootStrapKubeConfig != nil {
		kubeConfig, err := r.driver.BuildKubeConfigFromBootstrap(secretOption.BootStrapKubeConfig)
		if err != nil {
			utilruntime.Must(err)
		}
		if kubeConfig != nil {
			kubeconfigData, err := clientcmd.Write(*kubeConfig)
			if err != nil {
				utilruntime.Must(err)
			}
			additionalSercretData[KubeconfigFile] = kubeconfigData
		}
	}

	if len(secretOption.ClusterName) > 0 {
		additionalSercretData[ClusterNameFile] = []byte(secretOption.ClusterName)
	}

	if len(secretOption.AgentName) > 0 {
		additionalSercretData[AgentNameFile] = []byte(secretOption.AgentName)
	}

	r.driver.Start(ctx, name, statusUpdater, recorder, secretOption, option, additionalSercretData)
}
