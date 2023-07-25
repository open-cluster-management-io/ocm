package patcher

import (
	"context"
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestAddFinalizer(t *testing.T) {
	cases := []struct {
		name            string
		obj             *clusterv1.ManagedCluster
		finalizers      []string
		opts            PatchOptions
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:       "add finalizer",
			obj:        newManagedClusterWithFinalizer(),
			finalizers: []string{"test-finalizer"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertFinalizers(t, managedCluster, []string{"test-finalizer"})
			},
		},
		{
			name:       "multiple finalizers",
			obj:        newManagedClusterWithFinalizer("test-finalizer-1"),
			finalizers: []string{"test-finalizer", "test-finalizer-1"},
			opts:       PatchOptions{IgnoreResourceVersion: true},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertFinalizers(t, managedCluster, []string{"test-finalizer-1", "test-finalizer"})
			},
		},
		{
			name:            "no action",
			obj:             newManagedClusterWithFinalizer("test-finalizer-1", "test-finalizer"),
			finalizers:      []string{"test-finalizer"},
			validateActions: testingcommon.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.obj)
			patcher := NewPatcher[
				*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
				clusterClient.ClusterV1().ManagedClusters()).WithOptions(c.opts)
			if _, err := patcher.AddFinalizer(context.TODO(), c.obj, c.finalizers...); err != nil {
				t.Error(err)
			}
			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestRemoveFinalizer(t *testing.T) {
	cases := []struct {
		name            string
		obj             *clusterv1.ManagedCluster
		finalizers      []string
		opts            PatchOptions
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:       "remove finalizer",
			obj:        newManagedClusterWithFinalizer("test-finalizer", "test-finalizer-1"),
			finalizers: []string{"test-finalizer"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertFinalizers(t, managedCluster, []string{"test-finalizer-1"})
			},
		},
		{
			name:       "remove multiple finalizers",
			obj:        newManagedClusterWithFinalizer("test-finalizer", "test-finalizer-1", "test-finalizer-2"),
			finalizers: []string{"test-finalizer", "test-finalizer-2"},
			opts:       PatchOptions{IgnoreResourceVersion: true},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertFinalizers(t, managedCluster, []string{"test-finalizer-1"})
			},
		},
		{
			name:       "remove multiple finalizers, some unmatched",
			obj:        newManagedClusterWithFinalizer("test-finalizer", "test-finalizer-1", "test-finalizer-2"),
			finalizers: []string{"test-finalizer", "test-finalizer-3"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				testinghelpers.AssertFinalizers(t, managedCluster, []string{"test-finalizer-1", "test-finalizer-2"})
			},
		},
		{
			name:            "no action",
			obj:             newManagedClusterWithFinalizer("test-finalizer-1"),
			finalizers:      []string{"test-finalizer"},
			validateActions: testingcommon.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.obj)
			patcher := NewPatcher[
				*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
				clusterClient.ClusterV1().ManagedClusters()).WithOptions(c.opts)
			if err := patcher.RemoveFinalizer(context.TODO(), c.obj, c.finalizers...); err != nil {
				t.Error(err)
			}
			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestPatchSpec(t *testing.T) {
	cases := []struct {
		name            string
		obj             *clusterv1.ManagedCluster
		newObj          *clusterv1.ManagedCluster
		opts            PatchOptions
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:   "patch spec",
			obj:    newManagedClusterWithTaint(clusterv1.Taint{Key: "key1"}),
			newObj: newManagedClusterWithTaint(clusterv1.Taint{Key: "key2"}),
			opts:   PatchOptions{IgnoreResourceVersion: true},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				if !equality.Semantic.DeepEqual(managedCluster.Spec, newManagedClusterWithTaint(clusterv1.Taint{Key: "key2"}).Spec) {
					t.Errorf("not patched correctly got %v", managedCluster.Spec)
				}
			},
		},
		{
			name:            "no patch",
			obj:             newManagedClusterWithTaint(clusterv1.Taint{Key: "key1"}),
			newObj:          newManagedClusterWithTaint(clusterv1.Taint{Key: "key1"}),
			validateActions: testingcommon.AssertNoActions,
		},
		{
			name:            "no patch with status change",
			obj:             newManagedClusterWithConditions(metav1.Condition{Type: "Type1"}),
			newObj:          newManagedClusterWithConditions(metav1.Condition{Type: "Type2"}),
			validateActions: testingcommon.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.obj)
			patcher := NewPatcher[
				*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
				clusterClient.ClusterV1().ManagedClusters()).WithOptions(c.opts)
			if _, err := patcher.PatchSpec(context.TODO(), c.obj, c.newObj.Spec, c.obj.Spec); err != nil {
				t.Error(err)
			}
			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestPatchStatus(t *testing.T) {
	cases := []struct {
		name            string
		obj             *clusterv1.ManagedCluster
		newObj          *clusterv1.ManagedCluster
		opts            PatchOptions
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:   "patch status",
			obj:    newManagedClusterWithConditions(metav1.Condition{Type: "Type1"}),
			newObj: newManagedClusterWithConditions(metav1.Condition{Type: "Type2"}),
			opts:   PatchOptions{IgnoreResourceVersion: true},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				if !equality.Semantic.DeepEqual(managedCluster.Status, newManagedClusterWithConditions(metav1.Condition{Type: "Type2"}).Status) {
					t.Errorf("not patched correctly got %v", managedCluster.Status)
				}
			},
		},
		{
			name:            "no patch",
			obj:             newManagedClusterWithConditions(metav1.Condition{Type: "Type1"}),
			newObj:          newManagedClusterWithConditions(metav1.Condition{Type: "Type1"}),
			validateActions: testingcommon.AssertNoActions,
		},
		{
			name:            "no patch with spec change",
			obj:             newManagedClusterWithTaint(clusterv1.Taint{Key: "key1"}),
			newObj:          newManagedClusterWithTaint(clusterv1.Taint{Key: "key2"}),
			validateActions: testingcommon.AssertNoActions,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.obj)
			patcher := NewPatcher[
				*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
				clusterClient.ClusterV1().ManagedClusters()).WithOptions(c.opts)
			if _, err := patcher.PatchStatus(context.TODO(), c.obj, c.newObj.Status, c.obj.Status); err != nil {
				t.Error(err)
			}
			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func TestPatchLabelAnnotations(t *testing.T) {
	cases := []struct {
		name            string
		obj             *clusterv1.ManagedCluster
		newObj          *clusterv1.ManagedCluster
		opts            PatchOptions
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:            "empty value",
			obj:             newManagedClusterWithLabelAnnotations(nil, nil),
			newObj:          newManagedClusterWithLabelAnnotations(nil, nil),
			validateActions: testingcommon.AssertNoActions,
		},
		{
			name:   "add labels",
			obj:    newManagedClusterWithLabelAnnotations(nil, nil),
			newObj: newManagedClusterWithLabelAnnotations(map[string]string{"key": "value"}, nil),
			opts:   PatchOptions{IgnoreResourceVersion: true},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				if !equality.Semantic.DeepEqual(managedCluster.Labels, map[string]string{"key": "value"}) {
					t.Errorf("not patched correctly got %v", managedCluster.Labels)
				}
			},
		},
		{
			name:   "add annotation",
			obj:    newManagedClusterWithLabelAnnotations(nil, nil),
			newObj: newManagedClusterWithLabelAnnotations(nil, map[string]string{"key": "value"}),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				managedCluster := &clusterv1.ManagedCluster{}
				err := json.Unmarshal(patch, managedCluster)
				if err != nil {
					t.Fatal(err)
				}
				if !equality.Semantic.DeepEqual(managedCluster.Annotations, map[string]string{"key": "value"}) {
					t.Errorf("not patched correctly got %v", managedCluster.Annotations)
				}
			},
		},
		{
			name:            "no update",
			obj:             newManagedClusterWithLabelAnnotations(nil, map[string]string{"key": "value"}),
			newObj:          newManagedClusterWithLabelAnnotations(nil, map[string]string{"key": "value"}),
			validateActions: testingcommon.AssertNoActions,
		},
		{
			name:   "remove label",
			obj:    newManagedClusterWithLabelAnnotations(map[string]string{"key": "value", "key1": "value1"}, nil),
			newObj: newManagedClusterWithLabelAnnotations(map[string]string{"key": "value"}, nil),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "patch")
				patch := actions[0].(clienttesting.PatchAction).GetPatch()
				labelPatch := map[string]interface{}{}
				err := json.Unmarshal(patch, &labelPatch)
				if err != nil {
					t.Fatal(err)
				}
				if !equality.Semantic.DeepEqual(
					labelPatch["metadata"],
					map[string]interface{}{"uid": "", "resourceVersion": "", "labels": map[string]interface{}{"key1": nil}}) {
					t.Errorf("not patched correctly got %v", labelPatch)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.obj)
			patcher := NewPatcher[
				*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
				clusterClient.ClusterV1().ManagedClusters()).WithOptions(c.opts)
			if _, err := patcher.PatchLabelAnnotations(context.TODO(), c.obj, c.newObj.ObjectMeta, c.obj.ObjectMeta); err != nil {
				t.Error(err)
			}
			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func newManagedClusterWithFinalizer(finalizers ...string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test",
			Finalizers: finalizers,
		},
	}
}

func newManagedClusterWithLabelAnnotations(labels map[string]string, annotations map[string]string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Labels:      labels,
			Annotations: annotations,
		},
	}
}

func newManagedClusterWithTaint(taints ...clusterv1.Taint) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: clusterv1.ManagedClusterSpec{
			Taints: taints,
		},
	}
}

func newManagedClusterWithConditions(conds ...metav1.Condition) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Status: clusterv1.ManagedClusterStatus{
			Conditions: conds,
		},
	}
}
