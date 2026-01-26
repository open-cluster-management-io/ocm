package clusterprofile

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	cpfake "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned/fake"
	cpinformers "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	v1 "open-cluster-management.io/api/cluster/v1"
	v1beta2 "open-cluster-management.io/api/cluster/v1beta2"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
	testinghelpers "open-cluster-management.io/ocm/pkg/registration/helpers/testing"
)

func TestSyncClusterProfile(t *testing.T) {
	managedCluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testinghelpers.TestManagedClusterName,
			Labels: map[string]string{v1beta2.ClusterSetLabel: "default"},
		},
		Status: v1.ManagedClusterStatus{
			Version: v1.ManagedClusterVersion{
				Kubernetes: "v1.25.3",
			},
			ClusterClaims: []v1.ManagedClusterClaim{
				{Name: "claim1", Value: "value1"},
			},
			Conditions: []metav1.Condition{
				{
					Type:   v1.ManagedClusterConditionAvailable,
					Status: metav1.ConditionTrue,
				},
				{
					Type:   v1.ManagedClusterConditionJoined,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	managedClusterSet := &v1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
		Spec: v1beta2.ManagedClusterSetSpec{
			ClusterSelector: v1beta2.ManagedClusterSelector{
				SelectorType: v1beta2.ExclusiveClusterSetLabel,
			},
		},
	}

	boundBinding := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "test-ns",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "default",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	unboundBinding := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "test-ns",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "default",
		},
		Status: v1beta2.ManagedClusterSetBindingStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.ClusterSetBindingBoundType,
					Status: metav1.ConditionFalse,
				},
			},
		},
	}

	cases := []struct {
		name            string
		key             string
		mc              []runtime.Object
		mcs             []runtime.Object
		mcsb            []runtime.Object
		cp              []runtime.Object
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name: "create clusterprofile in custom namespace",
			key:  "test-ns/" + testinghelpers.TestManagedClusterName,
			mc:   []runtime.Object{managedCluster},
			mcs:  []runtime.Object{managedClusterSet},
			mcsb: []runtime.Object{boundBinding},
			cp:   []runtime.Object{},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "create")
				clusterprofile := actions[0].(clienttesting.CreateAction).GetObject().(*cpv1alpha1.ClusterProfile)
				if clusterprofile.Namespace != "test-ns" {
					t.Errorf("expect namespace test-ns but get %s", clusterprofile.Namespace)
				}
				if clusterprofile.Name != testinghelpers.TestManagedClusterName {
					t.Errorf("expect name %s but get %s", testinghelpers.TestManagedClusterName, clusterprofile.Name)
				}
				if clusterprofile.Spec.ClusterManager.Name != ClusterProfileManagerName {
					t.Errorf("expect cluster manager %s but get %s", ClusterProfileManagerName, clusterprofile.Spec.ClusterManager.Name)
				}
			},
		},
		{
			name: "update existing clusterprofile",
			key:  "test-ns/" + testinghelpers.TestManagedClusterName,
			mc:   []runtime.Object{managedCluster},
			mcs:  []runtime.Object{managedClusterSet},
			mcsb: []runtime.Object{boundBinding},
			cp: []runtime.Object{&cpv1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testinghelpers.TestManagedClusterName,
					Namespace: "test-ns",
					Labels: map[string]string{
						cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
					},
				},
				Spec: cpv1alpha1.ClusterProfileSpec{
					DisplayName: testinghelpers.TestManagedClusterName,
					ClusterManager: cpv1alpha1.ClusterManager{
						Name: ClusterProfileManagerName,
					},
				},
			}},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				// Should patch labels and status
				if len(actions) == 0 {
					// No changes needed
					testingcommon.AssertNoActions(t, actions)
					return
				}
				// Could have patches
				for _, action := range actions {
					if action.GetVerb() != "patch" {
						t.Errorf("expect patch action but get %s", action.GetVerb())
					}
				}
			},
		},
		{
			name: "delete clusterprofile when binding is unbound",
			key:  "test-ns/" + testinghelpers.TestManagedClusterName,
			mc:   []runtime.Object{managedCluster},
			mcs:  []runtime.Object{managedClusterSet},
			mcsb: []runtime.Object{unboundBinding},
			cp: []runtime.Object{&cpv1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testinghelpers.TestManagedClusterName,
					Namespace: "test-ns",
					Labels: map[string]string{
						cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
					},
				},
				Spec: cpv1alpha1.ClusterProfileSpec{
					ClusterManager: cpv1alpha1.ClusterManager{
						Name: ClusterProfileManagerName,
					},
				},
			}},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "delete")
			},
		},
		{
			name: "delete clusterprofile when cluster is deleted",
			key:  "test-ns/" + testinghelpers.TestManagedClusterName,
			mc:   []runtime.Object{testinghelpers.NewDeletingManagedCluster()},
			mcs:  []runtime.Object{managedClusterSet},
			mcsb: []runtime.Object{boundBinding},
			cp: []runtime.Object{&cpv1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testinghelpers.TestManagedClusterName,
					Namespace: "test-ns",
					Labels: map[string]string{
						cpv1alpha1.LabelClusterManagerKey: ClusterProfileManagerName,
					},
				},
				Spec: cpv1alpha1.ClusterProfileSpec{
					ClusterManager: cpv1alpha1.ClusterManager{
						Name: ClusterProfileManagerName,
					},
				},
			}},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "delete")
			},
		},
		{
			name: "delete action when cluster not found but profile exists",
			key:  "test-ns/" + testinghelpers.TestManagedClusterName,
			mc:   []runtime.Object{},
			mcs:  []runtime.Object{managedClusterSet},
			mcsb: []runtime.Object{boundBinding},
			cp: []runtime.Object{&cpv1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testinghelpers.TestManagedClusterName,
					Namespace: "test-ns",
				},
				Spec: cpv1alpha1.ClusterProfileSpec{
					ClusterManager: cpv1alpha1.ClusterManager{
						Name: ClusterProfileManagerName,
					},
				},
			}},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertActions(t, actions, "delete")
			},
		},
		{
			name: "skip profile not managed by ocm",
			key:  "test-ns/" + testinghelpers.TestManagedClusterName,
			mc:   []runtime.Object{managedCluster},
			mcs:  []runtime.Object{managedClusterSet},
			mcsb: []runtime.Object{boundBinding},
			cp: []runtime.Object{&cpv1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testinghelpers.TestManagedClusterName,
					Namespace: "test-ns",
					Labels: map[string]string{
						cpv1alpha1.LabelClusterManagerKey: "other-manager",
					},
				},
				Spec: cpv1alpha1.ClusterProfileSpec{
					ClusterManager: cpv1alpha1.ClusterManager{
						Name: "other-manager",
					},
				},
			}},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testingcommon.AssertNoActions(t, actions)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(append(c.mc, append(c.mcs, c.mcsb...)...)...)
			clusterProfileClient := cpfake.NewSimpleClientset(c.cp...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterProfileInformerFactory := cpinformers.NewSharedInformerFactory(clusterProfileClient, time.Minute*10)

			// Add objects to stores
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.mc {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			clusterSetStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore()
			for _, clusterSet := range c.mcs {
				if err := clusterSetStore.Add(clusterSet); err != nil {
					t.Fatal(err)
				}
			}

			bindingStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Informer().GetStore()
			for _, binding := range c.mcsb {
				if err := bindingStore.Add(binding); err != nil {
					t.Fatal(err)
				}
			}

			clusterProfileStore := clusterProfileInformerFactory.Apis().V1alpha1().ClusterProfiles().Informer().GetStore()
			for _, clusterprofile := range c.cp {
				if err := clusterProfileStore.Add(clusterprofile); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := clusterProfileController{
				clusterLister:            clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterSetLister:         clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
				clusterSetBindingLister:  clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Lister(),
				clusterProfileClient:     clusterProfileClient,
				clusterProfileLister:     clusterProfileInformerFactory.Apis().V1alpha1().ClusterProfiles().Lister(),
				clusterSetBindingIndexer: clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Informer().GetIndexer(),
				clusterProfileIndexer:    clusterProfileInformerFactory.Apis().V1alpha1().ClusterProfiles().Informer().GetIndexer(),
			}

			syncErr := ctrl.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, c.key), c.key)
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterProfileClient.Actions())
		})
	}
}

