package hub

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workapplier "open-cluster-management.io/sdk-go/pkg/apis/work/v1/applier"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/source/codec"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"

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
	workClient, err := workclientset.NewForConfig(controllerContext.KubeConfig)
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

	workInformers := workinformers.NewSharedInformerFactoryWithOptions(workClient, 30*time.Minute, workInformOption)
	replicaSetInformers := workinformers.NewSharedInformerFactory(workClient, 30*time.Minute)

	return c.RunControllerManagerWithInformers(
		ctx,
		controllerContext,
		workClient,
		replicaSetInformers,
		workInformers,
		clusterInformerFactory,
	)
}

func (c *WorkHubManagerConfig) RunControllerManagerWithInformers(
	ctx context.Context,
	controllerContext *controllercmd.ControllerContext,
	workClient workclientset.Interface,
	replicaSetInformers workinformers.SharedInformerFactory,
	workInformers workinformers.SharedInformerFactory,
	clusterInformers clusterinformers.SharedInformerFactory,
) error {
	var manifestWorkClient workclientset.Interface
	var manifestWorkInformer workv1informers.ManifestWorkInformer

	if c.workOptions.WorkDriver == "kube" {
		manifestWorkClient = workClient
		manifestWorkInformer = workInformers.Work().V1().ManifestWorks()
	} else {
		// For cloudevents drivers, we build ManifestWork client that implements the
		// ManifestWorkInterface and ManifestWork informer based on different driver configuration.
		// Refer to Event Based Manifestwork proposal in enhancements repo to get more details.
		watcherStore := store.NewSourceInformerWatcherStore(ctx)

		_, config, err := generic.NewConfigLoader(c.workOptions.WorkDriver, c.workOptions.WorkDriverConfig).
			LoadConfig()
		if err != nil {
			return err
		}

		clientOptions := options.NewGenericClientOptions(
			config, codec.NewManifestBundleCodec(), c.workOptions.CloudEventsClientID).
			WithSourceID(sourceID).
			WithClientWatcherStore(watcherStore)
		clientHolder, err := work.NewSourceClientHolder(ctx, clientOptions)
		if err != nil {
			return err
		}

		manifestWorkClient = clientHolder.WorkInterface()

		ceInformers := workinformers.NewSharedInformerFactoryWithOptions(workClient, 30*time.Minute)
		manifestWorkInformer = ceInformers.Work().V1().ManifestWorks()
		watcherStore.SetInformer(manifestWorkInformer.Informer())
	}

	manifestWorkReplicaSetController := manifestworkreplicasetcontroller.NewManifestWorkReplicaSetController(
		controllerContext.EventRecorder,
		workClient,
		workapplier.NewWorkApplierWithTypedClient(manifestWorkClient, manifestWorkInformer.Lister()),
		replicaSetInformers.Work().V1alpha1().ManifestWorkReplicaSets(),
		manifestWorkInformer,
		clusterInformers.Cluster().V1beta1().Placements(),
		clusterInformers.Cluster().V1beta1().PlacementDecisions(),
	)

	go clusterInformers.Start(ctx.Done())
	go replicaSetInformers.Start(ctx.Done())
	go manifestWorkReplicaSetController.Run(ctx, 5)

	go manifestWorkInformer.Informer().Run(ctx.Done())

	<-ctx.Done()
	return nil
}
