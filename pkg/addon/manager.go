package addon

import (
	"context"
	"os"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"open-cluster-management.io/addon-framework/pkg/index"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions"

	"open-cluster-management.io/ocm/pkg/addon/controllers/addonconfiguration"
	"open-cluster-management.io/ocm/pkg/addon/controllers/addonmanagement"
	"open-cluster-management.io/ocm/pkg/addon/controllers/addonowner"
	"open-cluster-management.io/ocm/pkg/addon/controllers/addonprogressing"
	"open-cluster-management.io/ocm/pkg/addon/controllers/addontemplate"
	"open-cluster-management.io/ocm/pkg/addon/controllers/cmainstallprogression"
	addonindex "open-cluster-management.io/ocm/pkg/addon/index"
)

func RunManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// setting up contextual logger
	logger := klog.NewKlogr()
	podName := os.Getenv("POD_NAME")
	if podName != "" {
		logger = logger.WithValues("podName", podName)
	}
	ctx = klog.NewContext(ctx, logger)

	kubeConfig := controllerContext.KubeConfig
	hubKubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	hubClusterClient, err := clusterclientset.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	addonClient, err := addonv1alpha1client.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	workClient, err := workv1client.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	clusterInformerFactory := clusterinformers.NewSharedInformerFactory(hubClusterClient, 30*time.Minute)
	addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	workInformers := workv1informers.NewSharedInformerFactoryWithOptions(workClient, 10*time.Minute,
		workv1informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			selector := &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      addonv1alpha1.AddonLabelKey,
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			}
			listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
		}),
	)

	dynamicInformers := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 10*time.Minute)

	return RunControllerManagerWithInformers(
		ctx, controllerContext,
		hubKubeClient,
		addonClient,
		workClient,
		clusterInformerFactory,
		addonInformerFactory,
		workInformers,
		dynamicInformers,
	)
}

func RunControllerManagerWithInformers(
	ctx context.Context,
	controllerContext *controllercmd.ControllerContext,
	hubKubeClient kubernetes.Interface,
	hubAddOnClient addonv1alpha1client.Interface,
	hubWorkClient workv1client.Interface,
	clusterInformers clusterinformers.SharedInformerFactory,
	addonInformers addoninformers.SharedInformerFactory,
	workinformers workv1informers.SharedInformerFactory,
	dynamicInformers dynamicinformer.DynamicSharedInformerFactory,
) error {
	// addonDeployController
	err := workinformers.Work().V1().ManifestWorks().Informer().AddIndexers(
		cache.Indexers{
			index.ManifestWorkByAddon:           index.IndexManifestWorkByAddon,
			index.ManifestWorkByHostedAddon:     index.IndexManifestWorkByHostedAddon,
			index.ManifestWorkHookByHostedAddon: index.IndexManifestWorkHookByHostedAddon,
		},
	)
	if err != nil {
		return err
	}

	err = addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Informer().AddIndexers(
		cache.Indexers{
			addonindex.ManagedClusterAddonByName: addonindex.IndexManagedClusterAddonByName, // addonConfigurationController, addonManagementController
			index.ManagedClusterAddonByNamespace: index.IndexManagedClusterAddonByNamespace, // agentDeployController
			index.AddonByConfig:                  index.IndexAddonByConfig,                  // addonConfigController
		},
	)
	if err != nil {
		return err
	}

	// managementAddonConfigController
	err = addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Informer().AddIndexers(
		cache.Indexers{
			addonindex.ClusterManagementAddonByPlacement: addonindex.IndexClusterManagementAddonByPlacement, // addonConfigurationController, addonManagementController
			index.ClusterManagementAddonByConfig:         index.IndexClusterManagementAddonByConfig,         // cmaConfigController
		})
	if err != nil {
		return err
	}

	addonManagementController := addonmanagement.NewAddonManagementController(
		hubAddOnClient,
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
		clusterInformers.Cluster().V1beta1().Placements(),
		clusterInformers.Cluster().V1beta1().PlacementDecisions(),
		utils.ManagedByAddonManager,
	)

	addonConfigurationController := addonconfiguration.NewAddonConfigurationController(
		hubAddOnClient,
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
		clusterInformers.Cluster().V1beta1().Placements(),
		clusterInformers.Cluster().V1beta1().PlacementDecisions(),
		utils.ManagedByAddonManager,
	)

	addonOwnerController := addonowner.NewAddonOwnerController(
		hubAddOnClient,
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
		utils.ManagedByAddonManager,
	)

	addonProgressingController := addonprogressing.NewAddonProgressingController(
		hubAddOnClient,
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
		workinformers.Work().V1().ManifestWorks(),
		utils.ManagedByAddonManager,
	)

	mgmtAddonInstallProgressionController := cmainstallprogression.NewCMAInstallProgressionController(
		hubAddOnClient,
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
		utils.ManagedByAddonManager,
	)

	addonTemplateController := addontemplate.NewAddonTemplateController(
		controllerContext.KubeConfig,
		hubKubeClient,
		hubAddOnClient,
		hubWorkClient,
		addonInformers,
		clusterInformers,
		// can share the same dynamic informers for different template type addons since
		// these addons only support addontemplate and addondeploymentconfig
		dynamicInformers,
		workinformers,
	)

	go addonManagementController.Run(ctx, 2)
	go addonConfigurationController.Run(ctx, 2)
	go addonOwnerController.Run(ctx, 2)
	go addonProgressingController.Run(ctx, 2)
	go mgmtAddonInstallProgressionController.Run(ctx, 2)
	// There should be only one instance of addonTemplateController running, since the addonTemplateController will
	// start a goroutine for each template-type addon it watches.
	go addonTemplateController.Run(ctx, 1)

	clusterInformers.Start(ctx.Done())
	addonInformers.Start(ctx.Done())
	workinformers.Start(ctx.Done())
	dynamicInformers.Start(ctx.Done())

	<-ctx.Done()
	return nil
}
