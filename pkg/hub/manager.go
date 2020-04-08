package hub

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/client-go/kubernetes"

	clusterv1client "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterv1informers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	"github.com/open-cluster-management/registration/pkg/hub/spokecluster"
)

// RunControllerManager starts the controllers on hub to manage spoke cluster registraiton.
func RunControllerManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	clusterClient, err := clusterv1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	clusterInformers := clusterv1informers.NewSharedInformerFactory(clusterClient, 10*time.Minute)

	spokeClusterController := spokecluster.NewSpokeClusterController(
		kubeClient,
		clusterClient,
		clusterInformers.Cluster().V1().SpokeClusters().Informer(),
		controllerContext.EventRecorder,
	)

	go clusterInformers.Start(ctx.Done())

	go spokeClusterController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
