package spoke

import (
	"context"
	"fmt"
	"os"
	"path"
	"reflect"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterscheme "open-cluster-management.io/api/client/cluster/clientset/versioned/scheme"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/clientcert"
	"open-cluster-management.io/ocm/pkg/registration/spoke/addon"
	"open-cluster-management.io/ocm/pkg/registration/spoke/lease"
	"open-cluster-management.io/ocm/pkg/registration/spoke/managedcluster"
	"open-cluster-management.io/ocm/pkg/registration/spoke/registration"
)

// AddOnLeaseControllerSyncInterval is exposed so that integration tests can crank up the constroller sync speed.
// TODO if we register the lease informer to the lease controller, we need to increase this time
var AddOnLeaseControllerSyncInterval = 30 * time.Second

type SpokeAgentConfig struct {
	agentOptions       *commonoptions.AgentOptions
	registrationOption *SpokeAgentOptions
}

// NewSpokeAgentConfig returns a SpokeAgentConfig
func NewSpokeAgentConfig(commonOpts *commonoptions.AgentOptions, opts *SpokeAgentOptions) *SpokeAgentConfig {
	return &SpokeAgentConfig{
		agentOptions:       commonOpts,
		registrationOption: opts,
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
//
// The agent will be restarted once any of the following happens:
//   - the bootstrap hub kubeconfig changes (updated/deleted);
//   - the client certificate referenced by the hub kubeconfig become expired (Return failure when
//     checking the health of the agent);
func (o *SpokeAgentConfig) RunSpokeAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	kubeConfig := controllerContext.KubeConfig

	// load spoke client config and create spoke clients,
	// the registration agent may not running in the spoke/managed cluster.
	spokeClientConfig, err := o.agentOptions.SpokeKubeConfig(kubeConfig)
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

func (o *SpokeAgentConfig) RunSpokeAgentWithSpokeInformers(ctx context.Context,
	kubeConfig, spokeClientConfig *rest.Config,
	spokeKubeClient kubernetes.Interface,
	spokeKubeInformerFactory informers.SharedInformerFactory,
	spokeClusterInformerFactory clusterv1informers.SharedInformerFactory,
	recorder events.Recorder) error {
	logger := klog.FromContext(ctx)
	logger.Info("Cluster name and agent ID", "clusterName", o.agentOptions.SpokeClusterName, "agentID", o.agentOptions.AgentID)

	// create management kube client
	managementKubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	// dump data in hub kubeconfig secret into file system if it exists
	err = registration.DumpSecret(
		managementKubeClient.CoreV1(), o.agentOptions.ComponentNamespace, o.registrationOption.HubKubeconfigSecret,
		o.agentOptions.HubKubeconfigDir, ctx, recorder)
	if err != nil {
		return err
	}

	if err := o.registrationOption.Validate(); err != nil {
		logger.Error(err, "Error during Validating")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	if err := o.agentOptions.Complete(); err != nil {
		logger.Error(err, "Error during Complete")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	if err := o.agentOptions.Validate(); err != nil {
		logger.Error(err, "Error during Validating")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	// get spoke cluster CA bundle
	spokeClusterCABundle, err := o.getSpokeClusterCABundle(spokeClientConfig)
	if err != nil {
		return err
	}

	// create a shared informer factory with specific namespace for the management cluster.
	namespacedManagementKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		managementKubeClient, 10*time.Minute, informers.WithNamespace(o.agentOptions.ComponentNamespace))

	// load bootstrap client config and create bootstrap clients
	bootstrapClientConfig, err := clientcmd.BuildConfigFromFlags("", o.registrationOption.BootstrapKubeconfig)
	if err != nil {
		return fmt.Errorf("unable to load bootstrap kubeconfig from file %q: %w", o.registrationOption.BootstrapKubeconfig, err)
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
		o.agentOptions.SpokeClusterName, o.registrationOption.SpokeExternalServerURLs, o.registrationOption.ClusterAnnotations,
		spokeClusterCABundle,
		bootstrapClusterClient,
		recorder,
	)
	go spokeClusterCreatingController.Run(ctx, 1)

	secretInformer := namespacedManagementKubeInformerFactory.Core().V1().Secrets()
	if o.registrationOption.bootstrapKubeconfigHealthChecker != nil {
		// registter bootstrapKubeconfigHealthChecker as an event handle of secret informer
		if _, err = secretInformer.Informer().AddEventHandler(o.registrationOption.bootstrapKubeconfigHealthChecker); err != nil {
			return err
		}
	}

	hubKubeconfigSecretController := registration.NewHubKubeconfigSecretController(
		o.agentOptions.HubKubeconfigDir, o.agentOptions.ComponentNamespace, o.registrationOption.HubKubeconfigSecret,
		// the hub kubeconfig secret stored in the cluster where the agent pod runs
		managementKubeClient.CoreV1(),
		secretInformer,
		recorder,
	)
	go hubKubeconfigSecretController.Run(ctx, 1)
	go namespacedManagementKubeInformerFactory.Start(ctx.Done())

	// check if there already exists a valid client config for hub
	ok, err := o.HasValidHubClientConfig(ctx)
	if err != nil {
		return err
	}

	// create and start a ClientCertForHubController for spoke agent bootstrap to deal with scenario #1 and #4.
	// Running the bootstrap ClientCertForHubController is optional. If always run it no matter if there already
	// exists a valid client config for hub or not, the controller will be started and then stopped immediately
	// in scenario #2 and #3, which results in an error message in log: 'Observed a panic: timeout waiting for
	// informer cache'
	if !ok {
		// delete the hub kubeconfig secret if exits
		if err := managementKubeClient.CoreV1().Secrets(o.agentOptions.ComponentNamespace).Delete(ctx,
			o.registrationOption.HubKubeconfigSecret, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}

		// create a ClientCertForHubController for spoke agent bootstrap
		// the bootstrap informers are supposed to be terminated after completing the bootstrap process.
		bootstrapInformerFactory := informers.NewSharedInformerFactory(bootstrapKubeClient, 10*time.Minute)
		bootstrapNamespacedManagementKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(
			managementKubeClient, 10*time.Minute, informers.WithNamespace(o.agentOptions.ComponentNamespace))

		// create a kubeconfig with references to the key/cert files in the same secret
		srever, proxyURL, caData, err := parseKubeconfig(o.registrationOption.BootstrapKubeconfig)
		if err != nil {
			return err
		}
		kubeconfig := clientcert.BuildKubeconfig(srever, caData, proxyURL, clientcert.TLSCertFile, clientcert.TLSKeyFile)
		kubeconfigData, err := clientcmd.Write(kubeconfig)
		if err != nil {
			return err
		}

		csrControl, err := clientcert.NewCSRControl(logger, bootstrapInformerFactory.Certificates(), bootstrapKubeClient)
		if err != nil {
			return err
		}

		controllerName := fmt.Sprintf("BootstrapClientCertController@cluster:%s", o.agentOptions.SpokeClusterName)
		clientCertForHubController := registration.NewClientCertForHubController(
			o.agentOptions.SpokeClusterName, o.agentOptions.AgentID, o.agentOptions.ComponentNamespace, o.registrationOption.HubKubeconfigSecret,
			kubeconfigData,
			// store the secret in the cluster where the agent pod runs
			bootstrapNamespacedManagementKubeInformerFactory.Core().V1().Secrets(),
			csrControl,
			o.registrationOption.ClientCertExpirationSeconds,
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

		logger.Info("Waiting for hub client config and managed cluster to be ready")
		if err := wait.PollUntilContextCancel(bootstrapCtx, 1*time.Second, true, o.HasValidHubClientConfig); err != nil {
			// TODO need run the bootstrap CSR forever to re-establish the client-cert if it is ever lost.
			stopBootstrap()
			return err
		}

		// stop the clientCertForHubController for bootstrap once the hub client config is ready
		stopBootstrap()
	}

	// create hub clients and shared informer factories from hub kube config
	hubClientConfig, err := clientcmd.BuildConfigFromFlags("", o.agentOptions.HubKubeconfigFile)
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
			listOptions.LabelSelector = fmt.Sprintf("%s=%s", clusterv1.ClusterNameLabelKey, o.agentOptions.SpokeClusterName)
		}),
	)
	addOnInformerFactory := addoninformers.NewSharedInformerFactoryWithOptions(
		addOnClient,
		10*time.Minute,
		addoninformers.WithNamespace(o.agentOptions.SpokeClusterName),
	)
	// create a cluster informer factory with name field selector because we just need to handle the current spoke cluster
	hubClusterInformerFactory := clusterv1informers.NewSharedInformerFactoryWithOptions(
		hubClusterClient,
		10*time.Minute,
		clusterv1informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			listOptions.FieldSelector = fields.OneTermEqualSelector("metadata.name", o.agentOptions.SpokeClusterName).String()
		}),
	)

	recorder.Event("HubClientConfigReady", "Client config for hub is ready.")

	// create a kubeconfig with references to the key/cert files in the same secret
	server, proxyURL, caData, err := parseKubeconfig(o.agentOptions.HubKubeconfigFile)
	if err != nil {
		return err
	}
	kubeconfig := clientcert.BuildKubeconfig(server, caData, proxyURL, clientcert.TLSCertFile, clientcert.TLSKeyFile)
	kubeconfigData, err := clientcmd.Write(kubeconfig)
	if err != nil {
		return err
	}

	csrControl, err := clientcert.NewCSRControl(logger, hubKubeInformerFactory.Certificates(), hubKubeClient)
	if err != nil {
		return err
	}

	// create another ClientCertForHubController for client certificate rotation
	controllerName := fmt.Sprintf("ClientCertController@cluster:%s", o.agentOptions.SpokeClusterName)
	clientCertForHubController := registration.NewClientCertForHubController(
		o.agentOptions.SpokeClusterName, o.agentOptions.AgentID, o.agentOptions.ComponentNamespace, o.registrationOption.HubKubeconfigSecret,
		kubeconfigData,
		namespacedManagementKubeInformerFactory.Core().V1().Secrets(),
		csrControl,
		o.registrationOption.ClientCertExpirationSeconds,
		managementKubeClient,
		registration.GenerateStatusUpdater(
			hubClusterClient,
			hubClusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
			o.agentOptions.SpokeClusterName),
		recorder,
		controllerName,
	)
	if err != nil {
		return err
	}

	// create ManagedClusterLeaseController to keep the spoke cluster heartbeat
	managedClusterLeaseController := lease.NewManagedClusterLeaseController(
		o.agentOptions.SpokeClusterName,
		hubKubeClient,
		hubClusterInformerFactory.Cluster().V1().ManagedClusters(),
		recorder,
	)

	hubEventRecorder, err := helpers.NewEventRecorder(ctx, clusterscheme.Scheme, hubKubeClient, "klusterlet-agent")
	if err != nil {
		return err
	}
	// create NewManagedClusterStatusController to update the spoke cluster status
	managedClusterHealthCheckController := managedcluster.NewManagedClusterStatusController(
		o.agentOptions.SpokeClusterName,
		hubClusterClient,
		hubClusterInformerFactory.Cluster().V1().ManagedClusters(),
		spokeKubeClient.Discovery(),
		spokeClusterInformerFactory.Cluster().V1alpha1().ClusterClaims(),
		spokeKubeInformerFactory.Core().V1().Nodes(),
		o.registrationOption.MaxCustomClusterClaims,
		o.registrationOption.ClusterHealthCheckPeriod,
		recorder,
		hubEventRecorder,
	)

	var addOnLeaseController factory.Controller
	var addOnRegistrationController factory.Controller
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.AddonManagement) {
		addOnLeaseController = addon.NewManagedClusterAddOnLeaseController(
			o.agentOptions.SpokeClusterName,
			addOnClient,
			addOnInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
			hubKubeClient.CoordinationV1(),
			managementKubeClient.CoordinationV1(),
			spokeKubeClient.CoordinationV1(),
			AddOnLeaseControllerSyncInterval, //TODO: this interval time should be allowed to change from outside
			recorder,
		)

		addOnRegistrationController = addon.NewAddOnRegistrationController(
			o.agentOptions.SpokeClusterName,
			o.agentOptions.AgentID,
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
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.ClusterClaim) {
		go spokeClusterInformerFactory.Start(ctx.Done())
	}

	go clientCertForHubController.Run(ctx, 1)
	go managedClusterLeaseController.Run(ctx, 1)
	go managedClusterHealthCheckController.Run(ctx, 1)
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.AddonManagement) {
		go addOnLeaseController.Run(ctx, 1)
		go addOnRegistrationController.Run(ctx, 1)
	}

	// start health checking of hub client certificate
	if o.registrationOption.clientCertHealthChecker != nil {
		tlsCertFile := path.Join(o.agentOptions.HubKubeconfigDir, clientcert.TLSCertFile)
		go o.registrationOption.clientCertHealthChecker.start(ctx, tlsCertFile)
	}

	<-ctx.Done()
	return nil
}

