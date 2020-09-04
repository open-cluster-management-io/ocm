package managedclusterset

import (
	"context"
	"testing"
	"time"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterinformers "github.com/open-cluster-management/api/client/cluster/informers/externalversions"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	clusterv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
	testinghelpers "github.com/open-cluster-management/registration/pkg/helpers/testing"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/diff"
)

func TestSelectClusters(t *testing.T) {
	cases := []struct {
		name                     string
		existingClusters         []*clusterv1.ManagedCluster
		selector                 clusterv1alpha1.ClusterSelector
		expectedSelectedClusters []string
	}{
		{
			name: "select cluster with cluster names",
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", nil),
				newManagedCluster("cluster2", nil),
				newManagedCluster("cluster3", nil),
			},
			selector: clusterv1alpha1.ClusterSelector{
				ClusterNames: []string{"cluster1", "cluster2"},
			},
			expectedSelectedClusters: []string{"cluster1", "cluster2"},
		},
		{
			name: "select cluster with label selector",
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", map[string]string{"env": "dev"}),
				newManagedCluster("cluster2", map[string]string{"env": "prod"}),
				newManagedCluster("cluster3", map[string]string{"env": "prod"}),
			},
			selector: clusterv1alpha1.ClusterSelector{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"env": "prod",
					},
				},
			},
			expectedSelectedClusters: []string{"cluster2", "cluster3"},
		},
		{
			name: "select no cluster",
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", nil),
				newManagedCluster("cluster2", nil),
				newManagedCluster("cluster3", nil),
			},
			selector:                 clusterv1alpha1.ClusterSelector{},
			expectedSelectedClusters: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			for _, cluster := range c.existingClusters {
				objects = append(objects, cluster)
			}

			clusterClient := clusterfake.NewSimpleClientset(objects...)
			informerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, 5*time.Minute)
			for _, cluster := range c.existingClusters {
				informerFactory.Cluster().V1().ManagedClusters().Informer().GetStore().Add(cluster)
			}

			ctrl := managedClusterSetController{
				clusterLister: informerFactory.Cluster().V1().ManagedClusters().Lister(),
			}
			selectedClusters, err := ctrl.selectClusters(c.selector)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}

			actual := sets.NewString(selectedClusters...)
			expected := sets.NewString(c.expectedSelectedClusters...)
			if !actual.Equal(expected) {
				t.Errorf(diff.ObjectDiff(selectedClusters, c.expectedSelectedClusters))
			}
		})
	}
}

