package operators

import (
	"context"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	apiregistrationclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	nucleusclient "github.com/open-cluster-management/api/client/nucleus/clientset/versioned"
	nucleusinformer "github.com/open-cluster-management/api/client/nucleus/informers/externalversions"
	"github.com/open-cluster-management/nucleus/pkg/operators/hub"
	"github.com/open-cluster-management/nucleus/pkg/operators/spoke"
)

// RunNucleusHubOperator starts a new nucleus hub operator
func RunNucleusHubOperator(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// Build kubclient client and informer for spoke cluster
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	apiExtensionClient, err := apiextensionsclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	apiRegistrationClient, err := apiregistrationclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// Build nucleus client and informer
	nucleusClient, err := nucleusclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	nucleusInformer := nucleusinformer.NewSharedInformerFactory(nucleusClient, 5*time.Minute)

	hubcontroller := hub.NewNucleusHubController(
		kubeClient,
		apiExtensionClient,
		apiRegistrationClient.ApiregistrationV1(),
		nucleusClient.NucleusV1().HubCores(),
		nucleusInformer.Nucleus().V1().HubCores(),
		controllerContext.EventRecorder)

	go nucleusInformer.Start(ctx.Done())
	go hubcontroller.Run(ctx, 1)
	<-ctx.Done()
	return nil
}

// RunNucleusSpokeOperator starts a new nucleus spoke operator
func RunNucleusSpokeOperator(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// Build kubclient client and informer for spoke cluster
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// Build nucleus client and informer
	nucleusClient, err := nucleusclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	nucleusInformer := nucleusinformer.NewSharedInformerFactory(nucleusClient, 5*time.Minute)

	spokeController := spoke.NewNucleusSpokeController(
		kubeClient,
		nucleusClient.NucleusV1().SpokeCores(),
		nucleusInformer.Nucleus().V1().SpokeCores(),
		controllerContext.EventRecorder)

	go nucleusInformer.Start(ctx.Done())
	go spokeController.Run(ctx, 1)
	<-ctx.Done()
	return nil
}
