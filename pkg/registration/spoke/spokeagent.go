package spoke

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/clientcert"
	"open-cluster-management.io/ocm/pkg/registration/helpers"
	"open-cluster-management.io/ocm/pkg/registration/spoke/addon"
	"open-cluster-management.io/ocm/pkg/registration/spoke/lease"
	"open-cluster-management.io/ocm/pkg/registration/spoke/managedcluster"
	"open-cluster-management.io/ocm/pkg/registration/spoke/registration"
)

const (
	// spokeAgentNameLength is the length of the spoke agent name which is generated automatically
	spokeAgentNameLength = 5
	// defaultSpokeComponentNamespace is the default namespace in which the spoke agent is deployed
	defaultSpokeComponentNamespace = "open-cluster-management-agent"
)

// AddOnLeaseControllerSyncInterval is exposed so that integration tests can crank up the constroller sync speed.
// TODO if we register the lease informer to the lease controller, we need to increase this time
var AddOnLeaseControllerSyncInterval = 30 * time.Second

// SpokeAgentOptions holds configuration for spoke cluster agent
type SpokeAgentOptions struct {
	AgentOptions                *commonoptions.AgentOptions
	ComponentNamespace          string
	AgentName                   string
	BootstrapKubeconfig         string
	HubKubeconfigSecret         string
	HubKubeconfigDir            string
	SpokeExternalServerURLs     []string
	ClusterHealthCheckPeriod    time.Duration
	MaxCustomClusterClaims      int
	ClientCertExpirationSeconds int32
}

// NewSpokeAgentOptions returns a SpokeAgentOptions
func NewSpokeAgentOptions() *SpokeAgentOptions {
	return &SpokeAgentOptions{
		AgentOptions:             commonoptions.NewAgentOptions(),
		HubKubeconfigSecret:      "hub-kubeconfig-secret",
		HubKubeconfigDir:         "/spoke/hub-kubeconfig",
		ClusterHealthCheckPeriod: 1 * time.Minute,
		MaxCustomClusterClaims:   20,
	}
}

// RunSpokeAgent starts the controllers on spoke agent to register to the hub.
//
// There are two deploy mode for the registration agent: 'Default' mode and 'Detached' mode,
//   - In Default mode, the registration agent pod runs on the spoke/managed cluster.
//   - In Detached mode, the registration agent pod may run on a separated cluster from the
//     spoke/managed cluster, we define this cluster as 'management' cluster.
//
// The spoke agent uses four kubeconfigs for different concerns:
//   - The 'management' kubeconfig: used to communicate with the cluster where the agent pod
//     runs. In Default mode, it is the managed cluster's kubeconfig; in Detached mode, it is
//     the management cluster's kubeconfig.
//   - The 'spoke' kubeconfig: used to communicate with the spoke/managed cluster which will
//     be registered to the hub.
//   - The 'bootstrap' kubeconfig: used to communicate with the hub in order to
//     submit a CertificateSigningRequest, begin the join flow with the hub, and
//     to write the 'hub' kubeconfig.
//   - The 'hub' kubeconfig: used to communicate with the hub using a signed
//     certificate from the hub.
//
// RunSpokeAgent handles the following scenarios:
//
//	#1. Bootstrap kubeconfig is valid and there is no valid hub kubeconfig in secret
//	#2. Both bootstrap kubeconfig and hub kubeconfig are valid
//	#3. Bootstrap kubeconfig is invalid (e.g. certificate expired) and hub kubeconfig is valid
//	#4. Neither bootstrap kubeconfig nor hub kubeconfig is valid
//
// A temporary ClientCertForHubController with bootstrap kubeconfig is created
// and started if the hub kubeconfig does not exist or is invalid and used to
// create a valid hub kubeconfig. Once the hub kubeconfig is valid, the
// temporary controller is stopped and the main controllers are started.
func (o *SpokeAgentOptions) RunSpokeAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	kubeConfig := controllerContext.KubeConfig

	// load spoke client config and create spoke clients,
	// the registration agent may not running in the spoke/managed cluster.
	spokeClientConfig, err := o.AgentOptions.SpokeKubeConfig(kubeConfig)
	if err != nil {
		return err
	}

	spokeKubeClient, err := kubernetes.NewForConfig(spokeClientConfig)
	if err != nil {
		return err
	}

	spokeClusterClient, err := clusterv1client.NewForConfig(spokeClientConfig)
	if err != nil {
		return err
	}

	return o.RunSpokeAgentWithSpokeInformers(
		ctx,
		kubeConfig,
		spokeClientConfig,
		spokeKubeClient,
		informers.NewSharedInformerFactory(spokeKubeClient, 10*time.Minute),
		clusterv1informers.NewSharedInformerFactory(spokeClusterClient, 10*time.Minute),
		controllerContext.EventRecorder,
	)
}

