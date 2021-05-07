package hub

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	clusterclient "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	placement "github.com/open-cluster-management/placement/pkg/controllers/placement"
	placementdecision "github.com/open-cluster-management/placement/pkg/controllers/placementdecision"
)

// RunControllerManager starts the controllers on hub to make placement decisions.
func RunControllerManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	clusterClient, err := clusterclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)

	placementController := placement.NewPlacementController(
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterInformers.Cluster().V1alpha1().ManagedClusterSets(),
		controllerContext.EventRecorder,
	)

	placementDecisionCreatingController := placementdecision.NewPlacementDecisionCreatingController(
		clusterClient,
		clusterInformers.Cluster().V1alpha1().Placements(),
		clusterInformers.Cluster().V1alpha1().PlacementDecisions(),
		controllerContext.EventRecorder,
	)

	go clusterInformers.Start(ctx.Done())

	go placementController.Run(ctx, 1)
	go placementDecisionCreatingController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
