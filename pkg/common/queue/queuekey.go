package queue

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

func FileterByLabel(key string) func(obj interface{}) bool {
	return func(obj interface{}) bool {
		accessor, _ := meta.Accessor(obj)
		return len(accessor.GetLabels()) > 0 && len(accessor.GetLabels()[key]) > 0
	}
}

func FileterByLabelKeyValue(key, value string) factory.EventFilterFunc {
	return func(obj interface{}) bool {
		accessor, _ := meta.Accessor(obj)
		return len(accessor.GetLabels()) > 0 && accessor.GetLabels()[key] == value
	}
}

func FilterByNames(names ...string) factory.EventFilterFunc {
	return func(obj interface{}) bool {
		accessor, _ := meta.Accessor(obj)
		for _, name := range names {
			if accessor.GetName() == name {
				return true
			}
		}
		return false
	}
}

func UnionFilter(filters ...factory.EventFilterFunc) factory.EventFilterFunc {
	return func(obj interface{}) bool {
		for _, filter := range filters {
			if !filter(obj) {
				return false
			}
		}
		return true
	}
}

func QueueKeyByLabel(key string) factory.ObjectQueueKeysFunc {
	return func(obj runtime.Object) []string {
		accessor, _ := meta.Accessor(obj)
		if len(accessor.GetLabels()) == 0 || len(accessor.GetLabels()[key]) == 0 {
			return []string{}
		}
		return []string{accessor.GetLabels()[key]}
	}
}

func QueueKeyByMetaName(obj runtime.Object) []string {
	accessor, _ := meta.Accessor(obj)
	return []string{accessor.GetName()}
}

func QueueKeyByMetaNamespace(obj runtime.Object) []string {
	accessor, _ := meta.Accessor(obj)
	return []string{accessor.GetNamespace()}
}

func QueueKeyByMetaNamespaceName(obj runtime.Object) []string {
	key, _ := cache.MetaNamespaceKeyFunc(obj)
	return []string{key}
}