func (o *SpokeAgentOptions) RunSpokeAgentWithSpokeInformers(ctx context.Context,
	kubeConfig, spokeClientConfig *rest.Config,
	spokeKubeClient kubernetes.Interface,
	spokeKubeInformerFactory informers.SharedInformerFactory,
	spokeClusterInformerFactory clusterv1informers.SharedInformerFactory,
	recorder events.Recorder) error {
	klog.Infof("Cluster name is %q and agent name is %q", o.AgentOptions.SpokeClusterName, o.AgentName)

	// create management kube client
	managementKubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	// the hub kubeconfig secret stored in the cluster where the agent pod runs
	if err := o.Complete(managementKubeClient.CoreV1(), ctx, recorder); err != nil {
		klog.Fatal(err)
	}

	if err := o.Validate(); err != nil {
		klog.Fatal(err)
	}

	// get spoke cluster CA bundle
	spokeClusterCABundle, err := o.getSpokeClusterCABundle(spokeClientConfig)
	if err != nil {
		return err
	}

	// create a shared informer factory with specific namespace for the management cluster.
	namespacedManagementKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		managementKubeClient, 10*time.Minute, informers.WithNamespace(o.ComponentNamespace))

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
	spokeClusterCreatingController := registration.NewManagedClusterCreatingController(
		o.AgentOptions.SpokeClusterName, o.SpokeExternalServerURLs,
		spokeClusterCABundle,
		bootstrapClusterClient,
		recorder,
	)
	go spokeClusterCreatingController.Run(ctx, 1)

	hubKubeconfigSecretController := registration.NewHubKubeconfigSecretController(
		o.HubKubeconfigDir, o.ComponentNamespace, o.HubKubeconfigSecret,
		// the hub kubeconfig secret stored in the cluster where the agent pod runs
		managementKubeClient.CoreV1(),
		namespacedManagementKubeInformerFactory.Core().V1().Secrets(),
		recorder,
	)
	go hubKubeconfigSecretController.Run(ctx, 1)
	go namespacedManagementKubeInformerFactory.Start(ctx.Done())

	// check if there already exists a valid client config for hub
	ok, err := o.hasValidHubClientConfig(ctx)
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
		// the bootstrap informers are supposed to be terminated after completing the bootstrap process.
		bootstrapInformerFactory := informers.NewSharedInformerFactory(bootstrapKubeClient, 10*time.Minute)
		bootstrapNamespacedManagementKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(
			managementKubeClient, 10*time.Minute, informers.WithNamespace(o.ComponentNamespace))

		// create a kubeconfig with references to the key/cert files in the same secret
		kubeconfig := clientcert.BuildKubeconfig(bootstrapClientConfig, clientcert.TLSCertFile, clientcert.TLSKeyFile)
		kubeconfigData, err := clientcmd.Write(kubeconfig)
		if err != nil {
			return err
		}

		csrControl, err := clientcert.NewCSRControl(bootstrapInformerFactory.Certificates(), bootstrapKubeClient)
		if err != nil {
			return err
		}

		controllerName := fmt.Sprintf("BootstrapClientCertController@cluster:%s", o.AgentOptions.SpokeClusterName)
		clientCertForHubController := registration.NewClientCertForHubController(
			o.AgentOptions.SpokeClusterName, o.AgentName, o.ComponentNamespace, o.HubKubeconfigSecret,
			kubeconfigData,
			// store the secret in the cluster where the agent pod runs
			bootstrapNamespacedManagementKubeInformerFactory.Core().V1().Secrets(),
			csrControl,
			o.ClientCertExpirationSeconds,
			managementKubeClient,
			registration.GenerateBootstrapStatusUpdater(),
			recorder,
			controllerName,
		)

		bootstrapCtx, stopBootstrap := context.WithCancel(ctx)

		go bootstrapInformerFactory.Start(bootstrapCtx.Done())
		go bootstrapNamespacedManagementKubeInformerFactory.Start(bootstrapCtx.Done())

		go clientCertForHubController.Run(bootstrapCtx, 1)

		// wait for the hub client config is ready.
		klog.Info("Waiting for hub client config and managed cluster to be ready")
		if err := wait.PollUntilContextCancel(bootstrapCtx, 1*time.Second, true, o.hasValidHubClientConfig); err != nil {
			// TODO need run the bootstrap CSR forever to re-establish the client-cert if it is ever lost.
			stopBootstrap()
			return err
		}

		// stop the clientCertForHubController for bootstrap once the hub client config is ready
		stopBootstrap()
	}

	// create hub clients and shared informer factories from hub kube config
	hubClientConfig, err := clientcmd.BuildConfigFromFlags("", path.Join(o.HubKubeconfigDir, clientcert.KubeconfigFile))
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

	addOnClient, err := addonclient.NewForConfig(hubClientConfig)
	if err != nil {
		return err
	}

	hubKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		hubKubeClient,
		10*time.Minute,
		informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			listOptions.LabelSelector = fmt.Sprintf("%s=%s", clusterv1.ClusterNameLabelKey, o.AgentOptions.SpokeClusterName)
		}),
	)
	addOnInformerFactory := addoninformers.NewSharedInformerFactoryWithOptions(
		addOnClient,
		10*time.Minute,
		addoninformers.WithNamespace(o.AgentOptions.SpokeClusterName),
	)
	// create a cluster informer factory with name field selector because we just need to handle the current spoke cluster
	hubClusterInformerFactory := clusterv1informers.NewSharedInformerFactoryWithOptions(
		hubClusterClient,
		10*time.Minute,
		clusterv1informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			listOptions.FieldSelector = fields.OneTermEqualSelector("metadata.name", o.AgentOptions.SpokeClusterName).String()
		}),
	)

	recorder.Event("HubClientConfigReady", "Client config for hub is ready.")

	// create a kubeconfig with references to the key/cert files in the same secret
	kubeconfig := clientcert.BuildKubeconfig(hubClientConfig, clientcert.TLSCertFile, clientcert.TLSKeyFile)
	kubeconfigData, err := clientcmd.Write(kubeconfig)
	if err != nil {
		return err
	}

	csrControl, err := clientcert.NewCSRControl(hubKubeInformerFactory.Certificates(), hubKubeClient)
	if err != nil {
		return err
	}

	// create another ClientCertForHubController for client certificate rotation
	controllerName := fmt.Sprintf("ClientCertController@cluster:%s", o.AgentOptions.SpokeClusterName)
	clientCertForHubController := registration.NewClientCertForHubController(
		o.AgentOptions.SpokeClusterName, o.AgentName, o.ComponentNamespace, o.HubKubeconfigSecret,
		kubeconfigData,
		namespacedManagementKubeInformerFactory.Core().V1().Secrets(),
		csrControl,
		o.ClientCertExpirationSeconds,
		managementKubeClient,
		registration.GenerateStatusUpdater(
			hubClusterClient,
			hubClusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
			o.AgentOptions.SpokeClusterName),
		recorder,
		controllerName,
	)
	if err != nil {
		return err
	}

	// create ManagedClusterLeaseController to keep the spoke cluster heartbeat
	managedClusterLeaseController := lease.NewManagedClusterLeaseController(
		o.AgentOptions.SpokeClusterName,
		hubKubeClient,
		hubClusterInformerFactory.Cluster().V1().ManagedClusters(),
		recorder,
	)

	// create NewManagedClusterStatusController to update the spoke cluster status
	managedClusterHealthCheckController := managedcluster.NewManagedClusterStatusController(
		o.AgentOptions.SpokeClusterName,
		hubClusterClient,
		hubClusterInformerFactory.Cluster().V1().ManagedClusters(),
		spokeKubeClient.Discovery(),
		spokeClusterInformerFactory.Cluster().V1alpha1().ClusterClaims(),
		spokeKubeInformerFactory.Core().V1().Nodes(),
		o.MaxCustomClusterClaims,
		o.ClusterHealthCheckPeriod,
		recorder,
	)

	var addOnLeaseController factory.Controller
	var addOnRegistrationController factory.Controller
	if features.DefaultSpokeRegistrationMutableFeatureGate.Enabled(ocmfeature.AddonManagement) {
		addOnLeaseController = addon.NewManagedClusterAddOnLeaseController(
			o.AgentOptions.SpokeClusterName,
			addOnClient,
			addOnInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
			hubKubeClient.CoordinationV1(),
			managementKubeClient.CoordinationV1(),
			spokeKubeClient.CoordinationV1(),
			AddOnLeaseControllerSyncInterval, //TODO: this interval time should be allowed to change from outside
			recorder,
		)

		addOnRegistrationController = addon.NewAddOnRegistrationController(
			o.AgentOptions.SpokeClusterName,
			o.AgentName,
			kubeconfigData,
			addOnClient,
			managementKubeClient,
			spokeKubeClient,
			csrControl,
			addOnInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
			recorder,
		)
	}

	go hubKubeInformerFactory.Start(ctx.Done())
	go hubClusterInformerFactory.Start(ctx.Done())
	go namespacedManagementKubeInformerFactory.Start(ctx.Done())
	go addOnInformerFactory.Start(ctx.Done())

	go spokeKubeInformerFactory.Start(ctx.Done())
	if features.DefaultSpokeRegistrationMutableFeatureGate.Enabled(ocmfeature.ClusterClaim) {
		go spokeClusterInformerFactory.Start(ctx.Done())
	}

	go clientCertForHubController.Run(ctx, 1)
	go managedClusterLeaseController.Run(ctx, 1)
	go managedClusterHealthCheckController.Run(ctx, 1)
	if features.DefaultSpokeRegistrationMutableFeatureGate.Enabled(ocmfeature.AddonManagement) {
		go addOnLeaseController.Run(ctx, 1)
		go addOnRegistrationController.Run(ctx, 1)
	}

	<-ctx.Done()
	return nil
}

