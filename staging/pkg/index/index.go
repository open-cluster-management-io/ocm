package index

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

const (
	ClusterManagementAddonByPlacement = "clusterManagementAddonByPlacement"
	ManagedClusterAddonByName         = "managedClusterAddonByName"
)

func IndexClusterManagementAddonByPlacement(obj interface{}) ([]string, error) {
	cma, ok := obj.(*addonv1alpha1.ClusterManagementAddOn)

	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a ClusterManagementAddon", obj)
	}

	var keys []string
	if cma.Spec.InstallStrategy.Type == "" || cma.Spec.InstallStrategy.Type == addonv1alpha1.AddonInstallStrategyManual {
		return keys, nil
	}

	for _, placement := range cma.Spec.InstallStrategy.Placements {
		key := fmt.Sprintf("%s/%s", placement.PlacementRef.Namespace, placement.PlacementRef.Name)
		keys = append(keys, key)
	}

	return keys, nil
}

func IndexManagedClusterAddonByName(obj interface{}) ([]string, error) {
	mca, ok := obj.(*addonv1alpha1.ManagedClusterAddOn)

	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a ManagedClusterAddon", obj)
	}

	return []string{mca.Name}, nil
}

func ClusterManagementAddonByPlacementQueueKey(
	cmai addoninformerv1alpha1.ClusterManagementAddOnInformer) func(obj runtime.Object) []string {
	return func(obj runtime.Object) []string {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
		if err != nil {
			utilruntime.HandleError(err)
			return []string{}
		}

		objs, err := cmai.Informer().GetIndexer().ByIndex(ClusterManagementAddonByPlacement, key)
		if err != nil {
			utilruntime.HandleError(err)
			return []string{}
		}

		var keys []string
		for _, o := range objs {
			cma := o.(*addonv1alpha1.ClusterManagementAddOn)
			klog.V(4).Infof("enqueue ClusterManagementAddon %s, because of placement %s", cma.Name, key)
			keys = append(keys, cma.Name)
		}

		return keys
	}
}

func ClusterManagementAddonByPlacementDecisionQueueKey(
	cmai addoninformerv1alpha1.ClusterManagementAddOnInformer) func(obj runtime.Object) []string {
	return func(obj runtime.Object) []string {
		accessor, _ := meta.Accessor(obj)
		placementName, ok := accessor.GetLabels()[clusterv1beta1.PlacementLabel]
		if !ok {
			return []string{}
		}

		objs, err := cmai.Informer().GetIndexer().ByIndex(ClusterManagementAddonByPlacement,
			fmt.Sprintf("%s/%s", accessor.GetNamespace(), placementName))
		if err != nil {
			utilruntime.HandleError(err)
			return []string{}
		}

		var keys []string
		for _, o := range objs {
			cma := o.(*addonv1alpha1.ClusterManagementAddOn)
			klog.V(4).Infof("enqueue ClusterManagementAddon %s, because of placementDecision %s/%s",
				cma.Name, accessor.GetNamespace(), accessor.GetName())
			keys = append(keys, cma.Name)
		}

		return keys
	}
}
