package apply

import (
	"context"
	"fmt"
	"testing"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
)

func TestServerSideApply(t *testing.T) {
	cases := []struct {
		name            string
		owner           metav1.OwnerReference
		existing        *unstructured.Unstructured
		required        *unstructured.Unstructured
		gvr             schema.GroupVersionResource
		conflict        bool
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:            "server side apply successfully",
			owner:           metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: "testowner"},
			existing:        nil,
			required:        spoketesting.NewUnstructured("v1", "Namespace", "", "test"),
			gvr:             schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {},
		},
		{
			name:     "server side apply successfully conflict",
			owner:    metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: "testowner"},
			existing: spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			required: spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:      schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			conflict: true,
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("Expect 1 actions, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "patch")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			if c.existing != nil {
				objects = append(objects, c.existing)
			}
			scheme := runtime.NewScheme()
			dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, objects...)

			// The fake client does not support PatchType ApplyPatchType, add an reactor to mock apply patch
			// see issue: https://github.com/kubernetes/kubernetes/issues/103816
			reactor := &reactor{}
			reactors := []clienttesting.Reactor{reactor}
			dynamicClient.Fake.ReactionChain = append(reactors, dynamicClient.Fake.ReactionChain...)

			applier := NewServerSideApply(dynamicClient)

			syncContext := spoketesting.NewFakeSyncContext(t, "test")
			option := &workapiv1.ManifestConfigOption{
				UpdateStrategy: &workapiv1.UpdateStrategy{
					Type: workapiv1.UpdateStrategyTypeServerSideApply,
				},
			}
			obj, err := applier.Apply(
				context.TODO(), c.gvr, c.required, c.owner, option, syncContext.Recorder())
			c.validateActions(t, dynamicClient.Actions())
			if !c.conflict {
				if err != nil {
					t.Errorf("expect no error, but got %v", err)
				}

				accessor, err := meta.Accessor(obj)
				if err != nil {
					t.Errorf("type %t cannot be accessed: %v", obj, err)
				}
				if accessor.GetNamespace() != c.required.GetNamespace() || accessor.GetName() != c.required.GetName() {
					t.Errorf("Expect resource %s/%s, but %s/%s",
						c.required.GetNamespace(), c.required.GetName(), accessor.GetNamespace(), accessor.GetName())
				}
				return
			}

			var ssaConflict *ServerSideApplyConflictError
			if !errors.As(err, &ssaConflict) {
				t.Errorf("expect serverside apply conflict error, but got %v", err)
			}

		})
	}
}

type reactor struct {
}

func (r *reactor) Handles(action clienttesting.Action) bool {
	switch action := action.(type) {
	case clienttesting.PatchActionImpl:
		if action.GetPatchType() == types.ApplyPatchType {
			return true
		}

	default:
		return false
	}
	return true
}

// React handles the action and returns results.  It may choose to
// delegate by indicated handled=false.
func (r *reactor) React(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
	switch action.GetResource().Resource {
	case "namespaces":
		return true, spoketesting.NewUnstructured("v1", "Namespace", "", "test"), nil
	case "secrets":
		return true, nil, apierrors.NewApplyConflict([]metav1.StatusCause{
			{
				Type:    metav1.CauseTypeFieldManagerConflict,
				Message: "field managed configl",
				Field:   "metadata.annotations",
			},
		}, "server side apply secret failed")
	}

	return true, nil, fmt.Errorf("PatchType is not supported")
}
