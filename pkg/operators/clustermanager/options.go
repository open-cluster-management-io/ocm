package clustermanager

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	apiregistrationclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"

	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions"
	"open-cluster-management.io/registration-operator/pkg/operators/clustermanager/controllers/certrotationcontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/clustermanager/controllers/clustermanagercontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/clustermanager/controllers/migrationcontroller"
	clustermanagerstatuscontroller "open-cluster-management.io/registration-operator/pkg/operators/clustermanager/controllers/statuscontroller"
)

type Options struct {
}

// RunClusterManagerOperator starts a new cluster manager operator
func (o *Options) RunClusterManagerOperator(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
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

	// kubeInformer is for 3 usages: configmapInformer, secretInformer, deploynmentInformer
	// After we introduced detached mode, the hub components could be installed in a customized namespace.(Before that, it only inform from "open-cluster-management-hub" namespace)
	// It requires us to add filter for each Informer respectively.
	// TODO: Wathc all namespace may cause performance issue.
	kubeInformer := informers.NewSharedInformerFactoryWithOptions(kubeClient, 5*time.Minute)

	// Build operator client and informer
	operatorClient, err := operatorclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	operatorInformer := operatorinformer.NewSharedInformerFactory(operatorClient, 5*time.Minute)

	clusterManagerController := clustermanagercontroller.NewClusterManagerController(
		kubeClient,
		apiExtensionClient,
		apiRegistrationClient.ApiregistrationV1(),
		operatorClient.OperatorV1().ClusterManagers(),
		operatorInformer.Operator().V1().ClusterManagers(),
		kubeInformer.Apps().V1().Deployments(),
		kubeInformer.Core().V1().ConfigMaps(),
		controllerContext.EventRecorder)

	statusController := clustermanagerstatuscontroller.NewClusterManagerStatusController(
		operatorClient.OperatorV1().ClusterManagers(),
		operatorInformer.Operator().V1().ClusterManagers(),
		kubeInformer.Apps().V1().Deployments(),
		controllerContext.EventRecorder)

	certRotationController := certrotationcontroller.NewCertRotationController(
		kubeClient,
		kubeInformer.Core().V1().Secrets(),
		kubeInformer.Core().V1().ConfigMaps(),
		operatorInformer.Operator().V1().ClusterManagers(),
		controllerContext.EventRecorder)

	crdMigrationController := migrationcontroller.NewCRDMigrationController(
		controllerContext.KubeConfig,
		operatorInformer.Operator().V1().ClusterManagers(),
		controllerContext.EventRecorder)

	go operatorInformer.Start(ctx.Done())
	go kubeInformer.Start(ctx.Done())
	go clusterManagerController.Run(ctx, 1)
	go statusController.Run(ctx, 1)
	go certRotationController.Run(ctx, 1)
	go crdMigrationController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