func TestClusterToQueueKeys(t *testing.T) {
	cluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster1",
			Labels: map[string]string{v1beta2.ClusterSetLabel: "set1"},
		},
	}

	clusterSet := &v1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "set1",
		},
		Spec: v1beta2.ManagedClusterSetSpec{
			ClusterSelector: v1beta2.ManagedClusterSelector{
				SelectorType: v1beta2.ExclusiveClusterSetLabel,
			},
		},
	}

	binding1 := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set1",
			Namespace: "ns1",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set1",
		},
	}

	binding2 := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set1",
			Namespace: "ns2",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set1",
		},
	}

	clusterClient := clusterfake.NewSimpleClientset(cluster, clusterSet, binding1, binding2)
	clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)

	clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
	clusterStore.Add(cluster)

	clusterSetStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore()
	clusterSetStore.Add(clusterSet)

	bindingStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Informer().GetStore()
	bindingIndexer := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Informer().GetIndexer()
	bindingIndexer.AddIndexers(cache.Indexers{
		bindingsByClusterSet: indexBindingByClusterSet,
	})
	bindingStore.Add(binding1)
	bindingStore.Add(binding2)

	// Create a fake cluster profile informer for the indexer
	clusterProfileClient := cpfake.NewSimpleClientset()
	clusterProfileInformerFactory := cpinformers.NewSharedInformerFactory(clusterProfileClient, time.Minute*10)
	profileIndexer := clusterProfileInformerFactory.Apis().V1alpha1().ClusterProfiles().Informer().GetIndexer()
	profileIndexer.AddIndexers(cache.Indexers{
		profilesByClusterName: indexProfileByClusterName,
	})

	ctrl := clusterProfileController{
		clusterLister:            clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
		clusterSetLister:         clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
		clusterSetBindingLister:  clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Lister(),
		clusterSetBindingIndexer: bindingIndexer,
		clusterProfileIndexer:    profileIndexer,
	}

	keys := ctrl.clusterToQueueKeys(cluster)
	expectedKeys := []string{"ns1/cluster1", "ns2/cluster1"}

	if len(keys) != len(expectedKeys) {
		t.Errorf("expected %d keys but got %d: %v", len(expectedKeys), len(keys), keys)
	}

	for _, expectedKey := range expectedKeys {
		found := false
		for _, key := range keys {
			if key == expectedKey {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected key %s not found in %v", expectedKey, keys)
		}
	}
}