// HasValidHubClientConfig returns ture if all the conditions below are met:
//  1. KubeconfigFile exists;
//  2. TLSKeyFile exists;
//  3. TLSCertFile exists;
//  4. Certificate in TLSCertFile is issued for the current cluster/agent;
//  5. Certificate in TLSCertFile is not expired;
//  6. Hub kubeconfig and bootstrap hub kubeconfig include the same server, proxyURL
//     and CA bundle.
//
// Normally, KubeconfigFile/TLSKeyFile/TLSCertFile will be created once the bootstrap process
// completes. Changing the name of the cluster will make the existing hub kubeconfig invalid,
// because certificate in TLSCertFile is issued to a specific cluster/agent.
func (o *SpokeAgentConfig) HasValidHubClientConfig(ctx context.Context) (bool, error) {
	logger := klog.FromContext(ctx)
	if _, err := os.Stat(o.agentOptions.HubKubeconfigFile); os.IsNotExist(err) {
		logger.V(4).Info("Kubeconfig file not found", "kubeconfigPath", o.agentOptions.HubKubeconfigFile)
		return false, nil
	}

	keyPath := path.Join(o.agentOptions.HubKubeconfigDir, clientcert.TLSKeyFile)
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		logger.V(4).Info("TLS key file not found", "keyPath", keyPath)
		return false, nil
	}

	certPath := path.Join(o.agentOptions.HubKubeconfigDir, clientcert.TLSCertFile)
	certData, err := os.ReadFile(path.Clean(certPath))
	if err != nil {
		logger.V(4).Info("Unable to load TLS cert file", "certPath", certPath)
		return false, nil
	}

	// check if the tls certificate is issued for the current cluster/agent
	clusterName, agentName, err := registration.GetClusterAgentNamesFromCertificate(certData)
	if err != nil {
		return false, nil
	}
	if clusterName != o.agentOptions.SpokeClusterName || agentName != o.agentOptions.AgentID {
		logger.V(4).Info("Certificate in file is issued for different agent",
			"certPath", certPath,
			"issuedFor", fmt.Sprintf("%s:%s", clusterName, agentName),
			"expectedFor", fmt.Sprintf("%s:%s", o.agentOptions.SpokeClusterName, o.agentOptions.AgentID))

		return false, nil
	}

	if valid, err := clientcert.IsCertificateValid(logger, certData, nil); err != nil || !valid {
		return false, err
	}

	return o.isHubKubeconfigValid(ctx)
}

