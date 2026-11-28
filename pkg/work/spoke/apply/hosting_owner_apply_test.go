package apply

import (
	"encoding/json"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	workapiv1 "open-cluster-management.io/api/work/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestExpectedHostingOwner(t *testing.T) {
	if got := ExpectedHostingOwner(map[string]string{HostingOwnerLabel: "cluster-a_proto192"}); got != "cluster-a/proto192" {
		t.Errorf("got %q", got)
	}
	if got := ExpectedHostingOwner(map[string]string{"other": "x"}); got != "" {
		t.Errorf("unlabelled Work must not enforce, got %q", got)
	}
}

func TestHostingOwnerApply(t *testing.T) {
	const expected = "cluster-a/proto192"
	gvr := schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}
	clusterRole := func(owner string, uid string) *unstructured.Unstructured {
		u := testingcommon.NewUnstructured("rbac.authorization.k8s.io/v1", "ClusterRole", "", "proto192")
		if owner != "" {
			u.SetAnnotations(map[string]string{HostingAddonAnnotation: owner})
		}
		u.SetUID(types.UID(uid))
		u.SetResourceVersion("7")
		return u
	}
	workOwner := metav1.OwnerReference{APIVersion: "work.open-cluster-management.io/v1", Kind: "AppliedManifestWork",
		Name: "amw", UID: "amw-uid"}
	orphanOwner := workOwner
	orphanOwner.UID = "amw-uid-"
	ssa := &workapiv1.ManifestConfigOption{UpdateStrategy: &workapiv1.UpdateStrategy{
		Type:            workapiv1.UpdateStrategyTypeServerSideApply,
		ServerSideApply: &workapiv1.ServerSideApplyConfig{FieldManager: "work-agent-cluster-a-proto192"},
	}}

	cases := []struct {
		name     string
		existing *unstructured.Unstructured
		required *unstructured.Unstructured
		owner    metav1.OwnerReference
		verbs    []string
		conflict bool
		wantErr  bool
		check    func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "absent: created with a create request, owner annotation and work owner", required: clusterRole(expected, ""),
			owner: workOwner, verbs: []string{"get", "create"},
			check: func(t *testing.T, actions []clienttesting.Action) {
				create := actions[1].(clienttesting.CreateActionImpl)
				obj := create.Object.(*unstructured.Unstructured)
				if obj.GetAnnotations()[HostingAddonAnnotation] != expected {
					t.Errorf("created without owner annotation: %v", obj.GetAnnotations())
				}
				if refs := obj.GetOwnerReferences(); len(refs) != 1 || refs[0].UID != "amw-uid" {
					t.Errorf("owner references = %v", refs)
				}
				if create.CreateOptions.FieldManager != "work-agent-cluster-a-proto192" {
					t.Errorf("field manager = %q", create.CreateOptions.FieldManager)
				}
			},
		},
		{
			name: "absent with orphan policy: created without owner reference", required: clusterRole(expected, ""),
			owner: orphanOwner, verbs: []string{"get", "create"},
			check: func(t *testing.T, actions []clienttesting.Action) {
				obj := actions[1].(clienttesting.CreateActionImpl).Object.(*unstructured.Unstructured)
				if refs := obj.GetOwnerReferences(); len(refs) != 0 {
					t.Errorf("orphaned resource got owner references %v", refs)
				}
			},
		},
		{
			name: "owned: forced apply bound to the read UID and resourceVersion", existing: clusterRole(expected, "live-uid"),
			required: clusterRole(expected, ""), owner: workOwner, verbs: []string{"get", "patch", "patch"},
			check: func(t *testing.T, actions []clienttesting.Action) {
				apply := actions[1].(clienttesting.PatchActionImpl)
				if apply.PatchType != types.ApplyPatchType {
					t.Fatalf("patch type = %s", apply.PatchType)
				}
				if apply.PatchOptions.Force == nil || !*apply.PatchOptions.Force ||
					apply.PatchOptions.FieldManager != "work-agent-cluster-a-proto192" {
					t.Errorf("apply options = %+v", apply.PatchOptions)
				}
				body := map[string]any{}
				if err := json.Unmarshal(apply.Patch, &body); err != nil {
					t.Fatal(err)
				}
				md := body["metadata"].(map[string]any)
				if md["uid"] != "live-uid" || md["resourceVersion"] != "7" {
					t.Errorf("apply not bound to the read object: uid=%v resourceVersion=%v", md["uid"], md["resourceVersion"])
				}
			},
		},
		{
			name: "unannotated live resource: conflict, no write", existing: clusterRole("", "live-uid"),
			required: clusterRole(expected, ""), owner: workOwner, verbs: []string{"get"}, conflict: true,
		},
		{
			name: "owned by another instance: conflict, no write", existing: clusterRole("cluster-b/proto192", "live-uid"),
			required: clusterRole(expected, ""), owner: workOwner, verbs: []string{"get"}, conflict: true,
		},
		{
			name: "manifest without the expected owner: rejected before any API call", required: clusterRole("cluster-b/proto192", ""),
			owner: workOwner, verbs: nil, wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var objects []runtime.Object
			if c.existing != nil {
				objects = append(objects, c.existing)
			}
			client := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
			// The fake client can't run apply patches; answer them with the live object.
			client.PrependReactor("patch", "clusterroles", func(clienttesting.Action) (bool, runtime.Object, error) {
				return true, c.existing.DeepCopy(), nil
			})

			_, err := NewHostingOwnerApply(client, expected).Apply(
				t.Context(), gvr, c.required, c.owner, ssa, testingcommon.NewFakeSyncContext(t, "test").Recorder())

			var conflict *HostingOwnerConflictError
			switch {
			case c.conflict && !errors.As(err, &conflict):
				t.Fatalf("want HostingOwnerConflictError, got %v", err)
			case c.wantErr && err == nil:
				t.Fatal("want an error")
			case !c.conflict && !c.wantErr && err != nil:
				t.Fatalf("unexpected error: %v", err)
			}
			testingcommon.AssertActions(t, client.Actions(), c.verbs...)
			if c.check != nil {
				c.check(t, client.Actions())
			}
		})
	}
}
