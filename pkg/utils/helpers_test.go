package utils

import (
	"testing"

	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"k8s.io/apimachinery/pkg/api/equality"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

func TestMergeRelatedObject(t *testing.T) {
	cases := []struct {
		name           string
		existingObject []addonapiv1alpha1.ObjectReference
		obj            addonapiv1alpha1.ObjectReference
		modified       bool
		expected       []addonapiv1alpha1.ObjectReference
	}{
		{
			name:           "existing is nil",
			existingObject: nil,
			obj:            relatedObject("test", "testns", "resources"),
			modified:       true,
			expected:       []addonapiv1alpha1.ObjectReference{relatedObject("test", "testns", "resources")},
		},
		{
			name:           "append to existing",
			existingObject: []addonapiv1alpha1.ObjectReference{relatedObject("test", "testns", "resources")},
			obj:            relatedObject("test", "testns", "resources1"),
			modified:       true,
			expected: []addonapiv1alpha1.ObjectReference{
				relatedObject("test", "testns", "resources"),
				relatedObject("test", "testns", "resources1"),
			},
		},
		{
			name: "no update",
			existingObject: []addonapiv1alpha1.ObjectReference{
				relatedObject("test", "testns", "resources"),
				relatedObject("test", "testns", "resources1"),
			},
			obj:      relatedObject("test", "testns", "resources1"),
			modified: false,
			expected: []addonapiv1alpha1.ObjectReference{
				relatedObject("test", "testns", "resources"),
				relatedObject("test", "testns", "resources1"),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			modified := resourcemerge.BoolPtr(false)
			MergeRelatedObjects(modified, &c.existingObject, c.obj)

			if !equality.Semantic.DeepEqual(c.existingObject, c.expected) {
				t.Errorf("Unexpected related object, expect %v, but got %v", c.expected, c.existingObject)
			}

			if *modified != c.modified {
				t.Errorf("Unexpected modified value")
			}
		})
	}
}

func relatedObject(name, namespace, resource string) addonapiv1alpha1.ObjectReference {
	return addonapiv1alpha1.ObjectReference{
		Name:      name,
		Namespace: namespace,
		Resource:  resource,
	}
}
