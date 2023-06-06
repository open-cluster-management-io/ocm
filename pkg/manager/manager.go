package manager

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/addonconfig"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/controllers/managementaddonconfig"
	"open-cluster-management.io/addon-framework/pkg/index"
	"open-cluster-management.io/addon-framework/pkg/manager/controllers/addonconfiguration"
	"open-cluster-management.io/addon-framework/pkg/manager/controllers/addonmanagement"
	"open-cluster-management.io/addon-framework/pkg/manager/controllers/addonowner"
	"open-cluster-management.io/addon-framework/pkg/manager/controllers/addonprogressing"
	"open-cluster-management.io/addon-framework/pkg/manager/controllers/addontemplate"
	"open-cluster-management.io/addon-framework/pkg/manager/controllers/managementaddoninstallprogression"
	"open-cluster-management.io/addon-framework/pkg/utils"
)

func RunManager(ctx context.Context, kubeConfig *rest.Config) error {
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
	addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
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

	err = addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns().Informer().AddIndexers(
		cache.Indexers{
			index.ClusterManagementAddonByPlacement: index.IndexClusterManagementAddonByPlacement,
		})
	if err != nil {
		return err
	}

	err = addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().AddIndexers(
		cache.Indexers{
			index.ManagedClusterAddonByName: index.IndexManagedClusterAddonByName,
		})
	if err != nil {
		return err
	}

	dynamicInformers := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 10*time.Minute)

	addonConfigController := addonconfig.NewAddonConfigController(
		addonClient,
		addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns(),
		dynamicInformers,
		utils.BuiltInAddOnConfigGVRs,
		utils.ManagedByAddonManager,
	)
	managementAddonConfigController := managementaddonconfig.NewManagementAddonConfigController(
		addonClient,
		addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns(),
		dynamicInformers,
		utils.BuiltInAddOnConfigGVRs,
		utils.ManagedByAddonManager,
	)

	addonManagementController := addonmanagement.NewAddonManagementController(
		addonClient,
		addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns(),
		clusterInformerFactory.Cluster().V1beta1().Placements(),
		clusterInformerFactory.Cluster().V1beta1().PlacementDecisions(),
		utils.ManagedByAddonManager,
	)

	addonConfigurationController := addonconfiguration.NewAddonConfigurationController(
		addonClient,
		addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns(),
		clusterInformerFactory.Cluster().V1beta1().Placements(),
		clusterInformerFactory.Cluster().V1beta1().PlacementDecisions(),
		utils.ManagedByAddonManager,
	)

	addonOwnerController := addonowner.NewAddonOwnerController(
		addonClient,
		addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns(),
		utils.ManagedByAddonManager,
	)

	addonProgressingController := addonprogressing.NewAddonProgressingController(
		addonClient,
		addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns(),
		workInformers.Work().V1().ManifestWorks(),
		utils.ManagedByAddonManager,
	)

	mgmtAddonInstallProgressionController := managementaddoninstallprogression.NewManagementAddonInstallProgressionController(
		addonClient,
		addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns(),
		utils.ManagedByAddonManager,
	)

	addonTemplateController := addontemplate.NewAddonTemplateController(
		kubeConfig,
		hubKubeClient,
		addonClient,
		addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns(),
	)

	go addonConfigController.Run(ctx, 2)
	go managementAddonConfigController.Run(ctx, 2)
	go addonManagementController.Run(ctx, 2)
	go addonConfigurationController.Run(ctx, 2)
	go addonOwnerController.Run(ctx, 2)
	go addonProgressingController.Run(ctx, 2)
	go mgmtAddonInstallProgressionController.Run(ctx, 2)
	// There should be only one instance of addonTemplateController running, since the addonTemplateController will
	// start a goroutine for each addonTemplate it watches.
	go addonTemplateController.Run(ctx, 1)

	go clusterInformerFactory.Start(ctx.Done())
	go addonInformerFactory.Start(ctx.Done())
	go workInformers.Start(ctx.Done())
	go dynamicInformers.Start(ctx.Done())

	<-ctx.Done()
	return nil
}
