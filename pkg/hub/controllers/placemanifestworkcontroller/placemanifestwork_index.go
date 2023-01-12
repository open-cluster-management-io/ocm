package placemanifestworkcontroller

import (
	"fmt"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"
)

const (
	placeManifestWorkByPlacement = "placeManifestWorkByPlacement"
)

func (m *PlaceManifestWorkController) placementQueueKeysFunc(obj runtime.Object) []string {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return []string{}
	}

	objs, err := m.placeManifestWorkIndexer.ByIndex(placeManifestWorkByPlacement, key)
	if err != nil {
		utilruntime.HandleError(err)
		return []string{}
	}

	var keys []string
	for _, o := range objs {
		placeManifestWork := o.(*workapiv1alpha1.PlaceManifestWork)
		klog.V(4).Infof("enqueue placeManifestWork %s/%s, because of placement %s", placeManifestWork.Namespace, placeManifestWork.Name, key)
		keys = append(keys, fmt.Sprintf("%s/%s", placeManifestWork.Namespace, placeManifestWork.Name))
	}

	return keys
}

func (m *PlaceManifestWorkController) placementDecisionQueueKeysFunc(obj runtime.Object) []string {
	accessor, _ := meta.Accessor(obj)
	placementName, ok := accessor.GetLabels()[clusterv1beta1.PlacementLabel]
	if !ok {
		return []string{}
	}

	objs, err := m.placeManifestWorkIndexer.ByIndex(placeManifestWorkByPlacement, fmt.Sprintf("%s/%s", accessor.GetNamespace(), placementName))
	if err != nil {
		utilruntime.HandleError(err)
		return []string{}
	}

	var keys []string
	for _, o := range objs {
		placeManifestWork := o.(*workapiv1alpha1.PlaceManifestWork)
		klog.V(4).Infof("enqueue placeManifestWork %s/%s, because of placementDecision %s/%s",
			placeManifestWork.Namespace, placeManifestWork.Name, accessor.GetNamespace(), accessor.GetName())
		keys = append(keys, fmt.Sprintf("%s/%s", placeManifestWork.Namespace, placeManifestWork.Name))
	}

	return keys
}

// we will generate manifestwork with a lab
func (m *PlaceManifestWorkController) manifestWorkQueueKeyFunc(obj runtime.Object) string {
	accessor, _ := meta.Accessor(obj)
	key, ok := accessor.GetLabels()[PlaceManifestWorkControllerNameLabelKey]
	if !ok {
		return ""
	}
	return key
}

func indexPlacementManifestWorkByPlacement(obj interface{}) ([]string, error) {
	placeManifestWork, ok := obj.(*workapiv1alpha1.PlaceManifestWork)

	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a PlaceManifestWork", obj)
	}

	key := fmt.Sprintf("%s/%s", placeManifestWork.Namespace, placeManifestWork.Spec.PlacementRef.Name)
	return []string{key}, nil
}
