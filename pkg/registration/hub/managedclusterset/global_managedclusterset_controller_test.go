package managedclusterset

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestSyncGlobalClusterSet(t *testing.T) {

	var editedGlobalManagedClusterSetSpec = clusterv1beta2.ManagedClusterSetSpec{
		ClusterSelector: clusterv1beta2.ManagedClusterSelector{
			SelectorType: "non-LegacyClusterSetLabel",
		},
	}

	cases := []struct {
		name               string
		existingClusterSet *clusterv1beta2.ManagedClusterSet
		validateActions    func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:               "sync global cluster set",
			existingClusterSet: newGlobalManagedClusterSet(GlobalManagedClusterSetName, GlobalManagedClusterSet.Spec, false),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:               "sync edited cluster selector in global cluster set",
			existingClusterSet: newGlobalManagedClusterSet(GlobalManagedClusterSetName, editedGlobalManagedClusterSetSpec, false),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {

				testingcommon.AssertActions(t, actions, "update")
				clusterset := actions[0].(clienttesting.UpdateAction).GetObject().(*clusterv1beta2.ManagedClusterSet)
				// if cluster selector not rollbacked, error
				if !equality.Semantic.DeepEqual(clusterset.Spec.ClusterSelector, GlobalManagedClusterSet.Spec.ClusterSelector) {
					t.Errorf("Failed to rollback global managed cluster set cluster selector after it is edited")
				}
			},
		},
		{
			name: "sync deleted global cluster set",
			// global cluster set should be created if it is deleted.
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
				clusterset := actions[0].(clienttesting.CreateAction).GetObject().(*clusterv1beta2.ManagedClusterSet)
				if clusterset.ObjectMeta.Name != GlobalManagedClusterSetName {
					t.Errorf("Failed to create global managed cluster set")
				}
			},
		},
		{
			name: "sync global cluster set with disabled annotation",
			existingClusterSet: newGlobalManagedClusterSetWithAnnotation(
				GlobalManagedClusterSetName, autoUpdateAnnotation, "false", GlobalManagedClusterSet.Spec, false),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:               "sync global cluster set with edited managed namespaces - no rollback",
			existingClusterSet: newGlobalManagedClusterSetWithManagedNamespaces(GlobalManagedClusterSetName, GlobalManagedClusterSet.Spec.ClusterSelector, false),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// Since only ClusterSelector is protected, editing ManagedNamespaces should not trigger rollback
				testingcommon.AssertNoActions(t, actions)
			},
		},
		{
			name:               "sync global cluster set with edited cluster selector and managed namespaces - only cluster selector rollback",
			existingClusterSet: newGlobalManagedClusterSetWithManagedNamespacesAndEditedSelector(GlobalManagedClusterSetName, false),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "update")
				clusterset := actions[0].(clienttesting.UpdateAction).GetObject().(*clusterv1beta2.ManagedClusterSet)
				// Only cluster selector should be rollbacked, managed namespaces should remain
				if !equality.Semantic.DeepEqual(clusterset.Spec.ClusterSelector, GlobalManagedClusterSet.Spec.ClusterSelector) {
					t.Errorf("Failed to rollback global managed cluster set cluster selector")
				}
				// Managed namespaces should be preserved
				if len(clusterset.Spec.ManagedNamespaces) == 0 {
					t.Errorf("ManagedNamespaces should be preserved during cluster selector rollback")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var objects []runtime.Object

			if c.existingClusterSet != nil {
				objects = append(objects, c.existingClusterSet)
			}

			clusterSetClient := clusterfake.NewSimpleClientset(objects...)
			informerFactory := clusterinformers.NewSharedInformerFactory(clusterSetClient, 5*time.Minute)

			if c.existingClusterSet != nil {
				if err := informerFactory.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore().Add(c.existingClusterSet); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := globalManagedClusterSetController{
				clusterSetClient: clusterSetClient.ClusterV1beta2(),
				clusterSetLister: informerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
			}

			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, testinghelpers.TestManagedClusterName), "")
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterSetClient.Actions())
		})
	}
}

func newGlobalManagedClusterSet(name string, spec clusterv1beta2.ManagedClusterSetSpec, terminating bool) *clusterv1beta2.ManagedClusterSet {
	clusterSet := &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: spec,
	}
	if terminating {
		now := metav1.Now()
		clusterSet.DeletionTimestamp = &now
	}

	return clusterSet
}

func newGlobalManagedClusterSetWithAnnotation(
	name, k, v string, spec clusterv1beta2.ManagedClusterSetSpec, terminating bool) *clusterv1beta2.ManagedClusterSet {
	clusterSet := &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				k: v,
			},
		},
		Spec: spec,
	}
	if terminating {
		now := metav1.Now()
		clusterSet.DeletionTimestamp = &now
	}

	return clusterSet
}

func newGlobalManagedClusterSetWithManagedNamespaces(
	name string, clusterSelector clusterv1beta2.ManagedClusterSelector, terminating bool) *clusterv1beta2.ManagedClusterSet {
	spec := clusterv1beta2.ManagedClusterSetSpec{
		ClusterSelector: clusterSelector,
		ManagedNamespaces: []clusterv1.ManagedNamespaceConfig{
			{
				Name: "test-namespace-1",
			},
			{
				Name: "test-namespace-2",
			},
		},
	}
	clusterSet := &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: spec,
	}
	if terminating {
		now := metav1.Now()
		clusterSet.DeletionTimestamp = &now
	}

	return clusterSet
}

func newGlobalManagedClusterSetWithManagedNamespacesAndEditedSelector(
	name string, terminating bool) *clusterv1beta2.ManagedClusterSet {
	editedSelector := clusterv1beta2.ManagedClusterSelector{
		SelectorType: "non-LegacyClusterSetLabel",
	}
	spec := clusterv1beta2.ManagedClusterSetSpec{
		ClusterSelector: editedSelector,
		ManagedNamespaces: []clusterv1.ManagedNamespaceConfig{
			{
				Name: "test-namespace-1",
			},
			{
				Name: "test-namespace-2",
			},
		},
	}
	clusterSet := &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: spec,
	}
	if terminating {
		now := metav1.Now()
		clusterSet.DeletionTimestamp = &now
	}

	return clusterSet
}
