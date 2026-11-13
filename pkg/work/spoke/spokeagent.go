package spoke

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	cloudeventsoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	cloudeventswork "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/agent/codec"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/builder"
	grpcoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
	ocmspiffe "open-cluster-management.io/sdk-go/pkg/spiffe"

	"open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/spoke/auth"
	"open-cluster-management.io/ocm/pkg/work/spoke/conditions"
	"open-cluster-management.io/ocm/pkg/work/spoke/controllers/finalizercontroller"
	"open-cluster-management.io/ocm/pkg/work/spoke/controllers/manifestcontroller"
	"open-cluster-management.io/ocm/pkg/work/spoke/controllers/statuscontroller"
)

type WorkAgentConfig struct {
	agentOptions *options.AgentOptions
	workOptions  *WorkloadAgentOptions

	// spiffeSource is the Workload API source of the agent's SVID, when it connects with SPIFFE.
	spiffeSource atomic.Pointer[ocmspiffe.X509Source]
}

// NewWorkAgentConfig returns a WorkAgentConfig
func NewWorkAgentConfig(commonOpts *options.AgentOptions, opts *WorkloadAgentOptions) *WorkAgentConfig {
	return &WorkAgentConfig{
		agentOptions: commonOpts,
		workOptions:  opts,
	}
}

