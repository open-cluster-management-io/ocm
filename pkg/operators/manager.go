package operators

import (
	"context"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	nucleusclient "github.com/open-cluster-management/api/client/nucleus/clientset/versioned"
	nucleusinformer "github.com/open-cluster-management/api/client/nucleus/informers/externalversions"
	"github.com/open-cluster-management/nucleus/pkg/operators/hub"
	"github.com/open-cluster-management/nucleus/pkg/operators/spoke"
)

// RunNucleusOperator starts a new nucleus operator
func RunNucleusOperator(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// Build kubclient client and informer for spoke cluster
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	apiExtensionClient, err := apiextensionsclient.NewForConfig(controllerContext.KubeConfig)
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
		nucleusClient.NucleusV1().HubCores(),
		nucleusInformer.Nucleus().V1().HubCores(),
		controllerContext.EventRecorder)
	agentController := spoke.NewNucleusAgentController(kubeClient, controllerContext.EventRecorder)

	go nucleusInformer.Start(ctx.Done())
	go hubcontroller.Run(ctx, 1)
	go agentController.Run(ctx, 1)
	<-ctx.Done()
	return nil
}