func TestSyncManagedClusterSet(t *testing.T) {
	cases := []struct {
		name                string
		queueKey            string
		existingClusters    []*clusterv1.ManagedCluster
		existingClusterSets []*clusterv1alpha1.ManagedClusterSet
		validateActions     func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:     "sync a deleted cluster set",
			queueKey: "mcs1",
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:     "sync a deleting cluster set",
			queueKey: "mcs1",
			existingClusterSets: []*clusterv1alpha1.ManagedClusterSet{
				newManagedClusterSet("mcs1", nil, true),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:     "sync a cluster set with single selector",
			queueKey: "mcs1",
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", nil),
			},
			existingClusterSets: []*clusterv1alpha1.ManagedClusterSet{
				newManagedClusterSet("mcs1", []clusterv1alpha1.ClusterSelector{
					{
						ClusterNames: []string{"cluster1"},
					},
				}, false),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "update")
				clusterSet := actions[0].(clienttesting.UpdateAction).GetObject().(*clusterv1alpha1.ManagedClusterSet)
				if !hasCondition(
					clusterSet.Status.Conditions,
					clusterv1alpha1.ManagedClusterSetConditionEmpty,
					metav1.ConditionFalse,
					"ClustersSelected",
					"1 ManagedClusters selected") {
					t.Errorf("expected conditon is not found: %v", clusterSet.Status.Conditions)
				}
			},
		},
		{
			name:     "sync a cluster set with multiple selectors",
			queueKey: "mcs1",
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", nil),
				newManagedCluster("cluster2", nil),
			},
			existingClusterSets: []*clusterv1alpha1.ManagedClusterSet{
				newManagedClusterSet("mcs1", []clusterv1alpha1.ClusterSelector{
					{
						ClusterNames: []string{"cluster1"},
					},
					{
						ClusterNames: []string{"cluster2"},
					},
				}, false),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "update")
				clusterSet := actions[0].(clienttesting.UpdateAction).GetObject().(*clusterv1alpha1.ManagedClusterSet)
				if !hasCondition(
					clusterSet.Status.Conditions,
					clusterv1alpha1.ManagedClusterSetConditionEmpty,
					metav1.ConditionFalse,
					"ClustersSelected",
					"2 ManagedClusters selected") {
					t.Errorf("expected conditon is not found: %v", clusterSet.Status.Conditions)
				}
			},
		},
		{
			name:     "sync a empty cluster set",
			queueKey: "mcs1",
			existingClusterSets: []*clusterv1alpha1.ManagedClusterSet{
				newManagedClusterSet("mcs1", []clusterv1alpha1.ClusterSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"env": "dev",
							},
						},
					},
				}, false),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "update")
				clusterSet := actions[0].(clienttesting.UpdateAction).GetObject().(*clusterv1alpha1.ManagedClusterSet)
				if !hasCondition(
					clusterSet.Status.Conditions,
					clusterv1alpha1.ManagedClusterSetConditionEmpty,
					metav1.ConditionTrue,
					"NoClusterMatched",
					"No ManagedCluster selected") {
					t.Errorf("expected conditon is not found: %v", clusterSet.Status.Conditions)
				}
			},
		},
		{
			name:     "sync all cluster sets",
			queueKey: "key",
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", nil),
				newManagedCluster("cluster2", nil),
				newManagedCluster("cluster3", nil),
			},
			existingClusterSets: []*clusterv1alpha1.ManagedClusterSet{
				newManagedClusterSet("mcs1", []clusterv1alpha1.ClusterSelector{
					{
						ClusterNames: []string{"cluster1", "cluster2"},
					},
				}, false),
				newManagedClusterSet("mcs2", []clusterv1alpha1.ClusterSelector{
					{
						ClusterNames: []string{"cluster3"},
					},
				}, false),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("expected %d actions but got: %d, %v", 2, len(actions), actions)
				}

				clusterSet := actions[0].(clienttesting.UpdateAction).GetObject().(*clusterv1alpha1.ManagedClusterSet)
				if !hasCondition(
					clusterSet.Status.Conditions,
					clusterv1alpha1.ManagedClusterSetConditionEmpty,
					metav1.ConditionFalse,
					"ClustersSelected",
					"2 ManagedClusters selected") {
					t.Errorf("expected conditon is not found: %v", clusterSet.Status.Conditions)
				}

				clusterSet = actions[1].(clienttesting.UpdateAction).GetObject().(*clusterv1alpha1.ManagedClusterSet)
				if !hasCondition(
					clusterSet.Status.Conditions,
					clusterv1alpha1.ManagedClusterSetConditionEmpty,
					metav1.ConditionFalse,
					"ClustersSelected",
					"1 ManagedClusters selected") {
					t.Errorf("expected conditon is not found: %v", clusterSet.Status.Conditions)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			for _, cluster := range c.existingClusters {
				objects = append(objects, cluster)
			}
			for _, clusterSet := range c.existingClusterSets {
				objects = append(objects, clusterSet)
			}

			clusterClient := clusterfake.NewSimpleClientset(objects...)

			informerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, 5*time.Minute)
			for _, cluster := range c.existingClusters {
				informerFactory.Cluster().V1().ManagedClusters().Informer().GetStore().Add(cluster)
			}
			for _, clusterSet := range c.existingClusterSets {
				informerFactory.Cluster().V1alpha1().ManagedClusterSets().Informer().GetStore().Add(clusterSet)
			}

			ctrl := managedClusterSetController{
				clusterClient,
				informerFactory.Cluster().V1().ManagedClusters().Lister(),
				informerFactory.Cluster().V1alpha1().ManagedClusterSets().Lister(),
				eventstesting.NewTestingEventRecorder(t)}

			syncErr := ctrl.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, c.queueKey))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
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

func newManagedClusterSet(name string, clusterSelectors []clusterv1alpha1.ClusterSelector, terminating bool) *clusterv1alpha1.ManagedClusterSet {
	clusterSet := &clusterv1alpha1.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: clusterv1alpha1.ManagedClusterSetSpec{
			ClusterSelectors: clusterSelectors,
		},
	}
	if terminating {
		now := metav1.Now()
		clusterSet.DeletionTimestamp = &now
	}

	return clusterSet
}

func hasCondition(conditions []metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string) bool {
	for _, condition := range conditions {
		if condition.Type != conditionType {
			continue
		}
		if condition.Status != status {
			continue
		}
		if condition.Reason != reason {
			continue
		}
		if condition.Message != message {
			continue
		}
		return true
	}
	return false
}
