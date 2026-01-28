package spoke

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	aboutclient "sigs.k8s.io/about-api/pkg/generated/clientset/versioned"
	aboutinformers "sigs.k8s.io/about-api/pkg/generated/informers/externalversions"

	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterscheme "open-cluster-management.io/api/client/cluster/clientset/versioned/scheme"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/spoke/addon"
	"open-cluster-management.io/ocm/pkg/registration/spoke/lease"
	"open-cluster-management.io/ocm/pkg/registration/spoke/managedcluster"
	"open-cluster-management.io/ocm/pkg/registration/spoke/registration"
)

// AddOnLeaseControllerSyncInterval is exposed so that integration tests can crank up the controller sync speed.
// TODO if we register the lease informer to the lease controller, we need to increase this time
var AddOnLeaseControllerSyncInterval = 30 * time.Second

type SpokeAgentConfig struct {
	agentOptions       *commonoptions.AgentOptions
	registrationOption *SpokeAgentOptions

	driver register.RegisterDriver

	// currentBootstrapKubeConfig is the selected bootstrap kubeconfig file path.
	// Only used in MultipleHubs feature.
	currentBootstrapKubeConfig string

	internalHubConfigValidFunc wait.ConditionWithContextFunc

	hubKubeConfigChecker *hubKubeConfigHealthChecker

	// agentStopFunc is the function to stop the agent, and the external system can restart the agent
	// with the refreshed configuration.
	agentStopFunc context.CancelFunc
}

// NewSpokeAgentConfig returns a SpokeAgentConfig
func NewSpokeAgentConfig(commonOpts *commonoptions.AgentOptions, opts *SpokeAgentOptions, cancel context.CancelFunc) *SpokeAgentConfig {
	cfg := &SpokeAgentConfig{
		agentOptions:       commonOpts,
		registrationOption: opts,
		agentStopFunc:      cancel,
	}
	cfg.hubKubeConfigChecker = &hubKubeConfigHealthChecker{
		checkFunc: cfg.IsHubKubeConfigValid,
	}

	return cfg
}

func (o *SpokeAgentConfig) HealthCheckers() []healthz.HealthChecker {
	return []healthz.HealthChecker{
		o.hubKubeConfigChecker,
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
// A temporary BootstrapController with bootstrap kubeconfig is created
// and started if the hub kubeconfig does not exist or is invalid and used to
// create a valid hub kubeconfig. Once the hub kubeconfig is valid, the
// temporary controller is stopped and the main controllers are started.
//
// The agent will be restarted once any of the following happens:
//   - the bootstrap hub kubeconfig changes (updated/deleted);
//   - the client certificate referenced by the hub kubeconfig become expired (Return failure when
//     checking the health of the agent);
func (o *SpokeAgentConfig) RunSpokeAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// setting up contextual logger
	logger := klog.NewKlogr()
	podName := os.Getenv("POD_NAME")
	if podName != "" {
		logger = logger.WithValues("podName", podName, "clusterName", o.agentOptions.SpokeClusterName)
	}
	ctx = klog.NewContext(ctx, logger)

	kubeConfig := controllerContext.KubeConfig

	// load spoke client config and create spoke clients,
	// the registration agent may not be running in the spoke/managed cluster.
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

	aboutClusterClient, err := aboutclient.NewForConfig(spokeClientConfig)
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
		aboutinformers.NewSharedInformerFactory(aboutClusterClient, 10*time.Minute),
		events.NewContextualLoggingEventRecorder("registration-agent"),
	)
}

