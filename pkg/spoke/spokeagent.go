package spoke

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/open-cluster-management/registration/pkg/spoke/hubclientcert"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/spf13/pflag"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

const (
	// spokeAgentNameLength is the length of the spoke agent name which is generated automatically
	spokeAgentNameLength = 5
	// defaultSpokeComponentNamespace is the default namespace in which the spoke agent is deployed
	defaultSpokeComponentNamespace = "open-cluster-management"
)

// SpokeAgentOptions holds configuration for spoke cluster agent
type SpokeAgentOptions struct {
	ComponentNamespace  string
	ClusterName         string
	AgentName           string
	BootstrapKubeconfig string
	HubKubeconfigSecret string
	HubKubeconfigDir    string
}

// NewSpokeAgentOptions returns a SpokeAgentOptions
func NewSpokeAgentOptions() *SpokeAgentOptions {
	return &SpokeAgentOptions{
		HubKubeconfigSecret: "hub-kubeconfig-secret",
		HubKubeconfigDir:    "/spoke/hub-kubeconfig",
	}
}

// RunSpokeAgent starts the controllers on spoke agent to register to hub.
// It handles the following scenarios:
//   #1. Bootstrap kubeconfig is valid and there is no valid hub kubeconfig in secret
//   #2. Both bootstrap kubeconfig and hub kubeconfig are valid
//   #3. Bootstrap kubeconfig is invalid (e.g. certificte expired) and hub kubeconfig is valid
//   #4. Neither bootstrap kubeconfig nor hub kubeconfig is valid
// A temporary ClientCertForHubController with bootstrap kubeconfig is created and started if
// hub kubeconfig does not exists or is invalid. It will be stopped once client config for hub
// is ready. And then run other controllers, including another ClientCertForHubController which
// is created for client certificate rotation.
func (o *SpokeAgentOptions) RunSpokeAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	if err := o.Complete(); err != nil {
		klog.Fatal(err)
	}

	if err := o.Validate(); err != nil {
		klog.Fatal(err)
	}

	klog.Infof("Cluster name is %q", o.ClusterName)
	klog.V(4).Infof("Agent name is %q", o.AgentName)

	// create kube client for spoke cluster
	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// check if there already exists a valid client config for hub
	ok, err := o.hasValidHubClientConfig()
	if err != nil {
		return err
	}

	// create and start a ClientCertForHubController for spoke agent bootstrap to deal with scenario #1 and #4.
	// Running the bootsrap ClientCertForHubController is optional. If always run it no matter if there already
	// exists a valid client config for hub or not, the controller will be started and then stopped immediately
	// in scenario #2 and #3, which results in an error message in log: 'Observed a panic: timeout waiting for
	// informer cache'
	var stopBootstrap context.CancelFunc
	if !ok {
		// load bootstrap clent config
		bootstrapClientConfig, err := hubclientcert.LoadClientConfig(o.BootstrapKubeconfig)
		if err != nil {
			return fmt.Errorf("unable to load bootstrap kubeconfig from file %q: %v", o.BootstrapKubeconfig, err)
		}

		// create and start a ClientCertForHubController for spoke agent bootstrap
		hubCSRInformer, spokeSecretInformer, clientCertForHubController, err := o.buildClientCertForHubController(o.ClusterName, o.AgentName,
			bootstrapClientConfig, spokeKubeClient, controllerContext.EventRecorder, "BoostrapClientCertForHubController")
		if err != nil {
			return err
		}
		var bootstrapCtx context.Context
		bootstrapCtx, stopBootstrap = context.WithCancel(ctx)
		go hubCSRInformer.Run(bootstrapCtx.Done())
		go spokeSecretInformer.Run(bootstrapCtx.Done())
		go clientCertForHubController.Run(bootstrapCtx, 1)
	}

	// wait for the client config for hub is ready
	klog.Info("Waiting for client config for hub to be ready")
	err = wait.PollImmediateInfinite(1*time.Second, o.hasValidHubClientConfig)
	if err != nil {
		if stopBootstrap != nil {
			stopBootstrap()
		}
		return err
	}

	// stop the ClientCertForHubController for bootstrap once the client config for hub is ready
	if stopBootstrap != nil {
		stopBootstrap()
	}

	kubeconfigPath := path.Join(o.HubKubeconfigDir, hubclientcert.KubeconfigFile)
	hubClientConfig, err := hubclientcert.LoadClientConfig(kubeconfigPath)
	if err != nil {
		return err
	}
	controllerContext.EventRecorder.Event("HubClientConfigReady", "Client config for hub is ready.")
	// build and start another ClientCertForHubController for client certificate rotation
	hubCSRInformer, spokeSecretInformer, clientCertForHubController, err := o.buildClientCertForHubController(o.ClusterName, o.AgentName,
		hubClientConfig, spokeKubeClient, controllerContext.EventRecorder, "")
	if err != nil {
		return err
	}

	go hubCSRInformer.Run(ctx.Done())
	go spokeSecretInformer.Run(ctx.Done())
	go clientCertForHubController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}

// AddFlags registers flags for Agent
func (o *SpokeAgentOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ClusterName, "cluster-name", o.ClusterName,
		"If non-empty, will use as cluster name instead of generated random name.")
	fs.StringVar(&o.BootstrapKubeconfig, "bootstrap-kubeconfig", o.BootstrapKubeconfig,
		"The path of the kubeconfig file for spoke agent bootstrap.")
	fs.StringVar(&o.HubKubeconfigSecret, "hub-kubeconfig-secret", o.HubKubeconfigSecret,
		"The name of secret in component namespace storing kubeconfig for hub.")
	fs.StringVar(&o.HubKubeconfigDir, "hub-kubeconfig-dir", o.HubKubeconfigDir,
		"The mount path of hub-kubeconfig-secret in the container.")
}