// AddFlags registers flags for Agent
func (o *SpokeAgentOptions) AddFlags(fs *pflag.FlagSet) {
	features.DefaultSpokeRegistrationMutableFeatureGate.AddFlag(fs)
	o.AgentOptions.AddFlags(fs)
	fs.StringVar(&o.BootstrapKubeconfig, "bootstrap-kubeconfig", o.BootstrapKubeconfig,
		"The path of the kubeconfig file for agent bootstrap.")
	fs.StringVar(&o.HubKubeconfigSecret, "hub-kubeconfig-secret", o.HubKubeconfigSecret,
		"The name of secret in component namespace storing kubeconfig for hub.")
	fs.StringVar(&o.HubKubeconfigDir, "hub-kubeconfig-dir", o.HubKubeconfigDir,
		"The mount path of hub-kubeconfig-secret in the container.")
	fs.StringArrayVar(&o.SpokeExternalServerURLs, "spoke-external-server-urls", o.SpokeExternalServerURLs,
		"A list of reachable spoke cluster api server URLs for hub cluster.")
	fs.DurationVar(&o.ClusterHealthCheckPeriod, "cluster-healthcheck-period", o.ClusterHealthCheckPeriod,
		"The period to check managed cluster kube-apiserver health")
	fs.IntVar(&o.MaxCustomClusterClaims, "max-custom-cluster-claims", o.MaxCustomClusterClaims,
		"The max number of custom cluster claims to expose.")
	fs.Int32Var(&o.ClientCertExpirationSeconds, "client-cert-expiration-seconds", o.ClientCertExpirationSeconds,
		"The requested duration in seconds of validity of the issued client certificate. If this is not set, "+
			"the value of --cluster-signing-duration command-line flag of the kube-controller-manager will be used.")
}

