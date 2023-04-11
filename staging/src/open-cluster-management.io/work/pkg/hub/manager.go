package hub

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	"open-cluster-management.io/work/pkg/hub/controllers/manifestworkreplicasetcontroller"
)

// RunWorkHubManager starts the controllers on hub.
func RunWorkHubManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	hubWorkClient, err := workclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	hubClusterClient, err := clusterclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	clusterInformerFactory := clusterinformers.NewSharedInformerFactory(hubClusterClient, 30*time.Minute)
	workInformerFactory := workinformers.NewSharedInformerFactory(hubWorkClient, 30*time.Minute)

	// we need a separated filtered manifestwork informers so we only watch the manifestworks that manifestworkreplicaset cares.
	// This could reduce a lot of memory consumptions
	manifestWorkInformerFactory := workinformers.NewSharedInformerFactoryWithOptions(hubWorkClient, 30*time.Minute, workinformers.WithTweakListOptions(
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
	))

	manifestWorkReplicaSetController := manifestworkreplicasetcontroller.NewManifestWorkReplicaSetController(
		controllerContext.EventRecorder,
		hubWorkClient,
		workInformerFactory.Work().V1alpha1().ManifestWorkReplicaSets(),
		manifestWorkInformerFactory.Work().V1().ManifestWorks(),
		clusterInformerFactory.Cluster().V1beta1().Placements(),
		clusterInformerFactory.Cluster().V1beta1().PlacementDecisions(),
	)

	go clusterInformerFactory.Start(ctx.Done())
	go workInformerFactory.Start(ctx.Done())
	go manifestWorkInformerFactory.Start(ctx.Done())
	go manifestWorkReplicaSetController.Run(ctx, 5)

	<-ctx.Done()
	return nil
}