func TestClusterSetToQueueKeys(t *testing.T) {
	cluster1 := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster1",
			Labels: map[string]string{v1beta2.ClusterSetLabel: "set1"},
		},
	}

	cluster2 := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster2",
			Labels: map[string]string{v1beta2.ClusterSetLabel: "set1"},
		},
	}

	clusterSet := &v1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "set1",
		},
		Spec: v1beta2.ManagedClusterSetSpec{
			ClusterSelector: v1beta2.ManagedClusterSelector{
				SelectorType: v1beta2.ExclusiveClusterSetLabel,
			},
		},
	}

	binding1 := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set1",
			Namespace: "ns1",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set1",
		},
	}

	binding2 := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set1",
			Namespace: "ns2",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set1",
		},
	}

	binding3 := &v1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "set1",
			Namespace: "ns3",
		},
		Spec: v1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: "set1",
		},
	}

	cases := []struct {
		name         string
		clusterSet   *v1beta2.ManagedClusterSet
		clusters     []*v1.ManagedCluster
		bindings     []*v1beta2.ManagedClusterSetBinding
		expectedKeys []string
	}{
		{
			name:       "clusterset with multiple clusters and multiple bindings",
			clusterSet: clusterSet,
			clusters:   []*v1.ManagedCluster{cluster1, cluster2},
			bindings:   []*v1beta2.ManagedClusterSetBinding{binding1, binding2, binding3},
			expectedKeys: []string{
				"ns1/cluster1", "ns1/cluster2",
				"ns2/cluster1", "ns2/cluster2",
				"ns3/cluster1", "ns3/cluster2",
			},
		},
		{
			name:         "clusterset with no clusters",
			clusterSet:   clusterSet,
			clusters:     []*v1.ManagedCluster{},
			bindings:     []*v1beta2.ManagedClusterSetBinding{binding1, binding2},
			expectedKeys: []string{},
		},
		{
			name:         "clusterset with no bindings",
			clusterSet:   clusterSet,
			clusters:     []*v1.ManagedCluster{cluster1, cluster2},
			bindings:     []*v1beta2.ManagedClusterSetBinding{},
			expectedKeys: []string{},
		},
		{
			name:         "clusterset with one cluster and one binding",
			clusterSet:   clusterSet,
			clusters:     []*v1.ManagedCluster{cluster1},
			bindings:     []*v1beta2.ManagedClusterSetBinding{binding1},
			expectedKeys: []string{"ns1/cluster1"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{c.clusterSet}
			for _, cluster := range c.clusters {
				objects = append(objects, cluster)
			}
			for _, binding := range c.bindings {
				objects = append(objects, binding)
			}

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)

			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.clusters {
				clusterStore.Add(cluster)
			}

			clusterSetStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore()
			clusterSetStore.Add(c.clusterSet)

			bindingStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Informer().GetStore()
			bindingIndexer := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Informer().GetIndexer()
			bindingIndexer.AddIndexers(cache.Indexers{
				bindingsByClusterSet: indexBindingByClusterSet,
			})
			for _, binding := range c.bindings {
				bindingStore.Add(binding)
			}

			ctrl := clusterProfileController{
				clusterLister:            clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterSetLister:         clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
				clusterSetBindingLister:  clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Lister(),
				clusterSetBindingIndexer: bindingIndexer,
			}

			keys := ctrl.clusterSetToQueueKeys(c.clusterSet)

			if len(keys) != len(c.expectedKeys) {
				t.Errorf("expected %d keys but got %d: %v", len(c.expectedKeys), len(keys), keys)
			}

			for _, expectedKey := range c.expectedKeys {
				found := false
				for _, key := range keys {
					if key == expectedKey {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected key %s not found in %v", expectedKey, keys)
				}
			}
		})
	}
}

