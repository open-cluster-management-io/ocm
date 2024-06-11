package apply

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestReadOnlyApply(t *testing.T) {
	cases := []struct {
		name     string
		owner    metav1.OwnerReference
		existing *unstructured.Unstructured
		required *unstructured.Unstructured
		gvr      schema.GroupVersionResource
	}{
		{
			name:     "an object",
			owner:    metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner},
			existing: nil,
			required: testingcommon.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:      schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			applier := NewReadOnlyApply()
			syncContext := testingcommon.NewFakeSyncContext(t, "test")
			obj, err := applier.Apply(
				context.TODO(), c.gvr, c.required, c.owner, nil, syncContext.Recorder())

			if err != nil {
				t.Errorf("expect no error, but got %v", obj)
			}
		})
	}
}
