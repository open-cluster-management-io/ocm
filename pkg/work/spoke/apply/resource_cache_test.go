package apply

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestCache(t *testing.T) {
	cache := NewResourceCache()
	if cache == nil {
		t.Fatal("expected non-nil resource cache")
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	})
	obj.SetResourceVersion("12345")

	obj.SetName("test")
	obj.SetNamespace("default")

	// Test UpdateCachedResourceMetadata
	cache.UpdateCachedResourceMetadata(obj, obj)

	// Test SafeToSkipApply
	if !cache.SafeToSkipApply(obj, obj) {
		t.Fatal("expected SafeToSkipApply to return true for identical objects")
	}

	// Test SafeToSkipApply with different objects
	obj2 := &unstructured.Unstructured{}
	obj2.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	})
	obj2.SetName("test2")
	if cache.SafeToSkipApply(obj, obj2) {
		t.Fatal("expected SafeToSkipApply to return false for different objects")
	}

	obj3 := obj.DeepCopy()
	obj3.SetResourceVersion("54321")
	if cache.SafeToSkipApply(obj, obj3) {
		t.Fatal("expected SafeToSkipApply to return false for objects with different resource versions")
	}
	cache.UpdateCachedResourceMetadata(obj, obj3)
	if !cache.SafeToSkipApply(obj, obj3) {
		t.Fatal("expected SafeToSkipApply to return true after updating cache with new resource version")
	}
}

func TestCurrentReadWriteCache(t *testing.T) {

	// cache := resourceapply.NewResourceCache()
	cache := NewResourceCache()
	if cache == nil {
		t.Fatal("expected non-nil resource cache")
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	})
	obj.SetName("test")
	obj.SetNamespace("default")

	errCnt := 0
	for i := 0; i < 10000; i++ {
		go func() {
			obj.SetResourceVersion(fmt.Sprintf("12345%d", i))
			// Test UpdateCachedResourceMetadata
			cache.UpdateCachedResourceMetadata(obj, obj)
			// Test SafeToSkipApply
			if !cache.SafeToSkipApply(obj, obj) {
				errCnt++
			}
		}()

	}

	if errCnt > 0 {
		t.Fatal("expected no errors in concurrent cache access, but got", errCnt)
	}

}