func TestBindingToQueueKeys(t *testing.T) {
	cluster1 := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster1",
			Labels: map[string]string{v1beta2.ClusterSetLabel: "set1"},
		},
	}

	cluster2 := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster2",
			Labels: map[string]string{v1beta2.ClusterSetLabel: "set1"},
		},
	}

	cluster3 := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster3",
			Labels: map[string]string{v1beta2.ClusterSetLabel: "set1"},
		},
	}

	clusterSet := &v1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "set1",
		},
		Spec: v1beta2.ManagedClusterSetSpec{
			ClusterSelector: v1beta2.ManagedClusterSelector{
				SelectorType: v1beta2.ExclusiveClusterSetLabel,
			},
		},
	}

	emptyClusterSet := &v1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "emptyset",
		},
		Spec: v1beta2.ManagedClusterSetSpec{
			ClusterSelector: v1beta2.ManagedClusterSelector{
				SelectorType: v1beta2.ExclusiveClusterSetLabel,
			},
		},
	}

	cases := []struct {
		name         string
		binding      *v1beta2.ManagedClusterSetBinding
		clusterSet   *v1beta2.ManagedClusterSet
		clusters     []*v1.ManagedCluster
		expectedKeys []string
	}{
		{
			name: "binding with multiple clusters in clusterset",
			binding: &v1beta2.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "set1",
					Namespace: "test-ns",
				},
				Spec: v1beta2.ManagedClusterSetBindingSpec{
					ClusterSet: "set1",
				},
			},
			clusterSet:   clusterSet,
			clusters:     []*v1.ManagedCluster{cluster1, cluster2, cluster3},
			expectedKeys: []string{"test-ns/cluster1", "test-ns/cluster2", "test-ns/cluster3"},
		},
		{
			name: "binding with single cluster in clusterset",
			binding: &v1beta2.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "set1",
					Namespace: "another-ns",
				},
				Spec: v1beta2.ManagedClusterSetBindingSpec{
					ClusterSet: "set1",
				},
			},
			clusterSet:   clusterSet,
			clusters:     []*v1.ManagedCluster{cluster1},
			expectedKeys: []string{"another-ns/cluster1"},
		},
		{
			name: "binding with empty clusterset",
			binding: &v1beta2.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "emptyset",
					Namespace: "test-ns",
				},
				Spec: v1beta2.ManagedClusterSetBindingSpec{
					ClusterSet: "emptyset",
				},
			},
			clusterSet:   emptyClusterSet,
			clusters:     []*v1.ManagedCluster{},
			expectedKeys: []string{},
		},
		{
			name: "binding with non-existent clusterset",
			binding: &v1beta2.ManagedClusterSetBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nonexistent",
					Namespace: "test-ns",
				},
				Spec: v1beta2.ManagedClusterSetBindingSpec{
					ClusterSet: "nonexistent",
				},
			},
			clusterSet:   nil,
			clusters:     []*v1.ManagedCluster{},
			expectedKeys: []string{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			if c.clusterSet != nil {
				objects = append(objects, c.clusterSet)
			}
			for _, cluster := range c.clusters {
				objects = append(objects, cluster)
			}
			objects = append(objects, c.binding)

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)

			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.clusters {
				clusterStore.Add(cluster)
			}

			clusterSetStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore()
			if c.clusterSet != nil {
				clusterSetStore.Add(c.clusterSet)
			}

			bindingStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Informer().GetStore()
			bindingStore.Add(c.binding)

			ctrl := clusterProfileController{
				clusterLister:           clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterSetLister:        clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
				clusterSetBindingLister: clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Lister(),
			}

			keys := ctrl.bindingToQueueKeys(c.binding)

			if len(keys) != len(c.expectedKeys) {
				t.Errorf("expected %d keys but got %d: %v", len(c.expectedKeys), len(keys), keys)
			}

			for _, expectedKey := range c.expectedKeys {
				found := false
				for _, key := range keys {
					if key == expectedKey {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected key %s not found in %v", expectedKey, keys)
				}
			}
		})
	}

	// Test deduplication scenario: cluster in multiple sets with multiple bindings in same namespace
	// This test verifies that when multiple bindings in the same namespace reference different ClusterSets
	// that contain the same cluster, both bindings will generate the same queue key (namespace/cluster).
	// The queue itself will deduplicate these keys, ensuring only one profile is created.
	t.Run("deduplication: cluster in multiple sets with bindings in same namespace", func(t *testing.T) {
		// Cluster that belongs to both set1 and set2
		sharedCluster := &v1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "shared-cluster",
				Labels: map[string]string{
					v1beta2.ClusterSetLabel: "set1",
					"clusterset-set2":       "true", // Also selected by set2's label selector
				},
			},
		}

		// Two different ClusterSets that both select the same cluster
		set1 := &v1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "set1",
			},
			Spec: v1beta2.ManagedClusterSetSpec{
				ClusterSelector: v1beta2.ManagedClusterSelector{
					SelectorType: v1beta2.ExclusiveClusterSetLabel,
				},
			},
		}

		set2 := &v1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "set2",
			},
			Spec: v1beta2.ManagedClusterSetSpec{
				ClusterSelector: v1beta2.ManagedClusterSelector{
					SelectorType: v1beta2.LabelSelector,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"clusterset-set2": "true",
						},
					},
				},
			},
		}

		// Two bindings in the same namespace, referencing different sets
		binding1 := &v1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "set1-binding",
				Namespace: "shared-ns",
			},
			Spec: v1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: "set1",
			},
		}

		binding2 := &v1beta2.ManagedClusterSetBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "set2-binding",
				Namespace: "shared-ns",
			},
			Spec: v1beta2.ManagedClusterSetBindingSpec{
				ClusterSet: "set2",
			},
		}

		clusterClient := clusterfake.NewSimpleClientset(sharedCluster, set1, set2, binding1, binding2)
		clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)

		clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
		clusterStore.Add(sharedCluster)

		clusterSetStore := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore()
		clusterSetStore.Add(set1)
		clusterSetStore.Add(set2)

		ctrl := clusterProfileController{
			clusterLister:           clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
			clusterSetLister:        clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
			clusterSetBindingLister: clusterInformerFactory.Cluster().V1beta2().ManagedClusterSetBindings().Lister(),
		}

		// Process binding1 - should generate "shared-ns/shared-cluster"
		keys1 := ctrl.bindingToQueueKeys(binding1)

		// Process binding2 - should also generate "shared-ns/shared-cluster"
		keys2 := ctrl.bindingToQueueKeys(binding2)

		// Both should generate the same key
		expectedKey := "shared-ns/shared-cluster"

		if len(keys1) != 1 || keys1[0] != expectedKey {
			t.Errorf("binding1: expected key %s but got %v", expectedKey, keys1)
		}

		if len(keys2) != 1 || keys2[0] != expectedKey {
			t.Errorf("binding2: expected key %s but got %v", expectedKey, keys2)
		}

		// Verify both bindings generate the same key (demonstrating that the queue will deduplicate)
		if keys1[0] != keys2[0] {
			t.Errorf("expected both bindings to generate the same key, but got %s and %s", keys1[0], keys2[0])
		}

		// In the real controller, the queue will deduplicate these identical keys,
		// ensuring only one ClusterProfile is created for shared-cluster in shared-ns
	})
}

