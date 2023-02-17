package manager

import (
	"context"
	"k8s.io/client-go/rest"
	"open-cluster-management.io/addon-framework/pkg/manager/controllers/addonmanagement"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	"time"
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

	addonManagementController := addonmanagement.NewAddonManagementController(
		addonClient,
		addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
		addonInformerFactory.Addon().V1alpha1().ClusterManagementAddOns(),
		clusterInformerFactory.Cluster().V1beta1().Placements(),
		clusterInformerFactory.Cluster().V1beta1().PlacementDecisions(),
	)

	go addonManagementController.Run(ctx, 2)

	go clusterInformerFactory.Start(ctx.Done())
	go addonInformerFactory.Start(ctx.Done())
	return nil
}