// Validate verifies the inputs.
func (o *SpokeAgentOptions) Validate() error {
	if o.BootstrapKubeconfig == "" {
		return errors.New("bootstrap-kubeconfig is required")
	}

	if err := o.AgentOptions.Validate(); err != nil {
		return err
	}

	if o.AgentName == "" {
		return errors.New("agent name is empty")
	}

	// if SpokeExternalServerURLs is specified we validate every URL in it, we expect the spoke external server URL is https
	if len(o.SpokeExternalServerURLs) != 0 {
		for _, serverURL := range o.SpokeExternalServerURLs {
			if !helpers.IsValidHTTPSURL(serverURL) {
				return fmt.Errorf("%q is invalid", serverURL)
			}
		}
	}

	if o.ClusterHealthCheckPeriod <= 0 {
		return errors.New("cluster healthcheck period must greater than zero")
	}

	if o.ClientCertExpirationSeconds != 0 && o.ClientCertExpirationSeconds < 3600 {
		return errors.New("client certificate expiration seconds must greater or qual to 3600")
	}

	return nil
}

// Complete fills in missing values.
func (o *SpokeAgentOptions) Complete(coreV1Client corev1client.CoreV1Interface, ctx context.Context, recorder events.Recorder) error {
	// get component namespace of spoke agent
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		o.ComponentNamespace = defaultSpokeComponentNamespace
	} else {
		o.ComponentNamespace = string(nsBytes)
	}

	// dump data in hub kubeconfig secret into file system if it exists
	err = registration.DumpSecret(coreV1Client, o.ComponentNamespace, o.HubKubeconfigSecret,
		o.HubKubeconfigDir, ctx, recorder)
	if err != nil {
		return err
	}

	// load or generate cluster/agent names
	o.AgentOptions.SpokeClusterName, o.AgentName = o.getOrGenerateClusterAgentNames()

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

