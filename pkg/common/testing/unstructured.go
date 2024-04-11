package testing

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func NewUnstructured(apiVersion, kind, namespace, name string, owners ...metav1.OwnerReference) *unstructured.Unstructured {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
		},
	}

	u.SetOwnerReferences(owners)

	return u
}

func NewUnstructuredWithContent(
	apiVersion, kind, namespace, name string, content map[string]interface{}) *unstructured.Unstructured {
	object := NewUnstructured(apiVersion, kind, namespace, name)
	for key, val := range content {
		object.Object[key] = val
	}

	return object
}

func NewUnstructuredSecret(namespace, name string, terminated bool, uid string, owners ...metav1.OwnerReference) *unstructured.Unstructured {
	u := NewUnstructured("v1", "Secret", namespace, name, owners...)
	if terminated {
		now := metav1.Now()
		u.SetDeletionTimestamp(&now)
	}
	if uid != "" {
		u.SetUID(types.UID(uid))
	}
	return u
}
