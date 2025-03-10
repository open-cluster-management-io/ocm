package hub

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1informer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workapplier "open-cluster-management.io/sdk-go/pkg/apis/work/v1/applier"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/source/codec"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/store"

	"open-cluster-management.io/ocm/pkg/work/hub/controllers/manifestworkreplicasetcontroller"
)

const sourceID = "mwrsctrl"

// WorkHubManagerConfig holds configuration for work hub manager controller
type WorkHubManagerConfig struct {
	workOptions *WorkHubManagerOptions
}

// NewWorkHubManagerConfig returns a WorkHubManagerConfig
func NewWorkHubManagerConfig(opts *WorkHubManagerOptions) *WorkHubManagerConfig {
	return &WorkHubManagerConfig{
		workOptions: opts,
	}
}

// RunWorkHubManager starts the controllers on hub.
func (c *WorkHubManagerConfig) RunWorkHubManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	hubClusterClient, err := clusterclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	clusterInformerFactory := clusterinformers.NewSharedInformerFactory(hubClusterClient, 30*time.Minute)

	// build a hub work client for ManifestWorkReplicaSets
	replicaSetsClient, err := workclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// we need a separated filtered manifestwork informers so we only watch the manifestworks that manifestworkreplicaset cares.
	// This could reduce a lot of memory consumptions
	workInformOption := workinformers.WithTweakListOptions(
		func(listOptions *metav1.ListOptions) {
			selector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      manifestworkreplicasetcontroller.ManifestWorkReplicaSetControllerNameLabelKey,
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			}
			listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
		},
	)

	var workClient workclientset.Interface
	var watcherStore *store.SourceInformerWatcherStore

	if c.workOptions.WorkDriver == "kube" {
		config := controllerContext.KubeConfig
		if c.workOptions.WorkDriverConfig != "" {
			config, err = clientcmd.BuildConfigFromFlags("", c.workOptions.WorkDriverConfig)
			if err != nil {
				return err
			}
		}

		workClient, err = workclientset.NewForConfig(config)
		if err != nil {
			return err
		}
	} else {
		// For cloudevents drivers, we build ManifestWork client that implements the
		// ManifestWorkInterface and ManifestWork informer based on different driver configuration.
		// Refer to Event Based Manifestwork proposal in enhancements repo to get more details.

		watcherStore = store.NewSourceInformerWatcherStore(ctx)

		_, config, err := generic.NewConfigLoader(c.workOptions.WorkDriver, c.workOptions.WorkDriverConfig).
			LoadConfig()
		if err != nil {
			return err
		}

		clientHolder, err := work.NewClientHolderBuilder(config).
			WithClientID(c.workOptions.CloudEventsClientID).
			WithSourceID(sourceID).
			WithCodec(codec.NewManifestBundleCodec()).
			WithWorkClientWatcherStore(watcherStore).
			NewSourceClientHolder(ctx)
		if err != nil {
			return err
		}

		workClient = clientHolder.WorkInterface()
	}

	factory := workinformers.NewSharedInformerFactoryWithOptions(workClient, 30*time.Minute, workInformOption)
	informer := factory.Work().V1().ManifestWorks()

	// For cloudevents work client, we use the informer store as the client store
	if watcherStore != nil {
		watcherStore.SetInformer(informer.Informer())
	}

	return RunControllerManagerWithInformers(
		ctx,
		controllerContext,
		replicaSetsClient,
		workClient,
		informer,
		clusterInformerFactory,
	)
}

func RunControllerManagerWithInformers(
	ctx context.Context,
	controllerContext *controllercmd.ControllerContext,
	replicaSetClient workclientset.Interface,
	workClient workclientset.Interface,
	workInformer workv1informer.ManifestWorkInformer,
	clusterInformers clusterinformers.SharedInformerFactory,
) error {
	replicaSetInformerFactory := workinformers.NewSharedInformerFactory(replicaSetClient, 30*time.Minute)

	manifestWorkReplicaSetController := manifestworkreplicasetcontroller.NewManifestWorkReplicaSetController(
		controllerContext.EventRecorder,
		replicaSetClient,
		workapplier.NewWorkApplierWithTypedClient(workClient, workInformer.Lister()),
		replicaSetInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets(),
		workInformer,
		clusterInformers.Cluster().V1beta1().Placements(),
		clusterInformers.Cluster().V1beta1().PlacementDecisions(),
	)

	go clusterInformers.Start(ctx.Done())
	go replicaSetInformerFactory.Start(ctx.Done())
	go manifestWorkReplicaSetController.Run(ctx, 5)

	go workInformer.Informer().Run(ctx.Done())

	<-ctx.Done()
	return nil
}
