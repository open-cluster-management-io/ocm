package manager

import (
	"context"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"

	"open-cluster-management.io/addon-framework/pkg/index"
	"open-cluster-management.io/addon-framework/pkg/manager/controllers/addonconfiguration"
	"open-cluster-management.io/addon-framework/pkg/manager/controllers/addonmanagement"
	"open-cluster-management.io/addon-framework/pkg/manager/controllers/addonstatus"
)

func RunManager(ctx context.Context, kubeConfig *rest.Config) error {
	hubClusterClient, err := clusterclientset.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	addonClient, err := addonv1alpha1client.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	clusterInformerFactory := clusterinformers.NewSharedInformerFactory(hubClusterClient, 30*time.Minute)
	addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)

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

	addonManagementController := addonmanagement.NewAddonManagementController(
		addonClient,
		addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns(),
		clusterInformerFactory.Cluster().V1beta1().Placements(),
		clusterInformerFactory.Cluster().V1beta1().PlacementDecisions(),
	)

	addonConfigurationController := addonconfiguration.NewAddonConfigurationController(
		addonClient,
		addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns(),
		clusterInformerFactory.Cluster().V1beta1().Placements(),
		clusterInformerFactory.Cluster().V1beta1().PlacementDecisions(),
	)

	addonStatusController := addonstatus.NewAddonStatusController(
		addonClient,
		addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns(),
	)

	go addonManagementController.Run(ctx, 2)
	go addonConfigurationController.Run(ctx, 2)
	go addonStatusController.Run(ctx, 2)

	go clusterInformerFactory.Start(ctx.Done())
	go addonInformerFactory.Start(ctx.Done())

	<-ctx.Done()
	return nil
}
