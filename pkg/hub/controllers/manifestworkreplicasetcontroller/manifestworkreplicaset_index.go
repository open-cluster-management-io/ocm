package manifestworkreplicasetcontroller

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
	manifestWorkReplicaSetByPlacement = "manifestWorkReplicaSetByPlacement"
)

func (m *ManifestWorkReplicaSetController) placementQueueKeysFunc(obj runtime.Object) []string {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return []string{}
	}

	objs, err := m.manifestWorkReplicaSetIndexer.ByIndex(manifestWorkReplicaSetByPlacement, key)
	if err != nil {
		utilruntime.HandleError(err)
		return []string{}
	}

	var keys []string
	for _, o := range objs {
		manifestWorkReplicaSet := o.(*workapiv1alpha1.ManifestWorkReplicaSet)
		klog.V(4).Infof("enqueue manifestWorkReplicaSet %s/%s, because of placement %s", manifestWorkReplicaSet.Namespace, manifestWorkReplicaSet.Name, key)
		keys = append(keys, fmt.Sprintf("%s/%s", manifestWorkReplicaSet.Namespace, manifestWorkReplicaSet.Name))
	}

	return keys
}

func (m *ManifestWorkReplicaSetController) placementDecisionQueueKeysFunc(obj runtime.Object) []string {
	accessor, _ := meta.Accessor(obj)
	placementName, ok := accessor.GetLabels()[clusterv1beta1.PlacementLabel]
	if !ok {
		return []string{}
	}

	objs, err := m.manifestWorkReplicaSetIndexer.ByIndex(manifestWorkReplicaSetByPlacement, fmt.Sprintf("%s/%s", accessor.GetNamespace(), placementName))
	if err != nil {
		utilruntime.HandleError(err)
		return []string{}
	}

	var keys []string
	for _, o := range objs {
		manifestWorkReplicaSet := o.(*workapiv1alpha1.ManifestWorkReplicaSet)
		klog.V(4).Infof("enqueue manifestWorkReplicaSet %s/%s, because of placementDecision %s/%s",
			manifestWorkReplicaSet.Namespace, manifestWorkReplicaSet.Name, accessor.GetNamespace(), accessor.GetName())
		keys = append(keys, fmt.Sprintf("%s/%s", manifestWorkReplicaSet.Namespace, manifestWorkReplicaSet.Name))
	}

	return keys
}

// we will generate manifestwork with a lab
func (m *ManifestWorkReplicaSetController) manifestWorkQueueKeyFunc(obj runtime.Object) string {
	accessor, _ := meta.Accessor(obj)
	key, ok := accessor.GetLabels()[ManifestWorkReplicaSetControllerNameLabelKey]
	if !ok {
		return ""
	}
	return key
}

func indexManifestWorkReplicaSetByPlacement(obj interface{}) ([]string, error) {
	manifestWorkReplicaSet, ok := obj.(*workapiv1alpha1.ManifestWorkReplicaSet)

	if !ok {
		return []string{}, fmt.Errorf("obj %T is not a ManifestWorkReplicaSet", obj)
	}

	var keys []string
	for _, placementRef := range manifestWorkReplicaSet.Spec.PlacementRefs {
		key := fmt.Sprintf("%s/%s", manifestWorkReplicaSet.Namespace, placementRef.Name)
		keys = append(keys, key)
	}

	return keys, nil
}
