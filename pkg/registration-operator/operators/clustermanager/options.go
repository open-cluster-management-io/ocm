package clustermanager

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"open-cluster-management.io/ocm/pkg/registration-operator/helpers"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions"
	"open-cluster-management.io/ocm/pkg/registration-operator/operators/clustermanager/controllers/certrotationcontroller"
	"open-cluster-management.io/ocm/pkg/registration-operator/operators/clustermanager/controllers/clustermanagercontroller"
	"open-cluster-management.io/ocm/pkg/registration-operator/operators/clustermanager/controllers/crdstatuccontroller"
	"open-cluster-management.io/ocm/pkg/registration-operator/operators/clustermanager/controllers/migrationcontroller"
	clustermanagerstatuscontroller "open-cluster-management.io/ocm/pkg/registration-operator/operators/clustermanager/controllers/statuscontroller"
)

type Options struct {
	SkipRemoveCRDs bool
}

// RunClusterManagerOperator starts a new cluster manager operator
func (o *Options) RunClusterManagerOperator(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// Build kubclient client and informer for managed cluster
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// kubeInformer is for 3 usages: configmapInformer, secretInformer, deploynmentInformer
	// After we introduced hosted mode, the hub components could be installed in a customized namespace.(Before that, it only inform from "open-cluster-management-hub" namespace)
	// It requires us to add filter for each Informer respectively.
	// TODO: Watch all namespace may cause performance issue.
	kubeInformer := informers.NewSharedInformerFactoryWithOptions(kubeClient, 5*time.Minute)

	newOnTermInformer := func(name string) informers.SharedInformerFactory {
		return informers.NewSharedInformerFactoryWithOptions(kubeClient, 5*time.Minute,
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.FieldSelector = fields.OneTermEqualSelector("metadata.name", name).String()
			}))
	}

	signerSecretInformer := newOnTermInformer(helpers.SignerSecret)
	registrationSecretInformer := newOnTermInformer(helpers.RegistrationWebhookSecret)
	workSecretInformer := newOnTermInformer(helpers.WorkWebhookSecret)
	configmapInformer := newOnTermInformer(helpers.CaBundleConfigmap)

	secretInformers := map[string]corev1informers.SecretInformer{
		helpers.SignerSecret:              signerSecretInformer.Core().V1().Secrets(),
		helpers.RegistrationWebhookSecret: registrationSecretInformer.Core().V1().Secrets(),
		helpers.WorkWebhookSecret:         workSecretInformer.Core().V1().Secrets(),
	}

	// Build operator client and informer
	operatorClient, err := operatorclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	operatorInformer := operatorinformer.NewSharedInformerFactory(operatorClient, 5*time.Minute)

	clusterManagerController := clustermanagercontroller.NewClusterManagerController(
		kubeClient,
		controllerContext.KubeConfig,
		operatorClient.OperatorV1().ClusterManagers(),
		operatorInformer.Operator().V1().ClusterManagers(),
		kubeInformer.Apps().V1().Deployments(),
		kubeInformer.Core().V1().ConfigMaps(),
		controllerContext.EventRecorder,
		o.SkipRemoveCRDs)

	statusController := clustermanagerstatuscontroller.NewClusterManagerStatusController(
		operatorClient.OperatorV1().ClusterManagers(),
		operatorInformer.Operator().V1().ClusterManagers(),
		kubeInformer.Apps().V1().Deployments(),
		controllerContext.EventRecorder)

	certRotationController := certrotationcontroller.NewCertRotationController(
		kubeClient,
		secretInformers,
		configmapInformer.Core().V1().ConfigMaps(),
		operatorInformer.Operator().V1().ClusterManagers(),
		controllerContext.EventRecorder)

	crdMigrationController := migrationcontroller.NewCRDMigrationController(
		controllerContext.KubeConfig,
		kubeClient,
		operatorClient.OperatorV1().ClusterManagers(),
		operatorInformer.Operator().V1().ClusterManagers(),
		controllerContext.EventRecorder)

	crdStatusController := crdstatuccontroller.NewCRDStatusController(
		controllerContext.KubeConfig,
		kubeClient,
		operatorInformer.Operator().V1().ClusterManagers(),
		controllerContext.EventRecorder)

	go operatorInformer.Start(ctx.Done())
	go kubeInformer.Start(ctx.Done())
	go signerSecretInformer.Start(ctx.Done())
	go registrationSecretInformer.Start(ctx.Done())
	go workSecretInformer.Start(ctx.Done())
	go configmapInformer.Start(ctx.Done())
	go clusterManagerController.Run(ctx, 1)
	go statusController.Run(ctx, 1)
	go certRotationController.Run(ctx, 1)
	go crdMigrationController.Run(ctx, 1)
	go crdStatusController.Run(ctx, 1)
	<-ctx.Done()
	return nil
}
