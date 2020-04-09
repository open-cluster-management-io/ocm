package spoke

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/open-cluster-management/registration/pkg/spoke/hubclientcert"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/pflag"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

const (
	defaultSpokeComponentNamespace = "open-cluster-management"
)

// AgentOptions holds configuration for spoke cluster agent
type AgentOptions struct {
	ClusterName                        string
	BootstrapKubeconfigSecretNamespace string
	BootstrapKubeconfigSecret          string
	HubKubeconfigSecret                string
	HubKubeconfigDir                   string
}

// NewAgentOptions returns an agent optons
func NewAgentOptions() *AgentOptions {
	return &AgentOptions{
		BootstrapKubeconfigSecret: "bootstrap-kubeconfig-secret",
		HubKubeconfigSecret:       "hub-kubeconfig-secret",
		HubKubeconfigDir:          "/spoke/hub-kubeconfig",
	}
}

// RunAgent starts the controllers on agent to register to hub.
func (o *AgentOptions) RunAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	if err := o.Validate(); err != nil {
		klog.Fatal(err)
	}

	if err := o.Complete(); err != nil {
		klog.Fatal(err)
	}

	// create kube client for spoke cluster
	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// load bootstrap clent config
	bootstrapClientConfig, err := hubclientcert.LoadClientConfigFromSecret(o.BootstrapKubeconfigSecretNamespace,
		o.BootstrapKubeconfigSecret, spokeKubeClient.CoreV1())
	if err != nil {
		return fmt.Errorf("unable to load bootstrap kubeconfig: %v", err)
	}

	// recover agent state
	spokeComponentNamespace := getSpokeComponentNamespace()
	clusterName, agentName, bootstrapped, err := hubclientcert.RecoverAgentState(spokeComponentNamespace,
		o.HubKubeconfigSecret, o.ClusterName, spokeKubeClient.CoreV1())
	if err != nil {
		return fmt.Errorf("unable to recover spoke agent state: %v", err)
	}

	clientCertForHubController, err := hubclientcert.NewClientCertForHubController(spokeComponentNamespace,
		o.HubKubeconfigSecret, o.HubKubeconfigDir, clusterName, agentName, bootstrapClientConfig,
		bootstrapped, spokeKubeClient.CoreV1())
	if err != nil {
		return err
	}

	initialHubKubeClient, err := kubernetes.NewForConfig(clientCertForHubController.GetInitialHubClientConfig())
	if err != nil {
		return fmt.Errorf("unable to create initial kube client for hub: %v", err)
	}
	csrInformer := informers.NewSharedInformerFactory(initialHubKubeClient, 10*time.Minute).Certificates().V1beta1().CertificateSigningRequests()

	go csrInformer.Informer().Run(ctx.Done())
	go clientCertForHubController.ToController(csrInformer, controllerContext.EventRecorder).Run(ctx, 1)

	// unblock until the client config for hub is ready
	_, err = clientCertForHubController.GetHubClientConfig()
	klog.Info("Client config for hub is ready.")

	<-ctx.Done()
	return nil
}

// AddFlags registers flags for Agent
func (o *AgentOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ClusterName, "cluster-name", o.ClusterName,
		"If non-empty, will use as cluster name instead of generated random name.")
	fs.StringVar(&o.BootstrapKubeconfigSecretNamespace, "bootstrap-kubeconfig-secret-namespace", o.BootstrapKubeconfigSecretNamespace,
		"The namespace of the bootstrap-kubeconfig-secret.")
	fs.StringVar(&o.BootstrapKubeconfigSecret, "bootstrap-kubeconfig-secret", o.BootstrapKubeconfigSecret,
		"The name of secret containing kubeconfig for spoke agent bootstrap.")
	fs.StringVar(&o.HubKubeconfigSecret, "hub-kubeconfig-secret", o.HubKubeconfigSecret,
		"The name of secret in component namespace storing kubeconfig for hub.")
	fs.StringVar(&o.HubKubeconfigDir, "hub-kubeconfig-dir", o.HubKubeconfigDir,
		"The mount path of hub-kubeconfig-secret in the container.")
}

// Validate verifies the inputs.
func (o *AgentOptions) Validate() error {
	if o.BootstrapKubeconfigSecretNamespace == "" {
		return errors.New("bootstrap-kubeconfig-secret-namespace is required")
	}
	return nil
}

// Complete fills in missing values.
func (o *AgentOptions) Complete() error {
	return nil
}

func getSpokeComponentNamespace() string {
	nsBytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return defaultSpokeComponentNamespace
	}
	return string(nsBytes)
}
