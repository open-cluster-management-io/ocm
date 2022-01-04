package operators

import (
	"context"
	"io/ioutil"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	versionutil "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	apiregistrationclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
	migrationclient "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	"open-cluster-management.io/registration-operator/pkg/helpers"
	certrotationcontroller "open-cluster-management.io/registration-operator/pkg/operators/clustermanager/controllers/certrotationcontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/clustermanager/controllers/clustermanagercontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/clustermanager/controllers/migrationcontroller"
	clustermanagerstatuscontroller "open-cluster-management.io/registration-operator/pkg/operators/clustermanager/controllers/statuscontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/klusterlet/controllers/bootstrapcontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/klusterlet/controllers/klusterletcontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/klusterlet/controllers/ssarcontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/klusterlet/controllers/statuscontroller"
)

// defaultSpokeComponentNamespace is the default namespace in which the operator is deployed
const defaultComponentNamespace = "open-cluster-management"

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
	migrationClient, err := migrationclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	kubeInformer := informers.NewSharedInformerFactoryWithOptions(kubeClient, 5*time.Minute, informers.WithNamespace(helpers.ClusterManagerNamespace))

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
		apiExtensionClient,
		migrationClient.MigrationV1alpha1(),
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

// RunKlusterletOperator starts a new klusterlet operator
func RunKlusterletOperator(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// Build kube client and informer for managed cluster
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	apiExtensionClient, err := apiextensionsclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	version, err := kubeClient.ServerVersion()
	if err != nil {
		return err
	}
	kubeVersion, err := versionutil.ParseGeneric(version.String())
	if err != nil {
		return err
	}

	kubeInformer := informers.NewSharedInformerFactory(kubeClient, 5*time.Minute)

	// Build operator client and informer
	operatorClient, err := operatorclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	operatorInformer := operatorinformer.NewSharedInformerFactory(operatorClient, 5*time.Minute)

	workClient, err := workclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// Read component namespace
	operatorNamespace := defaultComponentNamespace
	nsBytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil {
		operatorNamespace = string(nsBytes)
	}

	klusterletController := klusterletcontroller.NewKlusterletController(
		kubeClient,
		apiExtensionClient,
		operatorClient.OperatorV1().Klusterlets(),
		operatorInformer.Operator().V1().Klusterlets(),
		kubeInformer.Core().V1().Secrets(),
		kubeInformer.Apps().V1().Deployments(),
		workClient.WorkV1().AppliedManifestWorks(),
		kubeVersion,
		operatorNamespace,
		controllerContext.EventRecorder)

	ssarController := ssarcontroller.NewKlustrletSSARController(
		kubeClient,
		operatorClient.OperatorV1().Klusterlets(),
		operatorInformer.Operator().V1().Klusterlets(),
		kubeInformer.Core().V1().Secrets(),
		controllerContext.EventRecorder,
	)

	statusController := statuscontroller.NewKlusterletStatusController(
		kubeClient,
		operatorClient.OperatorV1().Klusterlets(),
		operatorInformer.Operator().V1().Klusterlets(),
		kubeInformer.Apps().V1().Deployments(),
		controllerContext.EventRecorder,
	)

	bootstrapController := bootstrapcontroller.NewBootstrapController(
		kubeClient,
		operatorInformer.Operator().V1().Klusterlets(),
		kubeInformer.Core().V1().Secrets(),
		controllerContext.EventRecorder,
	)

	go operatorInformer.Start(ctx.Done())
	go kubeInformer.Start(ctx.Done())
	go klusterletController.Run(ctx, 1)
	go statusController.Run(ctx, 1)
	go ssarController.Run(ctx, 1)
	go bootstrapController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
