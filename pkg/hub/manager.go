package hub

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/client-go/kubernetes"

	clusterv1client "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterv1informers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	"github.com/open-cluster-management/registration/pkg/hub/csr"
	"github.com/open-cluster-management/registration/pkg/hub/managedcluster"
	kubeinformers "k8s.io/client-go/informers"
)

// RunControllerManager starts the controllers on hub to manage spoke cluster registration.
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
	kubeInfomers := kubeinformers.NewSharedInformerFactory(kubeClient, 10*time.Minute)

	managedClusterController := managedcluster.NewManagedClusterController(
		kubeClient,
		clusterClient,
		clusterInformers.Cluster().V1().ManagedClusters().Informer(),
		controllerContext.EventRecorder,
	)

	csrController := csr.NewCSRApprovingController(
		kubeClient,
		kubeInfomers.Certificates().V1beta1().CertificateSigningRequests().Informer(),
		controllerContext.EventRecorder,
	)

	go clusterInformers.Start(ctx.Done())
	go kubeInfomers.Start(ctx.Done())

	go managedClusterController.Run(ctx, 1)
	go csrController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
