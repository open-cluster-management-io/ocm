package klusterlet

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	versionutil "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"

	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/pkg/operator/operators/klusterlet/controllers/addonsecretcontroller"
	"open-cluster-management.io/ocm/pkg/operator/operators/klusterlet/controllers/klusterletcontroller"
	"open-cluster-management.io/ocm/pkg/operator/operators/klusterlet/controllers/ssarcontroller"
	"open-cluster-management.io/ocm/pkg/operator/operators/klusterlet/controllers/statuscontroller"
)

type Options struct {
	SkipPlaceholderHubSecret bool
	ControlPlaneNodeLabels   map[string]string
	DeploymentReplicas       int32
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

	// to reduce cache size if there are larges number of secrets
	newOneTermInformer := func(name string) informers.SharedInformerFactory {
		return informers.NewSharedInformerFactoryWithOptions(kubeClient, 5*time.Minute,
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.FieldSelector = fields.OneTermEqualSelector("metadata.name", name).String()
			}))
	}

	hubConfigSecretInformer := newOneTermInformer(helpers.HubKubeConfig)
	bootstrapConfigSecretInformer := newOneTermInformer(helpers.BootstrapHubKubeConfig)
	externalConfigSecretInformer := newOneTermInformer(helpers.ExternalManagedKubeConfig)

	secretInformers := map[string]corev1informers.SecretInformer{
		helpers.HubKubeConfig:             hubConfigSecretInformer.Core().V1().Secrets(),
		helpers.BootstrapHubKubeConfig:    bootstrapConfigSecretInformer.Core().V1().Secrets(),
		helpers.ExternalManagedKubeConfig: externalConfigSecretInformer.Core().V1().Secrets(),
	}

	deploymentInformer := informers.NewSharedInformerFactoryWithOptions(kubeClient, 5*time.Minute,
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = "createdBy=klusterlet"
		}))

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

	klusterletController := klusterletcontroller.NewKlusterletController(
		kubeClient,
		apiExtensionClient,
		operatorClient.OperatorV1().Klusterlets(),
		operatorInformer.Operator().V1().Klusterlets(),
		secretInformers,
		deploymentInformer.Apps().V1().Deployments(),
		workClient.WorkV1().AppliedManifestWorks(),
		kubeVersion,
		helpers.GetOperatorNamespace(),
		o.ControlPlaneNodeLabels,
		o.DeploymentReplicas,
		controllerContext.EventRecorder,
		o.SkipPlaceholderHubSecret)

	klusterletCleanupController := klusterletcontroller.NewKlusterletCleanupController(
		kubeClient,
		apiExtensionClient,
		operatorClient.OperatorV1().Klusterlets(),
		operatorInformer.Operator().V1().Klusterlets(),
		secretInformers,
		deploymentInformer.Apps().V1().Deployments(),
		workClient.WorkV1().AppliedManifestWorks(),
		kubeVersion,
		helpers.GetOperatorNamespace(),
		o.ControlPlaneNodeLabels,
		o.DeploymentReplicas,
		controllerContext.EventRecorder)

	ssarController := ssarcontroller.NewKlusterletSSARController(
		kubeClient,
		operatorClient.OperatorV1().Klusterlets(),
		operatorInformer.Operator().V1().Klusterlets(),
		secretInformers,
		controllerContext.EventRecorder,
	)

	statusController := statuscontroller.NewKlusterletStatusController(
		kubeClient,
		operatorClient.OperatorV1().Klusterlets(),
		operatorInformer.Operator().V1().Klusterlets(),
		deploymentInformer.Apps().V1().Deployments(),
		controllerContext.EventRecorder,
	)

	addonController := addonsecretcontroller.NewAddonPullImageSecretController(
		kubeClient,
		helpers.GetOperatorNamespace(),
		kubeInformer.Core().V1().Namespaces(),
		controllerContext.EventRecorder,
	)

	go operatorInformer.Start(ctx.Done())
	go kubeInformer.Start(ctx.Done())
	go hubConfigSecretInformer.Start(ctx.Done())
	go bootstrapConfigSecretInformer.Start(ctx.Done())
	go externalConfigSecretInformer.Start(ctx.Done())
	go deploymentInformer.Start(ctx.Done())
	go klusterletController.Run(ctx, 1)
	go klusterletCleanupController.Run(ctx, 1)
	go statusController.Run(ctx, 1)
	go ssarController.Run(ctx, 1)
	go addonController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
