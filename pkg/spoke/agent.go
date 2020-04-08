package spoke

import (
	"context"
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
	defaultComponentNamespace = "open-cluster-management"
)

// AgentOptions holds configuration for spoke cluster agent
type AgentOptions struct {
	ClusterNameOverride       string
	BootstrapKubeconfigSecret string
	HubKubeconfigSecret       string
	HubKubeconfigDir          string
}

// NewAgentOptions returns an agent optons
func NewAgentOptions() *AgentOptions {
	return &AgentOptions{
		BootstrapKubeconfigSecret: "default/bootstrap-kubeconfig-secret",
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

	clientCertForHubController, err := hubclientcert.NewClientCertForHubController(o.HubKubeconfigSecret,
		o.BootstrapKubeconfigSecret, o.HubKubeconfigDir, o.ClusterNameOverride, spokeKubeClient.CoreV1())
	if err != nil {
		return err
	}

	initialHubKubeClient := clientCertForHubController.GetInitialHubKubeClient()
	csrInformer := informers.NewSharedInformerFactory(initialHubKubeClient, 10*time.Minute).Certificates().V1beta1().CertificateSigningRequests().Informer()

	go csrInformer.Run(ctx.Done())
	go clientCertForHubController.ToController(csrInformer, controllerContext.EventRecorder).Run(ctx, 1)

	// block until the client config for hub is ready
	_, err = clientCertForHubController.GetClientConfig()
	klog.Info("Client config for hub is ready.")

	<-ctx.Done()
	return nil
}

// AddFlags registers flags for Agent
func (o *AgentOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ClusterNameOverride, "cluster-name-override", o.ClusterNameOverride,
		"If non-empty, will use this string as cluster name instead of generated random name.")
	fs.StringVar(&o.BootstrapKubeconfigSecret, "bootstrap-kubeconfig-secret", o.BootstrapKubeconfigSecret,
		"The name of secret containing kubeconfig for spoke agent bootstrap in the format of namespace/name.")
	fs.StringVar(&o.HubKubeconfigSecret, "hub-kubeconfig-secret", o.HubKubeconfigSecret,
		"The name of secret in component namespace storing kubeconfig for hub.")
	fs.StringVar(&o.HubKubeconfigDir, "hub-kubeconfig-dir", o.HubKubeconfigDir,
		"The path of the directory in the container containing kubeconfig file for hub.")
}

// Validate verifies the inputs.
func (o *AgentOptions) Validate() error {
	return nil
}

// Complete fills in missing values.
func (o *AgentOptions) Complete() error {
	componentNamespace := getComponentNamespace()
	o.HubKubeconfigSecret = componentNamespace + "/" + o.HubKubeconfigSecret

	return nil
}

func getComponentNamespace() string {
	nsBytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return defaultComponentNamespace
	}
	return string(nsBytes)
}