func TestSyncLabelsFromCluster(t *testing.T) {
	cluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				v1beta2.ClusterSetLabel: "set1",
				"custom-label":          "custom-value",
			},
		},
	}

	profile := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "test-ns",
			Labels:    map[string]string{},
		},
	}

	syncLabelsFromCluster(profile, cluster)

	if profile.Labels[cpv1alpha1.LabelClusterManagerKey] != ClusterProfileManagerName {
		t.Errorf("expected cluster manager label %s but got %s", ClusterProfileManagerName, profile.Labels[cpv1alpha1.LabelClusterManagerKey])
	}

	if profile.Labels[cpv1alpha1.LabelClusterSetKey] != "set1" {
		t.Errorf("expected cluster set label set1 but got %s", profile.Labels[cpv1alpha1.LabelClusterSetKey])
	}
}

func TestSyncStatusFromCluster(t *testing.T) {
	cluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
		Status: v1.ManagedClusterStatus{
			Version: v1.ManagedClusterVersion{
				Kubernetes: "v1.25.3",
			},
			ClusterClaims: []v1.ManagedClusterClaim{
				{Name: "claim1", Value: "value1"},
				{Name: "claim2", Value: "value2"},
			},
			Conditions: []metav1.Condition{
				{
					Type:    v1.ManagedClusterConditionAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  "Available",
					Message: "Cluster is available",
				},
				{
					Type:    v1.ManagedClusterConditionJoined,
					Status:  metav1.ConditionTrue,
					Reason:  "Joined",
					Message: "Cluster is joined",
				},
			},
		},
	}

	profile := &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster1",
			Namespace: "test-ns",
		},
	}

	syncStatusFromCluster(profile, cluster)

	if profile.Status.Version.Kubernetes != "v1.25.3" {
		t.Errorf("expected kubernetes version v1.25.3 but got %s", profile.Status.Version.Kubernetes)
	}

	if len(profile.Status.Properties) != 2 {
		t.Errorf("expected 2 properties but got %d", len(profile.Status.Properties))
	}

	if len(profile.Status.Conditions) != 2 {
		t.Errorf("expected 2 conditions but got %d", len(profile.Status.Conditions))
	}
}
