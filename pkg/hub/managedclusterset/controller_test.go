package managedclusterset

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
)

func TestSyncClusterSet(t *testing.T) {
	cases := []struct {
		name               string
		clusterSetName     string
		existingClusterSet *clusterv1beta1.ManagedClusterSet
		existingClusters   []*clusterv1.ManagedCluster
		validateActions    func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:           "sync a deleted cluster set",
			clusterSetName: "mcs1",
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:               "sync a deleting cluster set",
			clusterSetName:     "mcs1",
			existingClusterSet: newManagedClusterSet("mcs1", true),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
		{
			name:               "sync a empty cluster set",
			clusterSetName:     "mcs1",
			existingClusterSet: newManagedClusterSet("mcs1", false),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "update")
				clusterSet := actions[0].(clienttesting.UpdateAction).GetObject().(*clusterv1beta1.ManagedClusterSet)
				if !hasCondition(
					clusterSet.Status.Conditions,
					clusterv1beta1.ManagedClusterSetConditionEmpty,
					metav1.ConditionTrue,
					"NoClusterMatched",
					"No ManagedCluster selected") {
					t.Errorf("expected conditon is not found: %v", clusterSet.Status.Conditions)
				}
			},
		},
		{
			name:               "sync a cluster set",
			clusterSetName:     "mcs1",
			existingClusterSet: newManagedClusterSet("mcs1", false),
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", "mcs1"),
				newManagedCluster("cluster2", "mcs2"),
				newManagedCluster("cluster3", "mcs1"),
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "update")
				clusterSet := actions[0].(clienttesting.UpdateAction).GetObject().(*clusterv1beta1.ManagedClusterSet)
				if !hasCondition(
					clusterSet.Status.Conditions,
					clusterv1beta1.ManagedClusterSetConditionEmpty,
					metav1.ConditionFalse,
					"ClustersSelected",
					"2 ManagedClusters selected") {
					t.Errorf("expected conditon is not found: %v", clusterSet.Status.Conditions)
				}
			},
		},
		{
			name:           "sync a legacy clusterset",
			clusterSetName: "mcs1",
			existingClusterSet: &clusterv1beta1.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcs1",
				},
				Spec: clusterv1beta1.ManagedClusterSetSpec{
					ClusterSelector: clusterv1beta1.ManagedClusterSelector{
						SelectorType: clusterv1beta1.LegacyClusterSetLabel,
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertActions(t, actions, "update")
			},
		},
		{
			name:           "ignore any non-legacy clusterset",
			clusterSetName: "mcs1",
			existingClusterSet: &clusterv1beta1.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcs1",
				},
				Spec: clusterv1beta1.ManagedClusterSetSpec{
					ClusterSelector: clusterv1beta1.ManagedClusterSelector{
						SelectorType: "SingleClusterLabel",
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				testinghelpers.AssertNoActions(t, actions)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			for _, cluster := range c.existingClusters {
				objects = append(objects, cluster)
			}
			if c.existingClusterSet != nil {
				objects = append(objects, c.existingClusterSet)
			}

			clusterClient := clusterfake.NewSimpleClientset(objects...)

			informerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, 5*time.Minute)
			for _, cluster := range c.existingClusters {
				if err := informerFactory.Cluster().V1().ManagedClusters().Informer().GetStore().Add(cluster); err != nil {
					t.Fatal(err)
				}
			}
			if c.existingClusterSet != nil {
				if err := informerFactory.Cluster().V1beta1().ManagedClusterSets().Informer().GetStore().Add(c.existingClusterSet); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := managedClusterSetController{
				clusterClient:    clusterClient,
				clusterLister:    informerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterSetLister: informerFactory.Cluster().V1beta1().ManagedClusterSets().Lister(),
				eventRecorder:    eventstesting.NewTestingEventRecorder(t),
			}

			syncErr := ctrl.sync(context.Background(), testinghelpers.NewFakeSyncContext(t, c.clusterSetName))
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}

			c.validateActions(t, clusterClient.Actions())
		})
	}
}

func newManagedCluster(name, clusterSet string) *clusterv1.ManagedCluster {
	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	if len(clusterSet) > 0 {
		cluster.Labels = map[string]string{
			clusterv1beta1.ClusterSetLabel: clusterSet,
		}
	}

	return cluster
}

func newManagedClusterSet(name string, terminating bool) *clusterv1beta1.ManagedClusterSet {
	clusterSet := &clusterv1beta1.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
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