// hasValidHubClientConfig returns ture if all the conditions below are met:
//  1. KubeconfigFile exists;
//  2. TLSKeyFile exists;
//  3. TLSCertFile exists;
//  4. Certificate in TLSCertFile is issued for the current cluster/agent;
//  5. Certificate in TLSCertFile is not expired;
//
// Normally, KubeconfigFile/TLSKeyFile/TLSCertFile will be created once the bootstrap process
// completes. Changing the name of the cluster will make the existing hub kubeconfig invalid,
// because certificate in TLSCertFile is issued to a specific cluster/agent.
func (o *SpokeAgentOptions) hasValidHubClientConfig(ctx context.Context) (bool, error) {
	kubeconfigPath := path.Join(o.HubKubeconfigDir, clientcert.KubeconfigFile)
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		klog.V(4).Infof("Kubeconfig file %q not found", kubeconfigPath)
		return false, nil
	}

	keyPath := path.Join(o.HubKubeconfigDir, clientcert.TLSKeyFile)
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		klog.V(4).Infof("TLS key file %q not found", keyPath)
		return false, nil
	}

	certPath := path.Join(o.HubKubeconfigDir, clientcert.TLSCertFile)
	certData, err := os.ReadFile(path.Clean(certPath))
	if err != nil {
		klog.V(4).Infof("Unable to load TLS cert file %q", certPath)
		return false, nil
	}

	// check if the tls certificate is issued for the current cluster/agent
	clusterName, agentName, err := registration.GetClusterAgentNamesFromCertificate(certData)
	if err != nil {
		return false, nil
	}
	if clusterName != o.AgentOptions.SpokeClusterName || agentName != o.AgentName {
		klog.V(4).Infof("Certificate in file %q is issued for agent %q instead of %q",
			certPath, fmt.Sprintf("%s:%s", clusterName, agentName),
			fmt.Sprintf("%s:%s", o.AgentOptions.SpokeClusterName, o.AgentName))
		return false, nil
	}

	return clientcert.IsCertificateValid(certData, nil)
}

