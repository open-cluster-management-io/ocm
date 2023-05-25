package klusterlet

import (
	"context"
	"io/ioutil"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	versionutil "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	"open-cluster-management.io/registration-operator/pkg/operators/klusterlet/controllers/addonsecretcontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/klusterlet/controllers/bootstrapcontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/klusterlet/controllers/klusterletcontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/klusterlet/controllers/ssarcontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/klusterlet/controllers/statuscontroller"
)

// defaultSpokeComponentNamespace is the default namespace in which the operator is deployed
const defaultComponentNamespace = "open-cluster-management"

type Options struct {
	SkipPlaceholderHubSecret bool
}

// RunKlusterletOperator starts a new klusterlet operator
func (o *Options) RunKlusterletOperator(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
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
		controllerContext.EventRecorder,
		o.SkipPlaceholderHubSecret)

	klusterletCleanupController := klusterletcontroller.NewKlusterletCleanupController(
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

	ssarController := ssarcontroller.NewKlusterletSSARController(
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

	addonController := addonsecretcontroller.NewAddonPullImageSecretController(
		kubeClient,
		operatorNamespace,
		kubeInformer.Core().V1().Namespaces(),
		controllerContext.EventRecorder,
	)

	go operatorInformer.Start(ctx.Done())
	go kubeInformer.Start(ctx.Done())
	go klusterletController.Run(ctx, 1)
	go klusterletCleanupController.Run(ctx, 1)
	go statusController.Run(ctx, 1)
	go ssarController.Run(ctx, 1)
	go bootstrapController.Run(ctx, 1)
	go addonController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
