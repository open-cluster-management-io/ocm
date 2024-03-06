package spoke

import (
	"context"
	"fmt"
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
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/mqtt"
	cloudeventswork "open-cluster-management.io/sdk-go/pkg/cloudevents/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/agent/codec"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
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
	agentOptions *commonoptions.AgentOptions
	workOptions  *WorkloadAgentOptions
}

// NewWorkAgentConfig returns a WorkAgentConfig
func NewWorkAgentConfig(commonOpts *commonoptions.AgentOptions, opts *WorkloadAgentOptions) *WorkAgentConfig {
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

	// build hub client and informer
	clientHolder, hubHash, agentID, err := o.buildHubClientHolder(ctx, o.agentOptions.SpokeClusterName, restMapper)
	if err != nil {
		return err
	}

	hubWorkClient := clientHolder.ManifestWorks(o.agentOptions.SpokeClusterName)
	hubWorkInformer := clientHolder.ManifestWorkInformer()

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

// To support consuming ManifestWorks from different drivers (like the Kubernetes apiserver or MQTT broker), we build
// ManifestWork client that implements the ManifestWorkInterface and ManifestWork informer based on different
// driver configuration.
// Refer to Event Based Manifestwork proposal in enhancements repo to get more details.
func (o *WorkAgentConfig) buildHubClientHolder(ctx context.Context,
	clusterName string, restMapper meta.RESTMapper) (*cloudeventswork.ClientHolder, string, string, error) {
	agentID := o.agentOptions.AgentID
	switch o.workOptions.WorkloadSourceDriver.Type {
	case KubeDriver:
		hubRestConfig, err := clientcmd.BuildConfigFromFlags("", o.workOptions.WorkloadSourceDriver.Config)
		if err != nil {
			return nil, "", "", err
		}

		hubHash := helper.HubHash(hubRestConfig.Host)
		if len(agentID) == 0 {
			agentID = hubHash
		}

		// Only watch the cluster namespace on hub
		clientHolder, err := cloudeventswork.NewClientHolderBuilder(agentID, hubRestConfig).
			WithInformerConfig(5*time.Minute, workinformers.WithNamespace(o.agentOptions.SpokeClusterName)).
			NewClientHolder(ctx)
		if err != nil {
			return nil, "", "", err
		}

		return clientHolder, hubHash, agentID, nil
	case MQTTDriver:
		mqttOptions, err := mqtt.BuildMQTTOptionsFromFlags(o.workOptions.WorkloadSourceDriver.Config)
		if err != nil {
			return nil, "", "", err
		}

		hubHash := helper.HubHash(mqttOptions.BrokerHost)
		if len(agentID) == 0 {
			agentID = fmt.Sprintf("%s-work-agent", o.agentOptions.SpokeClusterName)
		}

		clientHolder, err := cloudeventswork.NewClientHolderBuilder(agentID, mqttOptions).
			WithClusterName(o.agentOptions.SpokeClusterName).
			WithCodecs(codec.NewManifestCodec(restMapper)). // TODO support manifestbundles
			NewClientHolder(ctx)
		if err != nil {
			return nil, "", "", err
		}

		return clientHolder, hubHash, agentID, nil
	}

	return nil, "", "", fmt.Errorf("unsupported driver %s", o.workOptions.WorkloadSourceDriver.Type)
}
