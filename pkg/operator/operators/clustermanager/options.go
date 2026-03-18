package clustermanager

import (
	"context"
	"os"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions"

	tlslib "open-cluster-management.io/ocm/pkg/common/tls"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/pkg/operator/operators/clustermanager/controllers/certrotationcontroller"
	"open-cluster-management.io/ocm/pkg/operator/operators/clustermanager/controllers/clustermanagercontroller"
	"open-cluster-management.io/ocm/pkg/operator/operators/clustermanager/controllers/crdstatuccontroller"
	"open-cluster-management.io/ocm/pkg/operator/operators/clustermanager/controllers/migrationcontroller"
	clustermanagerstatuscontroller "open-cluster-management.io/ocm/pkg/operator/operators/clustermanager/controllers/statuscontroller"
)

type Options struct {
	SkipRemoveCRDs                bool
	ControlPlaneNodeLabelSelector string
	DeploymentReplicas            int32
	EnableSyncLabels              bool
}

// RunClusterManagerOperator starts a new cluster manager operator
func (o *Options) RunClusterManagerOperator(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// setting up contextual logger
	logger := klog.NewKlogr()
	podName := os.Getenv("POD_NAME")
	if podName != "" {
		logger = logger.WithValues("podName", podName)
	}
	ctx = klog.NewContext(ctx, logger)

	// Build kubclient client and informer for managed cluster
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	newOneTermInformer := func(name string) informers.SharedInformerFactory {
		return informers.NewSharedInformerFactoryWithOptions(kubeClient, 5*time.Minute,
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.FieldSelector = fields.OneTermEqualSelector("metadata.name", name).String()
			}))
	}

	signerSecretInformer := newOneTermInformer(helpers.SignerSecret)
	registrationSecretInformer := newOneTermInformer(helpers.RegistrationWebhookSecret)
	workSecretInformer := newOneTermInformer(helpers.WorkWebhookSecret)
	addonSecretInformer := newOneTermInformer(helpers.AddonWebhookSecret)
	grpcServerSecretInformer := newOneTermInformer(helpers.GRPCServerSecret)
	configmapInformer := newOneTermInformer(helpers.CaBundleConfigmap)
	tlsConfigMapInformer := newOneTermInformer(tlslib.ConfigMapName)

	deploymentInformer := informers.NewSharedInformerFactoryWithOptions(kubeClient, 5*time.Minute,
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			selector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      helpers.HubLabelKey,
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			}
			options.LabelSelector = metav1.FormatLabelSelector(selector)
		}))

	secretInformers := map[string]corev1informers.SecretInformer{
		helpers.SignerSecret:              signerSecretInformer.Core().V1().Secrets(),
		helpers.RegistrationWebhookSecret: registrationSecretInformer.Core().V1().Secrets(),
		helpers.WorkWebhookSecret:         workSecretInformer.Core().V1().Secrets(),
		helpers.AddonWebhookSecret:        addonSecretInformer.Core().V1().Secrets(),
		helpers.GRPCServerSecret:          grpcServerSecretInformer.Core().V1().Secrets(),
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
		deploymentInformer.Apps().V1().Deployments(),
		configmapInformer.Core().V1().ConfigMaps(),
		o.SkipRemoveCRDs,
		o.ControlPlaneNodeLabelSelector,
		o.DeploymentReplicas,
		controllerContext.OperatorNamespace,
		o.EnableSyncLabels,
	)

	statusController := clustermanagerstatuscontroller.NewClusterManagerStatusController(
		operatorClient.OperatorV1().ClusterManagers(),
		operatorInformer.Operator().V1().ClusterManagers(),
		deploymentInformer.Apps().V1().Deployments())

	certRotationController := certrotationcontroller.NewCertRotationController(
		kubeClient,
		secretInformers,
		configmapInformer.Core().V1().ConfigMaps(),
		operatorInformer.Operator().V1().ClusterManagers())

	crdMigrationController := migrationcontroller.NewCRDMigrationController(
		controllerContext.KubeConfig,
		kubeClient,
		operatorClient.OperatorV1().ClusterManagers(),
		operatorInformer.Operator().V1().ClusterManagers())

	crdStatusController := crdstatuccontroller.NewCRDStatusController(
		controllerContext.KubeConfig,
		kubeClient,
		operatorInformer.Operator().V1().ClusterManagers())

	// Setup TLS ConfigMap watcher to restart operator when TLS config changes.
	// Note: This code only runs on the leader pod (due to leader election in controllercmd).
	// Non-leader pods wait idle and run this function when they become leader, loading
	// the current ConfigMap at that time. This ensures each leader uses the latest config.
	currentTLSConfig, err := tlslib.LoadTLSConfigFromConfigMap(ctx, kubeClient, controllerContext.OperatorNamespace)
	if err != nil {
		logger.Error(err, "Failed to load TLS ConfigMap, using default TLS config")
	}
	if currentTLSConfig == nil {
		// No ConfigMap, use default TLS config
		currentTLSConfig = tlslib.GetDefaultTLSConfig()
		logger.Info("No TLS ConfigMap found, using default TLS config",
			"minVersion", tlslib.TLSVersionToString(currentTLSConfig.MinVersion))
	} else {
		logger.Info("Loaded TLS config from ConfigMap",
			"minVersion", tlslib.TLSVersionToString(currentTLSConfig.MinVersion),
			"cipherSuites", len(currentTLSConfig.CipherSuites))
	}

	// Compute hash of current TLS config
	currentTLSHash := tlslib.HashTLSConfig(currentTLSConfig)

	tlsInformer := tlsConfigMapInformer.Core().V1().ConfigMaps().Informer()
	tlsInformer.AddEventHandler(
		tlslib.NewRestartEventHandler(ctx, logger, "cluster-manager operator", currentTLSHash))

	go operatorInformer.Start(ctx.Done())
	go deploymentInformer.Start(ctx.Done())
	go signerSecretInformer.Start(ctx.Done())
	go registrationSecretInformer.Start(ctx.Done())
	go workSecretInformer.Start(ctx.Done())
	go addonSecretInformer.Start(ctx.Done())
	go grpcServerSecretInformer.Start(ctx.Done())
	go configmapInformer.Start(ctx.Done())
	go tlsConfigMapInformer.Start(ctx.Done())
	go clusterManagerController.Run(ctx, 1)
	go statusController.Run(ctx, 1)
	go certRotationController.Run(ctx, 1)
	go crdMigrationController.Run(ctx, 1)
	go crdStatusController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