// RunWorkloadAgent starts the controllers on agent to process work from hub.
func (o *WorkAgentConfig) RunWorkloadAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	if err := o.workOptions.Validate(); err != nil {
		return err
	}

	// setting up contextual logger
	logger := klog.NewKlogr()
	podName := os.Getenv("POD_NAME")
	if podName != "" {
		logger = logger.WithValues("podName", podName, "clusterName", o.agentOptions.SpokeClusterName)
	}
	ctx = klog.NewContext(ctx, logger)

	// load spoke client config and create spoke clients,
	// the work agent may not running in the spoke/managed cluster.
	spokeRestConfig, err := o.agentOptions.SpokeKubeConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// copy the config for work and set specific userAgent
	spokeRestConfig = rest.CopyConfig(spokeRestConfig)
	spokeRestConfig.UserAgent = o.workOptions.DefaultUserAgent

	spokeDynamicClient, err := dynamic.NewForConfig(spokeRestConfig)
	if err != nil {
		return err
	}
	spokeKubeClient, err := kubernetes.NewForConfig(spokeRestConfig)
	if err != nil {
		return err
	}
	spokeAPIExtensionClient, err := apiextensionsclient.NewForConfig(spokeRestConfig)
	if err != nil {
		return err
	}
	spokeWorkClient, err := workclientset.NewForConfig(spokeRestConfig)
	if err != nil {
		return err
	}

	// Resyncing at a small interval may cause performance issues when the number of AppliedManifestWorks is large.
	// Since the resync interval for the ManifestWork informer is set to 24 hours, use a different interval, such as
	// 21 hours, for the AppliedManifestWork informer to prevent concurrent resyncs between the two informers.
	spokeWorkInformerFactory := workinformers.NewSharedInformerFactory(spokeWorkClient, 21*time.Hour)

	httpClient, err := rest.HTTPClientFor(spokeRestConfig)
	if err != nil {
		return err
	}
	restMapper, err := apiutil.NewDynamicRESTMapper(spokeRestConfig, httpClient)
	if err != nil {
		return err
	}

	hubHost, hubWorkClient, hubWorkInformer, err := o.newWorkClientAndInformer(ctx)
	if err != nil {
		return err
	}

	agentID := o.agentOptions.AgentID
	hubHash := helper.HubHash(hubHost)
	if len(agentID) == 0 {
		agentID = hubHash
	}

	// create controllers
	validator := auth.NewFactory(
		spokeRestConfig,
		spokeKubeClient,
		hubWorkInformer,
		o.agentOptions.SpokeClusterName,
		restMapper,
	).NewExecutorValidator(ctx, features.SpokeMutableFeatureGate.Enabled(ocmfeature.ExecutorValidatingCaches))

	conditionReader, err := conditions.NewConditionReader()
	if err != nil {
		return err
	}

	objectReader, err := o.workOptions.ObjectReaderOption.NewObjectReader(spokeDynamicClient, hubWorkInformer)
	if err != nil {
		return err
	}

	manifestWorkController := manifestcontroller.NewManifestWorkController(
		spokeDynamicClient,
		spokeKubeClient,
		spokeAPIExtensionClient,
		hubWorkClient,
		hubWorkInformer,
		hubWorkInformer.Lister().ManifestWorks(o.agentOptions.SpokeClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		objectReader,
		hubHash, agentID,
		o.workOptions.HubClusterName,
		restMapper,
		validator,
	)
	addFinalizerController := finalizercontroller.NewAddFinalizerController(
		hubWorkClient,
		hubWorkInformer,
		hubWorkInformer.Lister().ManifestWorks(o.agentOptions.SpokeClusterName),
	)
	appliedManifestWorkFinalizeController := finalizercontroller.NewAppliedManifestWorkFinalizeController(
		spokeDynamicClient,
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		objectReader,
		agentID,
	)
	manifestWorkFinalizeController := finalizercontroller.NewManifestWorkFinalizeController(
		hubWorkClient,
		hubWorkInformer,
		hubWorkInformer.Lister().ManifestWorks(o.agentOptions.SpokeClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubHash,
	)
	unmanagedAppliedManifestWorkController := finalizercontroller.NewUnManagedAppliedWorkController(
		hubWorkInformer,
		hubWorkInformer.Lister().ManifestWorks(o.agentOptions.SpokeClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		o.workOptions.AppliedManifestWorkEvictionGracePeriod,
		hubHash, agentID,
	)
	availableStatusController := statuscontroller.NewAvailableStatusController(
		hubWorkClient,
		hubWorkInformer,
		hubWorkInformer.Lister().ManifestWorks(o.agentOptions.SpokeClusterName),
		conditionReader,
		objectReader,
		o.workOptions.MaxJSONRawLength,
		o.workOptions.StatusSyncInterval,
	)

	go spokeWorkInformerFactory.Start(ctx.Done())
	go hubWorkInformer.Informer().Run(ctx.Done())

	go addFinalizerController.Run(ctx, 1)
	go appliedManifestWorkFinalizeController.Run(ctx, o.workOptions.WorkloadAgentWorkers)
	go unmanagedAppliedManifestWorkController.Run(ctx, 1)
	go manifestWorkController.Run(ctx, o.workOptions.WorkloadAgentWorkers)
	go manifestWorkFinalizeController.Run(ctx, o.workOptions.WorkloadAgentWorkers)
	go availableStatusController.Run(ctx, o.workOptions.WorkloadAgentWorkers)

	<-ctx.Done()

	return nil
}

func (o *WorkAgentConfig) newWorkClientAndInformer(
	ctx context.Context,
) (string, workv1client.ManifestWorkInterface, workv1informers.ManifestWorkInformer, error) {
	var workClient workclientset.Interface
	var watcherStore *store.AgentInformerWatcherStore
	var hubHost string

	if o.workOptions.SPIFFE.Enabled() {
		// The agent's own SVID is its hub credential; nothing is read from a file or Secret.
		watcherStore = store.NewAgentInformerWatcherStore()
		grpcOptions, closer, err := grpcoptions.NewSPIFFEGRPCOptions(ctx, grpcoptions.SPIFFEConfig{
			URL:               o.workOptions.SPIFFE.HubEndpoint,
			HubSPIFFEID:       o.workOptions.SPIFFE.HubSPIFFEID,
			AgentSPIFFEID:     o.workOptions.SPIFFE.AgentSPIFFEID,
			WorkloadAPISocket: o.workOptions.SPIFFE.WorkloadAPISocket,
		})
		if err != nil {
			return "", nil, nil, err
		}
		if src, ok := closer.(*ocmspiffe.X509Source); ok {
			o.spiffeSource.Store(src)
		}
		go func() {
			<-ctx.Done()
			_ = closer.Close()
		}()
		clientID := o.workOptions.CloudEventsClientID
		if clientID == "" {
			clientID = o.agentOptions.SpokeClusterName + "-work-agent"
		}
		clientHolder, err := cloudeventswork.NewAgentClientHolder(ctx, cloudeventsoptions.NewGenericClientOptions(
			grpcOptions, codec.NewManifestBundleCodec(), clientID).
			WithClusterName(o.agentOptions.SpokeClusterName).
			WithClientWatcherStore(watcherStore))
		if err != nil {
			return "", nil, nil, err
		}
		hubHost = o.workOptions.SPIFFE.HubEndpoint
		workClient = clientHolder.WorkInterface()
	} else if o.workOptions.WorkloadSourceDriver == "kube" {
		config, err := clientcmd.BuildConfigFromFlags("", o.workOptions.WorkloadSourceConfig)
		if err != nil {
			return "", nil, nil, err
		}
		config.QPS = o.agentOptions.HubQPS
		config.Burst = o.agentOptions.HubBurst

		workClient, err = workclientset.NewForConfig(config)
		if err != nil {
			return "", nil, nil, err
		}

		hubHost = config.Host
	} else {
		// For cloudevents drivers, we build ManifestWork client that implements the
		// ManifestWorkInterface and ManifestWork informer based on different driver configuration.
		// Refer to Event Based Manifestwork proposal in enhancements repo to get more details.

		watcherStore = store.NewAgentInformerWatcherStore()

		serverHost, config, err := builder.NewConfigLoader(o.workOptions.WorkloadSourceDriver, o.workOptions.WorkloadSourceConfig).
			LoadConfig()
		if err != nil {
			return "", nil, nil, err
		}

		clientOptions := cloudeventsoptions.NewGenericClientOptions(
			config, codec.NewManifestBundleCodec(), o.workOptions.CloudEventsClientID).
			WithClusterName(o.agentOptions.SpokeClusterName).
			WithClientWatcherStore(watcherStore)
		clientHolder, err := cloudeventswork.NewAgentClientHolder(ctx, clientOptions)
		if err != nil {
			return "", nil, nil, err
		}

		hubHost = serverHost
		workClient = clientHolder.WorkInterface()
	}

	factory := workinformers.NewSharedInformerFactoryWithOptions(
		workClient,
		// resyncing at a small interval may cause performance issues when the number of ManifestWorks
		// is large.
		24*time.Hour,
		workinformers.WithNamespace(o.agentOptions.SpokeClusterName),
	)
	informer := factory.Work().V1().ManifestWorks()

	return hubHost, workClient.WorkV1().ManifestWorks(o.agentOptions.SpokeClusterName), informer, nil
}

// HealthCheckers reports the agent's SVID: not ready, with the expected and received IDs, while
// the Workload API has no SVID with the agent's ID.
func (o *WorkAgentConfig) HealthCheckers() []healthz.HealthChecker {
	return []healthz.HealthChecker{healthz.NamedCheck("spiffe-svid", func(_ *http.Request) error {
		if !o.workOptions.SPIFFE.Enabled() {
			return nil
		}
		src := o.spiffeSource.Load()
		if src == nil {
			return fmt.Errorf("the Workload API source is not open yet")
		}
		return src.Ready()
	})}
}
