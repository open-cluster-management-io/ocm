package spoke

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	cloudeventswork "open-cluster-management.io/sdk-go/pkg/cloudevents/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/agent/codec"

	"open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/spoke/auth"
	"open-cluster-management.io/ocm/pkg/work/spoke/controllers/appliedmanifestcontroller"
	"open-cluster-management.io/ocm/pkg/work/spoke/controllers/finalizercontroller"
	"open-cluster-management.io/ocm/pkg/work/spoke/controllers/manifestcontroller"
	"open-cluster-management.io/ocm/pkg/work/spoke/controllers/statuscontroller"
)

const (
	// If a controller queue size is too large (>500), the processing speed of the controller will drop significantly
	// with one worker, increasing the work numbers can improve the processing speed.
	// We compared the two situations where the worker is set to 1 and 10, when the worker is 10, the resource
	// utilization of the kubeapi-server and work agent do not increase significantly.
	//
	// TODO expose a flag to set the worker for each controller
	appliedManifestWorkFinalizeControllerWorkers = 10
	manifestWorkFinalizeControllerWorkers        = 10
	availableStatusControllerWorkers             = 10
)

type WorkAgentConfig struct {
	agentOptions *options.AgentOptions
	workOptions  *WorkloadAgentOptions
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
	// load spoke client config and create spoke clients,
	// the work agent may not running in the spoke/managed cluster.
	spokeRestConfig, err := o.agentOptions.SpokeKubeConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

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
	spokeWorkInformerFactory := workinformers.NewSharedInformerFactory(spokeWorkClient, 5*time.Minute)

	httpClient, err := rest.HTTPClientFor(spokeRestConfig)
	if err != nil {
		return err
	}
	restMapper, err := apiutil.NewDynamicRESTMapper(spokeRestConfig, httpClient)
	if err != nil {
		return err
	}

	hubHost, hubWorkClient, hubWorkInformer, err := o.newHubWorkClientAndInformer(ctx, restMapper)
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
		controllerContext.EventRecorder,
		restMapper,
	).NewExecutorValidator(ctx, features.SpokeMutableFeatureGate.Enabled(ocmfeature.ExecutorValidatingCaches))

	manifestWorkController := manifestcontroller.NewManifestWorkController(
		controllerContext.EventRecorder,
		spokeDynamicClient,
		spokeKubeClient,
		spokeAPIExtensionClient,
		hubWorkClient,
		hubWorkInformer,
		hubWorkInformer.Lister().ManifestWorks(o.agentOptions.SpokeClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubHash, agentID,
		restMapper,
		validator,
	)
	addFinalizerController := finalizercontroller.NewAddFinalizerController(
		controllerContext.EventRecorder,
		hubWorkClient,
		hubWorkInformer,
		hubWorkInformer.Lister().ManifestWorks(o.agentOptions.SpokeClusterName),
	)
	appliedManifestWorkFinalizeController := finalizercontroller.NewAppliedManifestWorkFinalizeController(
		controllerContext.EventRecorder,
		spokeDynamicClient,
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		agentID,
	)
	manifestWorkFinalizeController := finalizercontroller.NewManifestWorkFinalizeController(
		controllerContext.EventRecorder,
		hubWorkClient,
		hubWorkInformer,
		hubWorkInformer.Lister().ManifestWorks(o.agentOptions.SpokeClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubHash,
	)
	unmanagedAppliedManifestWorkController := finalizercontroller.NewUnManagedAppliedWorkController(
		controllerContext.EventRecorder,
		hubWorkInformer,
		hubWorkInformer.Lister().ManifestWorks(o.agentOptions.SpokeClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		o.workOptions.AppliedManifestWorkEvictionGracePeriod,
		hubHash, agentID,
	)
	appliedManifestWorkController := appliedmanifestcontroller.NewAppliedManifestWorkController(
		controllerContext.EventRecorder,
		spokeDynamicClient,
		hubWorkInformer,
		hubWorkInformer.Lister().ManifestWorks(o.agentOptions.SpokeClusterName),
		spokeWorkClient.WorkV1().AppliedManifestWorks(),
		spokeWorkInformerFactory.Work().V1().AppliedManifestWorks(),
		hubHash,
	)
	availableStatusController := statuscontroller.NewAvailableStatusController(
		controllerContext.EventRecorder,
		spokeDynamicClient,
		hubWorkClient,
		hubWorkInformer,
		hubWorkInformer.Lister().ManifestWorks(o.agentOptions.SpokeClusterName),
		o.workOptions.MaxJSONRawLength,
		o.workOptions.StatusSyncInterval,
	)

	go spokeWorkInformerFactory.Start(ctx.Done())
	go hubWorkInformer.Informer().Run(ctx.Done())

	go addFinalizerController.Run(ctx, 1)
	go appliedManifestWorkFinalizeController.Run(ctx, appliedManifestWorkFinalizeControllerWorkers)
	go unmanagedAppliedManifestWorkController.Run(ctx, 1)
	go appliedManifestWorkController.Run(ctx, 1)
	go manifestWorkController.Run(ctx, 1)
	go manifestWorkFinalizeController.Run(ctx, manifestWorkFinalizeControllerWorkers)
	go availableStatusController.Run(ctx, availableStatusControllerWorkers)

	<-ctx.Done()

	return nil
}

func buildCodecs(codecNames []string, restMapper meta.RESTMapper) []generic.Codec[*workv1.ManifestWork] {
	codecs := []generic.Codec[*workv1.ManifestWork]{}
	for _, name := range codecNames {
		if name == manifestBundleCodecName {
			codecs = append(codecs, codec.NewManifestBundleCodec())
		}

		if name == manifestCodecName {
			codecs = append(codecs, codec.NewManifestCodec(restMapper))
		}
	}
	return codecs
}

func (o *WorkAgentConfig) newHubWorkClientAndInformer(
	ctx context.Context,
	restMapper meta.RESTMapper,
) (string, workv1client.ManifestWorkInterface, workv1informers.ManifestWorkInformer, error) {
	if o.workOptions.WorkloadSourceDriver == "kube" {
		config, err := clientcmd.BuildConfigFromFlags("", o.workOptions.WorkloadSourceConfig)
		if err != nil {
			return "", nil, nil, err
		}

		kubeWorkClientSet, err := workclientset.NewForConfig(config)
		if err != nil {
			return "", nil, nil, err
		}

		factory := workinformers.NewSharedInformerFactoryWithOptions(
			kubeWorkClientSet,
			5*time.Minute,
			workinformers.WithNamespace(o.agentOptions.SpokeClusterName),
		)
		informer := factory.Work().V1().ManifestWorks()

		return config.Host, kubeWorkClientSet.WorkV1().ManifestWorks(o.agentOptions.SpokeClusterName), informer, nil
	}

	// For cloudevents drivers, we build ManifestWork client that implements the
	// ManifestWorkInterface and ManifestWork informer based on different driver configuration.
	// Refer to Event Based Manifestwork proposal in enhancements repo to get more details.
	hubHost, config, err := generic.NewConfigLoader(o.workOptions.WorkloadSourceDriver, o.workOptions.WorkloadSourceConfig).
		LoadConfig()
	if err != nil {
		return "", nil, nil, err
	}

	clientHolder, informer, err := cloudeventswork.NewClientHolderBuilder(config).
		WithClientID(o.workOptions.CloudEventsClientID).
		WithInformerConfig(5*time.Minute, workinformers.WithNamespace(o.agentOptions.SpokeClusterName)).
		WithClusterName(o.agentOptions.SpokeClusterName).
		WithCodecs(buildCodecs(o.workOptions.CloudEventsClientCodecs, restMapper)...).
		NewAgentClientHolderWithInformer(ctx)
	if err != nil {
		return "", nil, nil, err
	}

	return hubHost, clientHolder.ManifestWorks(o.agentOptions.SpokeClusterName), informer, err
}