// getOrGenerateClusterAgentNames returns cluster name and agent name.
// Rules for picking up cluster name:
//   1. Use cluster name from input arguments if 'cluster-name' is specified;
//   2. Parse cluster name from the common name of the certification subject if the certification exists;
//   3. Fallback to cluster name in the mounted secret if it exists;
//   4. TODO: Read cluster name from openshift struct if the agent is running in an openshift cluster;
//   5. Generate a random cluster name then;

// Rules for picking up agent name:
//  1. Parse agent name from the common name of the certification subject if the certification exists;
//  2. Fallback to agent name in the mounted secret if it exists;
//  3. Generate a random agent name then;
func (o *SpokeAgentOptions) getOrGenerateClusterAgentNames() (string, string) {
	// try to load cluster/agent name from tls certification
	var clusterNameInCert, agentNameInCert string
	certPath := path.Join(o.HubKubeconfigDir, clientcert.TLSCertFile)
	certData, certErr := os.ReadFile(path.Clean(certPath))
	if certErr == nil {
		clusterNameInCert, agentNameInCert, _ = registration.GetClusterAgentNamesFromCertificate(certData)
	}

	clusterName := o.AgentOptions.SpokeClusterName
	// if cluster name is not specified with input argument, try to load it from file
	if clusterName == "" {
		// TODO, read cluster name from openshift struct if the spoke agent is running in an openshift cluster

		// and then load the cluster name from the mounted secret
		clusterNameFilePath := path.Join(o.HubKubeconfigDir, clientcert.ClusterNameFile)
		clusterNameBytes, err := os.ReadFile(path.Clean(clusterNameFilePath))
		switch {
		case len(clusterNameInCert) > 0:
			// use cluster name loaded from the tls certification
			clusterName = clusterNameInCert
			if clusterNameInCert != string(clusterNameBytes) {
				klog.Warningf("Use cluster name %q in certification instead of %q in the mounted secret", clusterNameInCert, string(clusterNameBytes))
			}
		case err == nil:
			// use cluster name load from the mounted secret
			clusterName = string(clusterNameBytes)
		default:
			// generate random cluster name
			clusterName = generateClusterName()
		}
	}

	// try to load agent name from the mounted secret
	agentNameFilePath := path.Join(o.HubKubeconfigDir, clientcert.AgentNameFile)
	agentNameBytes, err := os.ReadFile(path.Clean(agentNameFilePath))
	var agentName string
	switch {
	case len(agentNameInCert) > 0:
		// use agent name loaded from the tls certification
		agentName = agentNameInCert
		if agentNameInCert != string(agentNameBytes) {
			klog.Warningf("Use agent name %q in certification instead of %q in the mounted secret", agentNameInCert, string(agentNameBytes))
		}
	case err == nil:
		// use agent name loaded from the mounted secret
		agentName = string(agentNameBytes)
	default:
		// generate random agent name
		agentName = generateAgentName()
	}

	return clusterName, agentName
}

// getSpokeClusterCABundle returns the spoke cluster Kubernetes client CA data when SpokeExternalServerURLs is specified
func (o *SpokeAgentOptions) getSpokeClusterCABundle(kubeConfig *rest.Config) ([]byte, error) {
	if len(o.SpokeExternalServerURLs) == 0 {
		return nil, nil
	}
	if kubeConfig.CAData != nil {
		return kubeConfig.CAData, nil
	}
	data, err := os.ReadFile(kubeConfig.CAFile)
	if err != nil {
		return nil, err
	}
	return data, nil
}