func (o *SpokeAgentConfig) RunSpokeAgentWithSpokeInformers(ctx context.Context,
	kubeConfig, spokeClientConfig *rest.Config,
	spokeKubeClient kubernetes.Interface,
	spokeKubeInformerFactory informers.SharedInformerFactory,
	spokeClusterInformerFactory clusterv1informers.SharedInformerFactory,
	aboutInformers aboutinformers.SharedInformerFactory,
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

	// bootstrapKubeConfig is the file path of the bootstrap kubeconfig. If MultipleHubs feature is enabled, it is
	// the selected bootstrap kubeconfig file path.
	//
	// hubValiedConfig is a function to check if the hub client config is valid.
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.MultipleHubs) {
		index, err := selectBootstrapKubeConfigs(ctx, o.agentOptions.SpokeClusterName, o.agentOptions.
			HubKubeconfigFile, o.registrationOption.BootstrapKubeconfigs)
		if err != nil {
			return fmt.Errorf("failed to select a bootstrap kubeconfig: %w", err)
		}
		recorder.Eventf(ctx, "BootstrapSelected", "Select bootstrap kubeconfig with index %d and file %s",
			index, o.registrationOption.BootstrapKubeconfigs[index])
		o.currentBootstrapKubeConfig = o.registrationOption.BootstrapKubeconfigs[index]
	} else {
		o.currentBootstrapKubeConfig = o.registrationOption.BootstrapKubeconfig
	}

	// build up the secretOption
	secretOption := register.SecretOption{
		SecretNamespace:         o.agentOptions.ComponentNamespace,
		SecretName:              o.registrationOption.HubKubeconfigSecret,
		ClusterName:             o.agentOptions.SpokeClusterName,
		AgentName:               o.agentOptions.AgentID,
		HubKubeconfigFile:       o.agentOptions.HubKubeconfigFile,
		HubKubeconfigDir:        o.agentOptions.HubKubeconfigDir,
		BootStrapKubeConfigFile: o.currentBootstrapKubeConfig,
	}

	// initiate registration driver
	o.driver, err = o.registrationOption.RegisterDriverOption.Driver(secretOption)
	if err != nil {
		return err
	}

	secretInformer := namespacedManagementKubeInformerFactory.Core().V1().Secrets()
	// Register BootstrapKubeconfigEventHandler as an event handler of secret informer,
	// monitor the bootstrap kubeconfig and restart the pod immediately if it changes.
	//
	// The BootstrapKubeconfigEventHandler was originally part of the healthcheck and was
	// moved out to take some cases into account. For example, the work agent may resync a
	// wrong bootstrap kubeconfig from the cache before restarting since the healthcheck will
	// retry 3 times.
	if _, err = secretInformer.Informer().AddEventHandler(&bootstrapKubeconfigEventHandler{
		bootstrapKubeconfigSecretName: &o.registrationOption.BootstrapKubeconfigSecret,
		cancel:                        o.agentStopFunc,
	}); err != nil {
		return err
	}

	hubKubeconfigSecretController := registration.NewHubKubeconfigSecretController(
		o.agentOptions.HubKubeconfigDir, o.agentOptions.ComponentNamespace, o.registrationOption.HubKubeconfigSecret,
		// the hub kubeconfig secret stored in the cluster where the agent pod runs
		managementKubeClient.CoreV1(),
		secretInformer,
	)
	go hubKubeconfigSecretController.Run(ctx, 1)
	go namespacedManagementKubeInformerFactory.Start(ctx.Done())

	// check if there already exists a valid client config for hub
	o.internalHubConfigValidFunc = register.IsHubKubeConfigValidFunc(o.driver, secretOption)
	ok, err := o.internalHubConfigValidFunc(ctx)
	if err != nil {
		return err
	}

	// create and start a BootstrapController for spoke agent bootstrap to deal with scenario #1 and #4.
	// Running the BootstrapController is optional. If always run it no matter if there already
	// exists a valid client config for hub or not, the controller will be started and then stopped immediately
	// in scenario #2 and #3, which results in an error message in log: 'Observed a panic: timeout waiting for
	// informer cache'
	if !ok {
		// create a ClientCertForHubController for spoke agent bootstrap
		// the bootstrap informers are supposed to be terminated after completing the bootstrap process.
		bootstrapCtx, stopBootstrap := context.WithCancel(ctx)
		bootstrapClients, err := o.driver.BuildClients(bootstrapCtx, secretOption, true)
		if err != nil {
			stopBootstrap()
			return err
		}
		driverInformer, _ := o.driver.InformerHandler()
		// start a SpokeClusterCreatingController to make sure there is a spoke cluster on hub cluster
		spokeClusterCreatingController := registration.NewManagedClusterCreatingController(
			o.agentOptions.SpokeClusterName,
			[]registration.ManagedClusterDecorator{
				registration.AnnotationDecorator(o.registrationOption.ClusterAnnotations),
				registration.ClientConfigDecorator(o.registrationOption.SpokeExternalServerURLs, spokeClusterCABundle),
				o.driver.ManagedClusterDecorator,
			},
			bootstrapClients.ClusterClient,
		)

		controllerName := fmt.Sprintf("BootstrapController@cluster:%s", o.agentOptions.SpokeClusterName)
		secretController := register.NewSecretController(
			secretOption, o.driver, register.GenerateBootstrapStatusUpdater(),
			managementKubeClient.CoreV1(),
			namespacedManagementKubeInformerFactory.Core().V1().Secrets().Informer(),
			controllerName)

		go bootstrapClients.ClusterInformer.Informer().Run(bootstrapCtx.Done())
		if driverInformer != nil {
			go driverInformer.Run(bootstrapCtx.Done())
		}
		go spokeClusterCreatingController.Run(bootstrapCtx, 1)
		go secretController.Run(bootstrapCtx, 1)

		// Wait for the hub client config is ready.
		// PollUntilContextCancel periodically executes the condition func `o.internalHubConfigValidFunc`
		// until one of the following conditions is met:
		// - condition returns `true`: Indicates the hub client configuration
		//   is ready, and the polling stops successfully.
		// - condition returns an error: This happens when loading the kubeconfig
		//   file fails or the kubeconfig is invalid. In such cases, the error is returned, causing the
		//   agent to exit with an error and triggering a new leader election.
		// - The context is canceled: In this case, no error is returned. This ensures that
		//   the current leader can release leadership, allowing a new pod to get leadership quickly.
		logger.Info("Waiting for hub client config and managed cluster to be ready")
		if err := wait.PollUntilContextCancel(bootstrapCtx, 1*time.Second, true, o.internalHubConfigValidFunc); err != nil {
			// TODO need run the bootstrap CSR forever to re-establish the client-cert if it is ever lost.
			stopBootstrap()
			if err != context.Canceled {
				return fmt.Errorf("failed to wait for hub client config for managed cluster to be ready: %w", err)
			}
		}

		// stop the clientCertForHubController for bootstrap once the hub client config is ready
		stopBootstrap()
	}

	// reset clients from driver
	hubClient, err := o.driver.BuildClients(ctx, secretOption, false)
	if err != nil {
		return err
	}
	hubDriverInformer, _ := o.driver.InformerHandler()

	recorder.Event(ctx, "HubClientConfigReady", "Client config for hub is ready.")
	// create another RegisterController for registration credential rotation
	controllerName := fmt.Sprintf("RegisterController@cluster:%s", o.agentOptions.SpokeClusterName)
	secretController := register.NewSecretController(
		secretOption, o.driver, register.GenerateStatusUpdater(
			hubClient.ClusterClient,
			hubClient.ClusterInformer.Lister(),
			o.agentOptions.SpokeClusterName),
		managementKubeClient.CoreV1(),
		namespacedManagementKubeInformerFactory.Core().V1().Secrets().Informer(),
		controllerName)

	// create ManagedClusterLeaseController to keep the spoke cluster heartbeat
	managedClusterLeaseController := lease.NewManagedClusterLeaseController(
		o.agentOptions.SpokeClusterName,
		hubClient.LeaseClient,
		hubClient.ClusterInformer)

	hubEventRecorder, err := events.NewEventRecorder(ctx, clusterscheme.Scheme, hubClient.EventsClient, "klusterlet-agent")
	if err != nil {
		return fmt.Errorf("failed to create event recorder: %w", err)
	}

	// get hub hash for namespace management
	hubHash, err := o.getHubHash()
	if err != nil {
		return fmt.Errorf("failed to get hub hash: %w", err)
	}

	// create filtered namespace informer for hub-specific namespaces
	hubClusterSetLabel := managedcluster.GetHubClusterSetLabel(hubHash)
	labelSelector := labels.SelectorFromSet(labels.Set{hubClusterSetLabel: "true"})
	filteredNamespaceInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		spokeKubeClient,
		10*time.Minute,
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = labelSelector.String()
		}),
	)

	// create NewManagedClusterStatusController to update the spoke cluster status
	// now includes managed namespace reconciler
	managedClusterHealthCheckController := managedcluster.NewManagedClusterStatusController(
		o.agentOptions.SpokeClusterName,
		hubHash,
		hubClient.ClusterClient,
		spokeKubeClient,
		hubClient.ClusterInformer,
		filteredNamespaceInformerFactory.Core().V1().Namespaces(),
		spokeKubeClient.Discovery(),
		spokeClusterInformerFactory.Cluster().V1alpha1().ClusterClaims(),
		aboutInformers.About().V1alpha1().ClusterProperties(),
		spokeKubeInformerFactory.Core().V1().Nodes(),
		o.registrationOption.MaxCustomClusterClaims,
		o.registrationOption.ReservedClusterClaimSuffixes,
		o.registrationOption.ClusterHealthCheckPeriod,
		hubEventRecorder,
	)

	var addOnLeaseController factory.Controller
	var addOnRegistrationController factory.Controller
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.AddonManagement) {
		addOnLeaseController = addon.NewManagedClusterAddOnLeaseController(
			o.agentOptions.SpokeClusterName,
			hubClient.AddonClient,
			hubClient.AddonInformer,
			managementKubeClient.CoordinationV1(),
			spokeKubeClient.CoordinationV1(),
			AddOnLeaseControllerSyncInterval, //TODO: this interval time should be allowed to change from outside
		)

		// addon registration enabled when AddonDriverFactory is provided (supports CSR and token-based drivers)
		if addonDriverFactory, ok := o.driver.(register.AddonDriverFactory); ok {
			addOnRegistrationController = addon.NewAddOnRegistrationController(
				o.agentOptions.SpokeClusterName,
				o.agentOptions.AgentID,
				o.currentBootstrapKubeConfig,
				hubClient.AddonClient,
				managementKubeClient,
				spokeKubeClient,
				addonDriverFactory,
				o.registrationOption.RegisterDriverOption,
				hubClient.AddonInformer,
			)
		}
	}

	var hubAcceptController, hubTimeoutController factory.Controller
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.MultipleHubs) {
		hubAcceptController = registration.NewHubAcceptController(
			o.agentOptions.SpokeClusterName,
			hubClient.ClusterInformer,
			func(ctx context.Context) error {
				logger.Info("Failed to connect to hub because of hubAcceptClient set to false, restart agent to reselect a new bootstrap kubeconfig")
				o.agentStopFunc()
				return nil
			},
		)

		hubTimeoutController = registration.NewHubTimeoutController(
			o.agentOptions.SpokeClusterName,
			hubClient.LeaseClient,
			o.registrationOption.HubConnectionTimeoutSeconds,
			func(ctx context.Context) error {
				logger.Info("Failed to connect to hub because of lease out-of-date, restart agent to reselect a new bootstrap kubeconfig")
				o.agentStopFunc()
				return nil
			},
		)
	}

	if hubDriverInformer != nil {
		go hubDriverInformer.Run(ctx.Done())
	}
	go hubClient.ClusterInformer.Informer().Run(ctx.Done())
	go namespacedManagementKubeInformerFactory.Start(ctx.Done())
	go hubClient.AddonInformer.Informer().Run(ctx.Done())

	go spokeKubeInformerFactory.Start(ctx.Done())
	go filteredNamespaceInformerFactory.Start(ctx.Done())
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.ClusterClaim) {
		go spokeClusterInformerFactory.Start(ctx.Done())
	}

	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.ClusterProperty) {
		go aboutInformers.Start(ctx.Done())
	}

	go secretController.Run(ctx, 1)
	go managedClusterLeaseController.Run(ctx, 1)
	go managedClusterHealthCheckController.Run(ctx, 1)
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.AddonManagement) {
		go addOnLeaseController.Run(ctx, 1)
		// addon registration controller runs when the driver implements AddonDriverFactory
		// (supports CSR, GRPC, and AWS IRSA drivers with both CSR and token-based authentication)
		if _, ok := o.driver.(register.AddonDriverFactory); ok {
			go addOnRegistrationController.Run(ctx, 1)
		}
	}

	// start health checking of hub client certificate
	if o.hubKubeConfigChecker != nil {
		o.hubKubeConfigChecker.setBootstrapped()
	}

	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.MultipleHubs) {
		go hubAcceptController.Run(ctx, 1)
		go hubTimeoutController.Run(ctx, 1)
	}

	<-ctx.Done()
	return nil
}

func (o *SpokeAgentConfig) IsHubKubeConfigValid(ctx context.Context) (bool, error) {
	if o.internalHubConfigValidFunc == nil {
		return false, nil
	}
	return o.internalHubConfigValidFunc(ctx)
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

func (o *SpokeAgentConfig) getHubHash() (string, error) {
	kubeConfig, err := clientcmd.BuildConfigFromFlags("", o.currentBootstrapKubeConfig)
	if err != nil {
		return "", fmt.Errorf("unable to load bootstrap kubeconfig: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(kubeConfig.Host))), nil
}