// The hub kubeconfig is valid when it shares the same value of the following with the
// bootstrap hub kubeconfig.
// 1. The hub server
// 2. The proxy url
// 3. The CA bundle
func (o *SpokeAgentConfig) isHubKubeconfigValid(ctx context.Context) (bool, error) {
	bootstrapServer, bootstrapProxyURL, bootstrapCABndle, err := parseKubeconfig(o.registrationOption.BootstrapKubeconfig)
	if err != nil {
		return false, err
	}

	server, proxyURL, caBundle, err := parseKubeconfig(o.agentOptions.HubKubeconfigFile)
	switch {
	case err != nil:
		return false, err
	case bootstrapServer != server, bootstrapProxyURL != proxyURL, !reflect.DeepEqual(bootstrapCABndle, caBundle):
		return false, nil
	default:
		return true, nil
	}
}

// getSpokeClusterCABundle returns the spoke cluster Kubernetes client CA data when SpokeExternalServerURLs is specified
func (o *SpokeAgentConfig) getSpokeClusterCABundle(kubeConfig *rest.Config) ([]byte, error) {
	if len(o.registrationOption.SpokeExternalServerURLs) == 0 {
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

func parseKubeconfig(filename string) (string, string, []byte, error) {
	config, err := clientcmd.LoadFromFile(filename)
	if err != nil {
		return "", "", nil, err
	}

	context, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return "", "", nil, fmt.Errorf("kubeconfig %q does not contains context: %s", filename, config.CurrentContext)
	}

	cluster, ok := config.Clusters[context.Cluster]
	if !ok {
		return "", "", nil, fmt.Errorf("kubeconfig %q does not contains cluster: %s", filename, context.Cluster)
	}

	return cluster.Server, cluster.ProxyURL, cluster.CertificateAuthorityData, nil
}
