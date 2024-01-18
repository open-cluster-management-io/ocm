package apply

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	testingcommon "open-cluster-management.io/sdk-go/pkg/testing"

	"open-cluster-management.io/ocm/pkg/work/spoke/spoketesting"
)

func TestCreateOnlyApply(t *testing.T) {
	cases := []struct {
		name            string
		owner           metav1.OwnerReference
		existing        *unstructured.Unstructured
		required        *unstructured.Unstructured
		gvr             schema.GroupVersionResource
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:     "create a non exist object",
			owner:    metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner},
			existing: nil,
			required: spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:      schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get", "create")

				obj := actions[1].(clienttesting.CreateActionImpl).Object.(*unstructured.Unstructured)
				owners := obj.GetOwnerReferences()
				if len(owners) != 1 {
					t.Errorf("Expect 1 owners, but have %d", len(owners))
				}

				if owners[0].UID != defaultOwner {
					t.Errorf("Owner UId is not correct, got %s", owners[0].UID)
				}
			},
		},
		{
			name:     "create an already existing object",
			owner:    metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: defaultOwner},
			existing: spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			required: spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:      schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "get")

				action := actions[0].(clienttesting.GetActionImpl)
				if action.Namespace != "ns1" || action.Name != "test" {
					t.Errorf("Expect get secret ns1/test, but %s/%s", action.Namespace, action.Name)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var objects []runtime.Object
			if c.existing != nil {
				objects = append(objects, c.existing)
			}
			scheme := runtime.NewScheme()
			dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, objects...)
			applier := NewCreateOnlyApply(dynamicClient)

			syncContext := testingcommon.NewFakeSyncContext(t, "test")
			obj, err := applier.Apply(
				context.TODO(), c.gvr, c.required, c.owner, nil, syncContext.Recorder())

			if err != nil {
				t.Errorf("expect no error, but got %v", obj)
			}

			accessor, err := meta.Accessor(obj)
			if err != nil {
				t.Errorf("type %t cannot be accessed: %v", obj, err)
			}
			if accessor.GetNamespace() != c.required.GetNamespace() || accessor.GetName() != c.required.GetName() {
				t.Errorf("Expect resource %s/%s, but %s/%s",
					c.required.GetNamespace(), c.required.GetName(), accessor.GetNamespace(), accessor.GetName())
			}
			c.validateActions(t, dynamicClient.Actions())
		})
	}
}