// Validate verifies the inputs.
func (o *SpokeAgentOptions) Validate() error {
	if o.BootstrapKubeconfig == "" {
		return errors.New("bootstrap-kubeconfig is required")
	}

	if o.ClusterName == "" {
		return errors.New("cluster name is empty")
	}

	if o.AgentName == "" {
		return errors.New("agent name is empty")
	}

	return nil
}

// Complete fills in missing values.
func (o *SpokeAgentOptions) Complete() error {
	// get component namespace of spoke agent
	nsBytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		o.ComponentNamespace = defaultSpokeComponentNamespace
	} else {
		o.ComponentNamespace = string(nsBytes)
	}

	// load or generate cluster/agent names
	o.ClusterName, o.AgentName = o.getOrGenerateClusterAgentNames()

	return nil
}

// generateClusterName generates a name for spoke cluster
func generateClusterName() string {
	return string(uuid.NewUUID())
}

// generateAgentName generates a random name for spoke cluster agent
func generateAgentName() string {
	return utilrand.String(spokeAgentNameLength)
}

// buildClientCertForHubController creates and returns a csr informer and a ClientCertForHubController
func (o *SpokeAgentOptions) buildClientCertForHubController(clusterName, agentName string,
	initialHubClientConfig *restclient.Config, spokeKubeClient kubernetes.Interface,
	recorder events.Recorder, controllerNameOverride string) (cache.SharedIndexInformer, cache.SharedIndexInformer, factory.Controller, error) {
	initialHubKubeClient, err := kubernetes.NewForConfig(initialHubClientConfig)
	if err != nil {
		return nil, nil, nil, err
	}

	// create an informer for csrs on hub cluster
	hubCSRInformer := informers.NewSharedInformerFactory(initialHubKubeClient,
		10*time.Minute).Certificates().V1beta1().CertificateSigningRequests()

	// create an informer for secrets on spoke cluster
	spokeSecretInformer := informers.NewSharedInformerFactory(spokeKubeClient,
		10*time.Minute).Core().V1().Secrets()

	clientCertForHubController, err := hubclientcert.NewClientCertForHubController(clusterName, agentName,
		o.ComponentNamespace, o.HubKubeconfigSecret, restclient.AnonymousClientConfig(initialHubClientConfig),
		spokeKubeClient.CoreV1(), initialHubKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
		hubCSRInformer, spokeSecretInformer, recorder, controllerNameOverride)
	if err != nil {
		return nil, nil, nil, err
	}
	return hubCSRInformer.Informer(), spokeSecretInformer.Informer(), clientCertForHubController, nil
}

// hasValidHubClientConfig returns ture if the conditions below are met:
//   1. KubeconfigFile exists
//   2. TLSKeyFile exists
//   3. TLSCertFile exists and the certificate is not expired
func (o *SpokeAgentOptions) hasValidHubClientConfig() (bool, error) {
	kubeconfigPath := path.Join(o.HubKubeconfigDir, hubclientcert.KubeconfigFile)
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		klog.V(4).Infof("Kubeconfig file %q not found", kubeconfigPath)
		return false, nil
	}

	keyPath := path.Join(o.HubKubeconfigDir, hubclientcert.TLSKeyFile)
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		klog.V(4).Infof("TLS key file %q not found", keyPath)
		return false, nil
	}

	certPath := path.Join(o.HubKubeconfigDir, hubclientcert.TLSCertFile)
	certData, err := ioutil.ReadFile(certPath)
	if err != nil {
		klog.V(4).Infof("Unable to load TLS cert file %q", certPath)
		return false, nil
	}

	return hubclientcert.IsCertificateValid(certData)
}

// getOrGenerateClusterAgentNames returns cluster name and agent name.
// Rules for picking up cluster/agent names:
//
//   1. take clusterName from input arguments if it is not empty
//   2. TODO: read cluster name from openshift struct if the spoke agent is running in an openshift cluster
//   3. take cluster/agent names from the mounted secret if they exist
//   4. generate random cluster/agent names then
func (o *SpokeAgentOptions) getOrGenerateClusterAgentNames() (string, string) {
	clusterName := o.ClusterName
	// if cluster name is not specified with nput argument, try to load it from file
	if clusterName == "" {
		// TODO, read cluster name from openshift struct if the spoke agent is running in an openshift cluster

		// and then load the cluster name from the mounted secret
		clusterNameFilePath := path.Join(o.HubKubeconfigDir, hubclientcert.ClusterNameFile)
		clusterNameBytes, err := ioutil.ReadFile(clusterNameFilePath)
		if err != nil {
			// generate random cluster name if faild
			clusterName = generateClusterName()
		} else {
			clusterName = string(clusterNameBytes)
		}
	}

	// try to load agent name from the mounted secret
	agentNameFilePath := path.Join(o.HubKubeconfigDir, hubclientcert.AgentNameFile)
	agentNameBytes, err := ioutil.ReadFile(agentNameFilePath)
	var agentName string
	if err != nil {
		// generate random agent name if faild
		agentName = generateAgentName()
	} else {
		agentName = string(agentNameBytes)
	}

	return clusterName, agentName
}
