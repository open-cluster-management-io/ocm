package operators

import (
	"context"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	apiregistrationclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	operatorclient "github.com/open-cluster-management/api/client/operator/clientset/versioned"
	operatorinformer "github.com/open-cluster-management/api/client/operator/informers/externalversions"
	"github.com/open-cluster-management/registration-operator/pkg/operators/clustermanager"
	"github.com/open-cluster-management/registration-operator/pkg/operators/klusterlet"
)

// RunClusterManagerOperator starts a new cluster manager operator
func RunClusterManagerOperator(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// Build kubclient client and informer for managed cluster
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

	// Build operator client and informer
	operatorClient, err := operatorclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	operatorInformer := operatorinformer.NewSharedInformerFactory(operatorClient, 5*time.Minute)

	clusterManagerController := clustermanager.NewClusterManagerController(
		kubeClient,
		apiExtensionClient,
		apiRegistrationClient.ApiregistrationV1(),
		operatorClient.OperatorV1().ClusterManagers(),
		operatorInformer.Operator().V1().ClusterManagers(),
		controllerContext.EventRecorder)

	go operatorInformer.Start(ctx.Done())
	go clusterManagerController.Run(ctx, 1)
	<-ctx.Done()
	return nil
}

// RunKlusterletOperator starts a new klusterlet operator
func RunKlusterletOperator(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// Build kubclient client and informer for managed cluster
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// Build operator client and informer
	operatorClient, err := operatorclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	operatorInformer := operatorinformer.NewSharedInformerFactory(operatorClient, 5*time.Minute)

	klusterletController := klusterlet.NewKlusterletController(
		kubeClient,
		operatorClient.OperatorV1().Klusterlets(),
		operatorInformer.Operator().V1().Klusterlets(),
		controllerContext.EventRecorder)

	go operatorInformer.Start(ctx.Done())
	go klusterletController.Run(ctx, 1)
	<-ctx.Done()
	return nil
}
