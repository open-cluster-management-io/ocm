package hub

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	clusterclient "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	scheduling "github.com/open-cluster-management/placement/pkg/controllers/scheduling"
)

// RunControllerManager starts the controllers on hub to make placement decisions.
func RunControllerManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	clusterClient, err := clusterclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)

	schedulingController := scheduling.NewSchedulingController(
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters().Lister(),
		clusterInformers.Cluster().V1alpha1().ManagedClusterSets().Lister(),
		clusterInformers.Cluster().V1alpha1().ManagedClusterSetBindings().Lister(),
		clusterInformers.Cluster().V1alpha1().Placements(),
		clusterInformers.Cluster().V1alpha1().PlacementDecisions(),
		controllerContext.EventRecorder,
	)

	go clusterInformers.Start(ctx.Done())

	go schedulingController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
