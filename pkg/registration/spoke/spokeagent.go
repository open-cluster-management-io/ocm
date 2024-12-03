package spoke

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/server/healthz"
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
	operatorv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/register"
	awsIrsa "open-cluster-management.io/ocm/pkg/registration/register/aws_irsa"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	"open-cluster-management.io/ocm/pkg/registration/spoke/addon"
	"open-cluster-management.io/ocm/pkg/registration/spoke/lease"
	"open-cluster-management.io/ocm/pkg/registration/spoke/managedcluster"
	"open-cluster-management.io/ocm/pkg/registration/spoke/registration"
)

// AddOnLeaseControllerSyncInterval is exposed so that integration tests can crank up the controller sync speed.
// TODO if we register the lease informer to the lease controller, we need to increase this time
var AddOnLeaseControllerSyncInterval = 30 * time.Second

const AwsIrsaAuthType = "awsirsa"
const CsrAuthType = "csr"

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

	// initiate registration driver
	var registerDriver register.RegisterDriver
	if o.registrationOption.RegistrationAuth == AwsIrsaAuthType {
		// TODO: may consider add additional validations
		if o.registrationOption.HubClusterArn != "" {
			registerDriver = awsIrsa.NewAWSIRSADriver()
			if o.registrationOption.ClusterAnnotations == nil {
				o.registrationOption.ClusterAnnotations = map[string]string{}
			}
			o.registrationOption.ClusterAnnotations[operatorv1.ClusterAnnotationsKeyPrefix+"/managed-cluster-arn"] = o.registrationOption.ManagedClusterArn
			o.registrationOption.ClusterAnnotations[operatorv1.ClusterAnnotationsKeyPrefix+"/managed-cluster-iam-role-suffix"] =
				o.registrationOption.ManagedClusterRoleSuffix

		} else {
			panic("A valid EKS Hub Cluster ARN is required with awsirsa based authentication")
		}
	} else {
		registerDriver = csr.NewCSRDriver()
	}

	o.driver = registerDriver

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
		recorder.Eventf("BootstrapSelected", "Select bootstrap kubeconfig with index %d and file %s",
			index, o.registrationOption.BootstrapKubeconfigs[index])
		o.currentBootstrapKubeConfig = o.registrationOption.BootstrapKubeconfigs[index]
	} else {
		o.currentBootstrapKubeConfig = o.registrationOption.BootstrapKubeconfig
	}

	// load bootstrap client config and create bootstrap clients
	bootstrapClientConfig, err := clientcmd.BuildConfigFromFlags("", o.currentBootstrapKubeConfig)
	if err != nil {
		return fmt.Errorf("unable to load bootstrap kubeconfig from file %q: %w", o.currentBootstrapKubeConfig, err)
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
		recorder,
	)
	go hubKubeconfigSecretController.Run(ctx, 1)
	go namespacedManagementKubeInformerFactory.Start(ctx.Done())

	// check if there already exists a valid client config for hub
	kubeconfig, err := clientcmd.LoadFromFile(o.currentBootstrapKubeConfig)
	if err != nil {
		return err
	}
	secretOption := register.SecretOption{
		SecretNamespace:          o.agentOptions.ComponentNamespace,
		SecretName:               o.registrationOption.HubKubeconfigSecret,
		ClusterName:              o.agentOptions.SpokeClusterName,
		AgentName:                o.agentOptions.AgentID,
		ManagementSecretInformer: namespacedManagementKubeInformerFactory.Core().V1().Secrets().Informer(),
		ManagementCoreClient:     managementKubeClient.CoreV1(),
		HubKubeconfigFile:        o.agentOptions.HubKubeconfigFile,
		HubKubeconfigDir:         o.agentOptions.HubKubeconfigDir,
		BootStrapKubeConfig:      kubeconfig,
	}
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
		bootstrapInformerFactory := informers.NewSharedInformerFactory(bootstrapKubeClient, 10*time.Minute)

		bootstrapClusterInformerFactory := clusterv1informers.NewSharedInformerFactory(bootstrapClusterClient, 10*time.Minute)

		// TODO: Generate csrOption or awsOption based on the value of --registration-auth may be move it under registerdriver as well

		var registrationAuthOption any
		if o.registrationOption.RegistrationAuth == AwsIrsaAuthType {
			if o.registrationOption.HubClusterArn != "" {
				registrationAuthOption, err = registration.NewAWSOption(
					secretOption,
					bootstrapClusterInformerFactory.Cluster(),
					bootstrapClusterClient)
				if err != nil {
					return err
				}
			} else {
				return fmt.Errorf("please provide EKS Hub Cluster ARN for the awsirsa based authentication")
			}
		} else {
			registrationAuthOption, err = registration.NewCSROption(logger,
				secretOption,
				o.registrationOption.ClientCertExpirationSeconds,
				bootstrapInformerFactory.Certificates(),
				bootstrapKubeClient)
			if err != nil {
				return err
			}
		}

		controllerName := fmt.Sprintf("BootstrapController@cluster:%s", o.agentOptions.SpokeClusterName)
		bootstrapCtx, stopBootstrap := context.WithCancel(ctx)
		secretController := register.NewSecretController(
			secretOption, registrationAuthOption, o.driver, registration.GenerateBootstrapStatusUpdater(), recorder, controllerName)

		go bootstrapInformerFactory.Start(bootstrapCtx.Done())
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

	// create hub clients and shared informer factories from hub kube config
	hubClientConfig, err := clientcmd.BuildConfigFromFlags("", o.agentOptions.HubKubeconfigFile)
	if err != nil {
		return fmt.Errorf("unable to load hub kubeconfig from file %q: %w", o.agentOptions.HubKubeconfigFile, err)
	}

	hubKubeClient, err := kubernetes.NewForConfig(hubClientConfig)
	if err != nil {
		return fmt.Errorf("failed to create hub kube client: %w", err)
	}

	hubClusterClient, err := clusterv1client.NewForConfig(hubClientConfig)
	if err != nil {
		return fmt.Errorf("failed to create hub cluster client: %w", err)
	}

	addOnClient, err := addonclient.NewForConfig(hubClientConfig)
	if err != nil {
		return fmt.Errorf("failed to create addon client: %w", err)
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

	csrOption, err := registration.NewCSROption(logger,
		secretOption,
		o.registrationOption.ClientCertExpirationSeconds,
		hubKubeInformerFactory.Certificates(),
		hubKubeClient)
	if err != nil {
		return fmt.Errorf("failed to create CSR option: %w", err)
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
		return fmt.Errorf("failed to create event recorder: %w", err)
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
			kubeconfig,
			addOnClient,
			managementKubeClient,
			spokeKubeClient,
			csrOption.CSRControl,
			addOnInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
			recorder,
		)
	}

	var hubAcceptController, hubTimeoutController factory.Controller
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.MultipleHubs) {
		hubAcceptController = registration.NewHubAcceptController(
			o.agentOptions.SpokeClusterName,
			hubClusterInformerFactory.Cluster().V1().ManagedClusters(),
			func(ctx context.Context) error {
				logger.Info("Failed to connect to hub because of hubAcceptClient set to false, restart agent to reselect a new bootstrap kubeconfig")
				o.agentStopFunc()
				return nil
			},
			recorder,
		)

		hubTimeoutController = registration.NewHubTimeoutController(
			o.agentOptions.SpokeClusterName,
			hubKubeClient,
			o.registrationOption.HubConnectionTimeoutSeconds,
			func(ctx context.Context) error {
				logger.Info("Failed to connect to hub because of lease out-of-date, restart agent to reselect a new bootstrap kubeconfig")
				o.agentStopFunc()
				return nil
			},
			recorder,
		)
	}

	if o.registrationOption.RegistrationAuth == CsrAuthType {
		// create another RegisterController for registration credential rotation
		controllerName := fmt.Sprintf("RegisterController@cluster:%s", o.agentOptions.SpokeClusterName)
		secretController := register.NewSecretController(
			secretOption, csrOption, o.driver, registration.GenerateStatusUpdater(
				hubClusterClient,
				hubClusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				o.agentOptions.SpokeClusterName), recorder, controllerName)

		go secretController.Run(ctx, 1)
	}

	go hubKubeInformerFactory.Start(ctx.Done())
	go hubClusterInformerFactory.Start(ctx.Done())
	go namespacedManagementKubeInformerFactory.Start(ctx.Done())
	go addOnInformerFactory.Start(ctx.Done())

	go spokeKubeInformerFactory.Start(ctx.Done())
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.ClusterClaim) {
		go spokeClusterInformerFactory.Start(ctx.Done())
	}

	go managedClusterLeaseController.Run(ctx, 1)
	go managedClusterHealthCheckController.Run(ctx, 1)
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.AddonManagement) {
		go addOnLeaseController.Run(ctx, 1)
		go addOnRegistrationController.Run(ctx, 1)
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
