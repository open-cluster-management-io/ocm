package spoke

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"time"

	clusterv1client "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterv1informers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	"github.com/open-cluster-management/registration/pkg/spoke/hubclientcert"
	"github.com/open-cluster-management/registration/pkg/spoke/spokecluster"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/pflag"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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
	ComponentNamespace     string
	ClusterName            string
	AgentName              string
	BootstrapKubeconfig    string
	HubKubeconfigSecret    string
	HubKubeconfigDir       string
	SpokeExternalServerUrl string
}

// NewSpokeAgentOptions returns a SpokeAgentOptions
func NewSpokeAgentOptions() *SpokeAgentOptions {
	return &SpokeAgentOptions{
		HubKubeconfigSecret: "hub-kubeconfig-secret",
		HubKubeconfigDir:    "/spoke/hub-kubeconfig",
	}
}

// RunSpokeAgent starts the controllers on spoke agent to register to the hub.
//
// The spoke agent uses three kubeconfigs for different concerns:
// - The 'spoke' kubeconfig: used to communicate with the spoke cluster where
//   the agent is running.
// - The 'bootstrap' kubeconfig: used to communicate with the hub in order to
//   submit a CertificateSigningRequest, begin the join flow with the hub, and
//   to write the 'hub' kubeconfig.
// - The 'hub' kubeconfig: used to communicate with the hub using a signed
//   certificate from the hub.
//
// RunSpokeAgent handles the following scenarios:
//   #1. Bootstrap kubeconfig is valid and there is no valid hub kubeconfig in secret
//   #2. Both bootstrap kubeconfig and hub kubeconfig are valid
//   #3. Bootstrap kubeconfig is invalid (e.g. certificate expired) and hub kubeconfig is valid
//   #4. Neither bootstrap kubeconfig nor hub kubeconfig is valid
//
// A temporary ClientCertForHubController with bootstrap kubeconfig is created
// and started if the hub kubeconfig does not exist or is invalid and used to
// create a valid hub kubeconfig. Once the hub kubeconfig is valid, the
// temporary controller is stopped and the main controllers are started.
func (o *SpokeAgentOptions) RunSpokeAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	if err := o.Complete(); err != nil {
		klog.Fatal(err)
	}

	if err := o.Validate(); err != nil {
		klog.Fatal(err)
	}

	klog.Infof("Cluster name is %q and agent name is %q", o.ClusterName, o.AgentName)

	// create kube client and shared informer factory for spoke cluster
	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	spokeKubeInformerFactory := informers.NewSharedInformerFactory(spokeKubeClient, 10*time.Minute)

	// get spoke cluster CA bundle
	spokeClusterCABundle, err := getSpokeClusterCABundle(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// load bootstrap client config and create bootstrap clients
	bootstrapClientConfig, err := clientcmd.BuildConfigFromFlags("", o.BootstrapKubeconfig)
	if err != nil {
		return fmt.Errorf("unable to load bootstrap kubeconfig from file %q: %w", o.BootstrapKubeconfig, err)
	}
	bootstrapKubeClient, err := kubernetes.NewForConfig(bootstrapClientConfig)
	if err != nil {
		return err
	}
	bootstrapClusterClient, err := clusterv1client.NewForConfig(bootstrapClientConfig)
	if err != nil {
		return err
	}

	// start a SpokeClusterCreatingController to make sure there is a spoke cluster on hub cluster
	spokeClusterCreatingController := spokecluster.NewSpokeClusterCreatingController(
		o.ClusterName, o.SpokeExternalServerUrl,
		spokeClusterCABundle,
		bootstrapClusterClient,
		controllerContext.EventRecorder,
	)
	go spokeClusterCreatingController.Run(ctx, 1)

	// check if there already exists a valid client config for hub
	ok, err := o.hasValidHubClientConfig()
	if err != nil {
		return err
	}

	// create and start a ClientCertForHubController for spoke agent bootstrap to deal with scenario #1 and #4.
	// Running the bootstrap ClientCertForHubController is optional. If always run it no matter if there already
	// exists a valid client config for hub or not, the controller will be started and then stopped immediately
	// in scenario #2 and #3, which results in an error message in log: 'Observed a panic: timeout waiting for
	// informer cache'
	if !ok {
		// create a ClientCertForHubController for spoke agent bootstrap
		bootstrapInformerFactory := informers.NewSharedInformerFactory(bootstrapKubeClient, 10*time.Minute)

		clientCertForHubController := hubclientcert.NewClientCertForHubController(
			o.ClusterName, o.AgentName, o.ComponentNamespace, o.HubKubeconfigSecret,
			restclient.AnonymousClientConfig(bootstrapClientConfig),
			spokeKubeClient.CoreV1(),
			bootstrapKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
			bootstrapInformerFactory.Certificates().V1beta1().CertificateSigningRequests(),
			spokeKubeInformerFactory.Core().V1().Secrets(),
			controllerContext.EventRecorder,
			"BootstrapClientCertForHubController",
		)

		bootstrapCtx, stopBootstrap := context.WithCancel(ctx)

		go bootstrapInformerFactory.Start(bootstrapCtx.Done())
		go spokeKubeInformerFactory.Start(bootstrapCtx.Done())

		go clientCertForHubController.Run(bootstrapCtx, 1)

		// wait for the hub client config is ready.
		klog.Info("Waiting for hub client config and spoke cluster to be ready")
		if err := wait.PollImmediateInfinite(1*time.Second, o.hasValidHubClientConfig); err != nil {
			// TODO need run the bootstrap CSR forever to re-establish the client-cert if it is ever lost.
			stopBootstrap()
			return err
		}

		// stop the clientCertForHubController for bootstrap once the hub client config is ready
		stopBootstrap()
	}

	// create hub clients and shared informer factories from hub kube config
	hubClientConfig, err := clientcmd.BuildConfigFromFlags("", path.Join(o.HubKubeconfigDir, hubclientcert.KubeconfigFile))
	if err != nil {
		return err
	}

	hubKubeClient, err := kubernetes.NewForConfig(hubClientConfig)
	if err != nil {
		return err
	}

	hubClusterClient, err := clusterv1client.NewForConfig(hubClientConfig)
	if err != nil {
		return err
	}

	hubKubeInformerFactory := informers.NewSharedInformerFactory(hubKubeClient, 10*time.Minute)
	hubClusterInformerFactory := clusterv1informers.NewSharedInformerFactory(hubClusterClient, 10*time.Minute)

	controllerContext.EventRecorder.Event("HubClientConfigReady", "Client config for hub is ready.")

	// create another ClientCertForHubController for client certificate rotation
	clientCertForHubController := hubclientcert.NewClientCertForHubController(
		o.ClusterName, o.AgentName, o.ComponentNamespace, o.HubKubeconfigSecret,
		restclient.AnonymousClientConfig(hubClientConfig),
		spokeKubeClient.CoreV1(),
		hubKubeClient.CertificatesV1beta1().CertificateSigningRequests(),
		hubKubeInformerFactory.Certificates().V1beta1().CertificateSigningRequests(),
		spokeKubeInformerFactory.Core().V1().Secrets(),
		controllerContext.EventRecorder,
		"ClientCertForHubController",
	)

	// create SpokeClusterController to reconcile instances of SpokeCluster on the spoke cluster
	spokeClusterController := spokecluster.NewSpokeClusterController(
		o.ClusterName,
		hubClusterClient,
		hubClusterInformerFactory.Cluster().V1().SpokeClusters(),
		spokeKubeClient.Discovery(),
		spokeKubeInformerFactory.Core().V1().Nodes(),
		controllerContext.EventRecorder,
	)

	go hubKubeInformerFactory.Start(ctx.Done())
	go hubClusterInformerFactory.Start(ctx.Done())
	go spokeKubeInformerFactory.Start(ctx.Done())

	go clientCertForHubController.Run(ctx, 1)
	go spokeClusterController.Run(ctx, 1)

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
	fs.StringVar(&o.SpokeExternalServerUrl, "spoke-external-server-url", o.SpokeExternalServerUrl,
		"A reachable URL of spoke cluster api server for hub cluster.")
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

	if o.SpokeExternalServerUrl == "" {
		return errors.New("spoke cluster external api server url is empty")
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
	// if cluster name is not specified with input argument, try to load it from file
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

func getSpokeClusterCABundle(kubeConfig *rest.Config) ([]byte, error) {
	if kubeConfig.CAData != nil {
		return kubeConfig.CAData, nil
	}
	data, err := ioutil.ReadFile(kubeConfig.CAFile)
	if err != nil {
		return nil, err
	}
	return data, nil
}
