package managedcluster

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestSyncManagedNamespacesForCluster(t *testing.T) {
	cases := []struct {
		name                      string
		cluster                   *clusterv1.ManagedCluster
		clusterSets               []runtime.Object
		expectedManagedNamespaces []clusterv1.ClusterSetManagedNamespaceConfig
		expectUpdate              bool
	}{
		{
			name:                      "cluster with no cluster sets",
			cluster:                   newManagedCluster("cluster1", map[string]string{}),
			clusterSets:               []runtime.Object{},
			expectedManagedNamespaces: []clusterv1.ClusterSetManagedNamespaceConfig{},
			expectUpdate:              false, // No change needed - already empty
		},
		{
			name: "cluster with existing namespaces but no cluster sets should clear them",
			cluster: newManagedClusterWithNamespaces("cluster1", map[string]string{}, []clusterv1.ClusterSetManagedNamespaceConfig{
				{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace1"}, ClusterSet: "set1"},
			}),
			clusterSets:               []runtime.Object{},
			expectedManagedNamespaces: []clusterv1.ClusterSetManagedNamespaceConfig{},
			expectUpdate:              true, // Should clear existing namespaces
		},
		{
			name:    "cluster with single cluster set",
			cluster: newManagedCluster("cluster1", map[string]string{"cluster.open-cluster-management.io/clusterset": "set1"}),
			clusterSets: []runtime.Object{
				newManagedClusterSet("set1", []clusterv1.ManagedNamespaceConfig{
					{Name: "namespace1"},
					{Name: "namespace2"},
				}),
			},
			expectedManagedNamespaces: []clusterv1.ClusterSetManagedNamespaceConfig{
				{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace1"}, ClusterSet: "set1"},
				{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace2"}, ClusterSet: "set1"},
			},
			expectUpdate: true,
		},
		{
			name:    "cluster with multiple cluster sets",
			cluster: newManagedCluster("cluster1", map[string]string{"cluster.open-cluster-management.io/clusterset": "set1", "label1": "value1"}),
			clusterSets: []runtime.Object{
				newManagedClusterSet("set1", []clusterv1.ManagedNamespaceConfig{
					{Name: "namespace1"},
				}),
				newManagedClusterSetWithLabelSelector("set2", []clusterv1.ManagedNamespaceConfig{
					{Name: "namespace2"},
				}, map[string]string{"label1": "value1"}),
			},
			expectedManagedNamespaces: []clusterv1.ClusterSetManagedNamespaceConfig{
				{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace1"}, ClusterSet: "set1"},
				{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace2"}, ClusterSet: "set2"},
			},
			expectUpdate: true,
		},
		{
			name: "cluster with existing namespaces - no change needed",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster1",
					Labels: map[string]string{"cluster.open-cluster-management.io/clusterset": "set1"},
				},
				Status: clusterv1.ManagedClusterStatus{
					ManagedNamespaces: []clusterv1.ClusterSetManagedNamespaceConfig{
						{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace1"}, ClusterSet: "set1"},
					},
				},
			},
			clusterSets: []runtime.Object{
				newManagedClusterSet("set1", []clusterv1.ManagedNamespaceConfig{
					{Name: "namespace1"},
				}),
			},
			expectedManagedNamespaces: []clusterv1.ClusterSetManagedNamespaceConfig{
				{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace1"}, ClusterSet: "set1"},
			},
			expectUpdate: false, // No change needed
		},
		{
			name: "cluster set being deleted should be ignored",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "cluster1",
					Labels: map[string]string{"cluster.open-cluster-management.io/clusterset": "set1"},
				},
				Status: clusterv1.ManagedClusterStatus{
					ManagedNamespaces: []clusterv1.ClusterSetManagedNamespaceConfig{
						{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace1"}, ClusterSet: "set1"},
					},
				},
			},
			clusterSets: []runtime.Object{
				&clusterv1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "set1",
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
					},
					Spec: clusterv1beta2.ManagedClusterSetSpec{
						ManagedNamespaces: []clusterv1.ManagedNamespaceConfig{
							{Name: "namespace1"},
						},
					},
				},
			},
			expectedManagedNamespaces: []clusterv1.ClusterSetManagedNamespaceConfig{},
			expectUpdate:              true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterObjs := []runtime.Object{c.cluster}
			clusterClient := clusterfake.NewSimpleClientset(append(clusterObjs, c.clusterSets...)...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterInformer := clusterInformerFactory.Cluster().V1().ManagedClusters()
			clusterSetInformer := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets()

			for _, obj := range clusterObjs {
				if err := clusterInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.clusterSets {
				if err := clusterSetInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := &managedNamespaceController{
				clusterPatcher: patcher.NewPatcher[
					*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
					clusterClient.ClusterV1().ManagedClusters()),
				clusterLister:    clusterInformer.Lister(),
				clusterSetLister: clusterSetInformer.Lister(),
			}

			syncCtx := testingcommon.NewFakeSyncContext(t, c.name)

			err := controller.syncManagedNamespacesForCluster(context.TODO(), syncCtx, c.cluster)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Check that the right number of patch actions were performed
			actions := clusterClient.Actions()
			updateActions := 0
			for _, action := range actions {
				if action.GetVerb() == "patch" {
					updateActions++
				}
			}

			if c.expectUpdate && updateActions == 0 {
				t.Errorf("expected cluster status update but none occurred")
			}
			if !c.expectUpdate && updateActions > 0 {
				t.Errorf("expected no cluster status update but %d occurred", updateActions)
			}
		})
	}
}

func TestClusterSetToClusterQueueKeysFunc(t *testing.T) {
	cases := []struct {
		name             string
		clusters         []runtime.Object
		clusterSet       *clusterv1beta2.ManagedClusterSet
		expectedClusters []string
	}{
		{
			name: "current membership only",
			clusters: []runtime.Object{
				newManagedCluster("cluster1", map[string]string{"cluster.open-cluster-management.io/clusterset": "set1"}),
				newManagedCluster("cluster2", map[string]string{"cluster.open-cluster-management.io/clusterset": "set1"}),
			},
			clusterSet:       newManagedClusterSet("set1", []clusterv1.ManagedNamespaceConfig{{Name: "namespace1"}}),
			expectedClusters: []string{"cluster1", "cluster2"},
		},
		{
			name: "previous membership only",
			clusters: []runtime.Object{
				newManagedClusterWithNamespaces("cluster1", map[string]string{}, []clusterv1.ClusterSetManagedNamespaceConfig{
					{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace1"}, ClusterSet: "set1"},
				}),
				newManagedClusterWithNamespaces("cluster2", map[string]string{}, []clusterv1.ClusterSetManagedNamespaceConfig{
					{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace1"}, ClusterSet: "set1"},
				}),
			},
			clusterSet:       newManagedClusterSet("set1", []clusterv1.ManagedNamespaceConfig{{Name: "namespace1"}}),
			expectedClusters: []string{"cluster1", "cluster2"},
		},
		{
			name: "both current and previous membership",
			clusters: []runtime.Object{
				newManagedCluster("cluster1", map[string]string{"cluster.open-cluster-management.io/clusterset": "set1"}),
				newManagedClusterWithNamespaces("cluster2", map[string]string{}, []clusterv1.ClusterSetManagedNamespaceConfig{
					{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace1"}, ClusterSet: "set1"},
				}),
				newManagedCluster("cluster3", map[string]string{"cluster.open-cluster-management.io/clusterset": "set1"}),
			},
			clusterSet:       newManagedClusterSet("set1", []clusterv1.ManagedNamespaceConfig{{Name: "namespace1"}}),
			expectedClusters: []string{"cluster1", "cluster2", "cluster3"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(append(c.clusters, c.clusterSet)...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterInformer := clusterInformerFactory.Cluster().V1().ManagedClusters()
			clusterSetInformer := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets()

			for _, obj := range c.clusters {
				if err := clusterInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			if err := clusterSetInformer.Informer().GetStore().Add(c.clusterSet); err != nil {
				t.Fatal(err)
			}

			controller := &managedNamespaceController{
				clusterPatcher: patcher.NewPatcher[
					*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
					clusterClient.ClusterV1().ManagedClusters()),
				clusterLister:    clusterInformer.Lister(),
				clusterSetLister: clusterSetInformer.Lister(),
			}

			clusterNames := controller.clusterSetToClusterQueueKeysFunc(c.clusterSet)

			if len(clusterNames) != len(c.expectedClusters) {
				t.Errorf("expected %d cluster names to be returned, got %d", len(c.expectedClusters), len(clusterNames))
			}

			expectedClusters := make(map[string]bool)
			for _, name := range c.expectedClusters {
				expectedClusters[name] = false
			}

			for _, name := range clusterNames {
				if _, exists := expectedClusters[name]; exists {
					expectedClusters[name] = true
				} else {
					t.Errorf("unexpected cluster name returned: %s", name)
				}
			}

			for cluster, found := range expectedClusters {
				if !found {
					t.Errorf("expected cluster %s to be returned but it wasn't", cluster)
				}
			}
		})
	}
}

func TestGetClustersPreviouslyInSet(t *testing.T) {
	cases := []struct {
		name             string
		clusters         []runtime.Object
		clusterSetName   string
		expectedClusters []string
	}{
		{
			name: "no clusters with managed namespaces",
			clusters: []runtime.Object{
				newManagedCluster("cluster1", map[string]string{}),
				newManagedCluster("cluster2", map[string]string{}),
			},
			clusterSetName:   "set1",
			expectedClusters: []string{},
		},
		{
			name: "some clusters with managed namespaces from target set",
			clusters: []runtime.Object{
				newManagedClusterWithNamespaces("cluster1", map[string]string{}, []clusterv1.ClusterSetManagedNamespaceConfig{
					{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace1"}, ClusterSet: "set1"},
				}),
				newManagedClusterWithNamespaces("cluster2", map[string]string{}, []clusterv1.ClusterSetManagedNamespaceConfig{
					{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace2"}, ClusterSet: "set2"},
				}),
				newManagedClusterWithNamespaces("cluster3", map[string]string{}, []clusterv1.ClusterSetManagedNamespaceConfig{
					{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace3"}, ClusterSet: "set1"},
				}),
			},
			clusterSetName:   "set1",
			expectedClusters: []string{"cluster1", "cluster3"},
		},
		{
			name: "clusters with multiple managed namespaces",
			clusters: []runtime.Object{
				newManagedClusterWithNamespaces("cluster1", map[string]string{}, []clusterv1.ClusterSetManagedNamespaceConfig{
					{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace1"}, ClusterSet: "set1"},
					{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace2"}, ClusterSet: "set2"},
				}),
				newManagedClusterWithNamespaces("cluster2", map[string]string{}, []clusterv1.ClusterSetManagedNamespaceConfig{
					{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace3"}, ClusterSet: "set3"},
				}),
			},
			clusterSetName:   "set1",
			expectedClusters: []string{"cluster1"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := clusterfake.NewSimpleClientset(c.clusters...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterInformer := clusterInformerFactory.Cluster().V1().ManagedClusters()

			for _, obj := range c.clusters {
				if err := clusterInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := &managedNamespaceController{
				clusterPatcher: patcher.NewPatcher[
					*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
					clusterClient.ClusterV1().ManagedClusters()),
				clusterLister: clusterInformer.Lister(),
			}

			clusters, err := controller.getClustersPreviouslyInSet(c.clusterSetName)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if len(clusters) != len(c.expectedClusters) {
				t.Errorf("expected %d clusters to be returned, got %d", len(c.expectedClusters), len(clusters))
			}

			expectedClusters := make(map[string]bool)
			for _, name := range c.expectedClusters {
				expectedClusters[name] = false
			}

			for _, cluster := range clusters {
				if _, exists := expectedClusters[cluster.Name]; exists {
					expectedClusters[cluster.Name] = true
				} else {
					t.Errorf("unexpected cluster returned: %s", cluster.Name)
				}
			}

			for cluster, found := range expectedClusters {
				if !found {
					t.Errorf("expected cluster %s to be returned but it wasn't", cluster)
				}
			}
		})
	}
}

func TestSync(t *testing.T) {
	cases := []struct {
		name        string
		cluster     *clusterv1.ManagedCluster
		clusterSets []runtime.Object
		expectError bool
		queueKey    string
	}{
		{
			name:        "empty queue key should return nil",
			cluster:     nil,
			clusterSets: []runtime.Object{},
			expectError: false,
			queueKey:    "",
		},
		{
			name:        "cluster not found should return nil",
			cluster:     nil,
			clusterSets: []runtime.Object{},
			expectError: false,
			queueKey:    "nonexistent-cluster",
		},
		{
			name: "normal cluster should be reconciled",
			cluster: newManagedCluster("cluster1", map[string]string{
				"cluster.open-cluster-management.io/clusterset": "set1",
			}),
			clusterSets: []runtime.Object{
				newManagedClusterSet("set1", []clusterv1.ManagedNamespaceConfig{
					{Name: "namespace1"},
				}),
			},
			expectError: false,
			queueKey:    "cluster1",
		},
		{
			name: "terminating cluster should be skipped",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "cluster1",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Status: clusterv1.ManagedClusterStatus{
					ManagedNamespaces: []clusterv1.ClusterSetManagedNamespaceConfig{
						{ManagedNamespaceConfig: clusterv1.ManagedNamespaceConfig{Name: "namespace1"}, ClusterSet: "set1"},
					},
				},
			},
			clusterSets: []runtime.Object{},
			expectError: false,
			queueKey:    "cluster1",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var objects []runtime.Object
			if c.cluster != nil {
				objects = append(objects, c.cluster)
			}
			objects = append(objects, c.clusterSets...)

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterInformer := clusterInformerFactory.Cluster().V1().ManagedClusters()
			clusterSetInformer := clusterInformerFactory.Cluster().V1beta2().ManagedClusterSets()

			if c.cluster != nil {
				if err := clusterInformer.Informer().GetStore().Add(c.cluster); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range c.clusterSets {
				if err := clusterSetInformer.Informer().GetStore().Add(obj); err != nil {
					t.Fatal(err)
				}
			}

			controller := &managedNamespaceController{
				clusterPatcher: patcher.NewPatcher[
					*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
					clusterClient.ClusterV1().ManagedClusters()),
				clusterLister:    clusterInformer.Lister(),
				clusterSetLister: clusterSetInformer.Lister(),
			}

			// Create a fake sync context
			syncCtx := testingcommon.NewFakeSyncContext(t, c.queueKey)
			err := controller.sync(context.TODO(), syncCtx, c.queueKey)

			if c.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !c.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func newManagedCluster(name string, labels map[string]string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func newManagedClusterWithNamespaces(name string, labels map[string]string, managedNS []clusterv1.ClusterSetManagedNamespaceConfig) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: clusterv1.ManagedClusterStatus{
			ManagedNamespaces: managedNS,
		},
	}
}

func newManagedClusterSet(name string, namespaces []clusterv1.ManagedNamespaceConfig) *clusterv1beta2.ManagedClusterSet {
	return &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: clusterv1beta2.ManagedClusterSetSpec{
			ClusterSelector: clusterv1beta2.ManagedClusterSelector{
				SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
			},
			ManagedNamespaces: namespaces,
		},
	}
}

func newManagedClusterSetWithLabelSelector(name string, namespaces []clusterv1.ManagedNamespaceConfig, labelSelector map[string]string) *clusterv1beta2.ManagedClusterSet {
	return &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: clusterv1beta2.ManagedClusterSetSpec{
			ClusterSelector: clusterv1beta2.ManagedClusterSelector{
				SelectorType: clusterv1beta2.LabelSelector,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: labelSelector,
				},
			},
			ManagedNamespaces: namespaces,
		},
	}
}
