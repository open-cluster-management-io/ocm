package managedclusterset

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
)

func TestSyncClusterSet(t *testing.T) {
	cases := []struct {
		name               string
		existingClusterSet *clusterv1beta2.ManagedClusterSet
		existingClusters   []*clusterv1.ManagedCluster
		expectCondition    metav1.Condition
		expectErr          bool
	}{
		{
			name: "sync a empty cluster set",
			existingClusterSet: &clusterv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcs1",
				},
			},
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", map[string]string{
					clusterv1beta2.ClusterSetLabel: "mcs1",
				}),
			},
			expectCondition: metav1.Condition{
				Type:    clusterv1beta2.ManagedClusterSetConditionEmpty,
				Status:  metav1.ConditionFalse,
				Reason:  "ClustersSelected",
				Message: "1 ManagedClusters selected",
			},
		},
		{
			name: "sync a legacy clusterset",
			existingClusterSet: &clusterv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcs1",
				},
				Spec: clusterv1beta2.ManagedClusterSetSpec{
					ClusterSelector: clusterv1beta2.ManagedClusterSelector{
						SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
					},
				},
			},
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", map[string]string{
					clusterv1beta2.ClusterSetLabel: "mcs1",
				}),
			},
			expectCondition: metav1.Condition{
				Type:    clusterv1beta2.ManagedClusterSetConditionEmpty,
				Status:  metav1.ConditionFalse,
				Reason:  "ClustersSelected",
				Message: "1 ManagedClusters selected",
			},
		},
		{
			name: "sync a legacy clusterset, and no cluster matched",
			existingClusterSet: &clusterv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcs1",
				},
				Spec: clusterv1beta2.ManagedClusterSetSpec{
					ClusterSelector: clusterv1beta2.ManagedClusterSelector{
						SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
					},
				},
			},
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", map[string]string{
					"vendor": "openShift",
				}),
			},
			expectCondition: metav1.Condition{
				Type:    clusterv1beta2.ManagedClusterSetConditionEmpty,
				Status:  metav1.ConditionTrue,
				Reason:  "NoClusterMatched",
				Message: "No ManagedCluster selected",
			},
		},
		{
			name: "sync a labelselector clusterset",
			existingClusterSet: &clusterv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcs1",
				},
				Spec: clusterv1beta2.ManagedClusterSetSpec{
					ClusterSelector: clusterv1beta2.ManagedClusterSelector{
						SelectorType: clusterv1beta2.LabelSelector,
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"vendor": "openShift",
							},
						},
					},
				},
			},
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", map[string]string{
					clusterv1beta2.ClusterSetLabel: "mcs1",
					"vendor":                       "openShift",
				}),
				newManagedCluster("cluster2", map[string]string{
					clusterv1beta2.ClusterSetLabel: "mcs2",
					"vendor":                       "openShift",
				}),
			},
			expectCondition: metav1.Condition{
				Type:    clusterv1beta2.ManagedClusterSetConditionEmpty,
				Status:  metav1.ConditionFalse,
				Reason:  "ClustersSelected",
				Message: "2 ManagedClusters selected",
			},
		},
		{
			name: "sync a global clusterset",
			existingClusterSet: &clusterv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcs1",
				},
				Spec: clusterv1beta2.ManagedClusterSetSpec{
					ClusterSelector: clusterv1beta2.ManagedClusterSelector{
						SelectorType:  clusterv1beta2.LabelSelector,
						LabelSelector: &metav1.LabelSelector{},
					},
				},
			},
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", map[string]string{
					clusterv1beta2.ClusterSetLabel: "mcs1",
					"vendor":                       "openShift",
				}),
				newManagedCluster("cluster2", map[string]string{
					clusterv1beta2.ClusterSetLabel: "mcs2",
					"vendor":                       "openShift",
				}),
			},
			expectCondition: metav1.Condition{
				Type:    clusterv1beta2.ManagedClusterSetConditionEmpty,
				Status:  metav1.ConditionFalse,
				Reason:  "ClustersSelected",
				Message: "2 ManagedClusters selected",
			},
		},
		{
			name: "sync a label clusterset with no labelselector specified(no cluster matched)",
			existingClusterSet: &clusterv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcs1",
				},
				Spec: clusterv1beta2.ManagedClusterSetSpec{
					ClusterSelector: clusterv1beta2.ManagedClusterSelector{
						SelectorType: clusterv1beta2.LabelSelector,
					},
				},
			},
			existingClusters: []*clusterv1.ManagedCluster{
				newManagedCluster("cluster1", map[string]string{
					clusterv1beta2.ClusterSetLabel: "mcs1",
					"vendor":                       "openShift",
				}),
				newManagedCluster("cluster2", map[string]string{
					clusterv1beta2.ClusterSetLabel: "mcs2",
					"vendor":                       "openShift",
				}),
			},
			expectCondition: metav1.Condition{
				Type:    clusterv1beta2.ManagedClusterSetConditionEmpty,
				Status:  metav1.ConditionTrue,
				Reason:  "NoClusterMatched",
				Message: "No ManagedCluster selected",
			},
		},
		{
			name: "ignore any other clusterset",
			existingClusterSet: &clusterv1beta2.ManagedClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcs1",
				},
				Spec: clusterv1beta2.ManagedClusterSetSpec{
					ClusterSelector: clusterv1beta2.ManagedClusterSelector{
						SelectorType: "SingleClusterLabel",
					},
				},
			},
			expectErr: true,
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
				err := informerFactory.Cluster().V1().ManagedClusters().Informer().GetStore().Add(cluster)
				if err != nil {
					t.Errorf("Failed to add cluster: %v, error: %v", cluster.Name, err)
				}
			}
			if c.existingClusterSet != nil {
				err := informerFactory.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore().Add(c.existingClusterSet)
				if err != nil {
					t.Errorf("Failed to add clusterset: %v, error: %v", c.existingClusterSet.Name, err)
				}
			}

			ctrl := managedClusterSetController{
				clusterClient:    clusterClient,
				clusterLister:    informerFactory.Cluster().V1().ManagedClusters().Lister(),
				clusterSetLister: informerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
				eventRecorder:    eventstesting.NewTestingEventRecorder(t),
			}

			syncErr := ctrl.syncClusterSet(context.Background(), c.existingClusterSet)
			if c.expectErr {
				if syncErr != nil {
					return
				}
				t.Errorf("Should syncClusterSet fail")
				return
			}
			if syncErr != nil {
				t.Errorf("unexpected err: %v", syncErr)
			}
			updatedSet, err := clusterClient.ClusterV1beta2().ManagedClusterSets().Get(context.Background(), c.existingClusterSet.Name, metav1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to get clusterset: %v, error: %v", c.existingClusterSet.Name, err)
			}
			if !hasCondition(updatedSet.Status.Conditions, c.expectCondition) {
				t.Errorf("expected conditon:%v. is not found: %v", c.expectCondition, updatedSet.Status.Conditions)
			}
		})
	}
}

func TestGetDiffClustersets(t *testing.T) {
	cases := []struct {
		name          string
		oldSets       []*clusterv1beta2.ManagedClusterSet
		newSets       []*clusterv1beta2.ManagedClusterSet
		expectDiffSet sets.String
	}{
		{
			name: "update a set",
			oldSets: []*clusterv1beta2.ManagedClusterSet{
				newManagedClusterSet("s1"), newManagedClusterSet("s2"),
			},
			newSets: []*clusterv1beta2.ManagedClusterSet{
				newManagedClusterSet("s1"), newManagedClusterSet("s3"),
			},
			expectDiffSet: sets.NewString("s2", "s3"),
		},
		{
			name: "add a set",
			oldSets: []*clusterv1beta2.ManagedClusterSet{
				newManagedClusterSet("s1"),
			},
			newSets: []*clusterv1beta2.ManagedClusterSet{
				newManagedClusterSet("s1"), newManagedClusterSet("s2"),
			},
			expectDiffSet: sets.NewString("s2"),
		},
		{
			name: "delete a set",
			oldSets: []*clusterv1beta2.ManagedClusterSet{
				newManagedClusterSet("s1"), newManagedClusterSet("s2"),
			},
			newSets: []*clusterv1beta2.ManagedClusterSet{
				newManagedClusterSet("s1"),
			},
			expectDiffSet: sets.NewString("s2"),
		},
		{
			name:    "old set is nil",
			oldSets: []*clusterv1beta2.ManagedClusterSet{},
			newSets: []*clusterv1beta2.ManagedClusterSet{
				newManagedClusterSet("s1"),
			},
			expectDiffSet: sets.NewString("s1"),
		},
		{
			name: "new set is nil",
			oldSets: []*clusterv1beta2.ManagedClusterSet{
				newManagedClusterSet("s1"),
			},
			newSets:       []*clusterv1beta2.ManagedClusterSet{},
			expectDiffSet: sets.NewString("s1"),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			retSets := getDiffClusterSetsNames(c.oldSets, c.newSets)
			if !reflect.DeepEqual(retSets, c.expectDiffSet) {
				t.Errorf("Case: %v, Failed to getDiffClustersets. returnSets: %v, expectSets: %v", c.name, retSets, c.expectDiffSet)
			}
		})
	}
}

func TestEnqueueUpdateClusterClusterSet(t *testing.T) {
	cases := []struct {
		name                string
		existingClusterSets []*clusterv1beta2.ManagedClusterSet
		oldCluster          *clusterv1.ManagedCluster
		newCluster          *clusterv1.ManagedCluster
		expectQueueLen      int
	}{
		{
			name: "update a cluster's clusterset",
			existingClusterSets: []*clusterv1beta2.ManagedClusterSet{
				newManagedClusterSet("mcs1"),
				newManagedClusterSet("mcs2"),
			},
			oldCluster:     newClusterWithLabel("c1", map[string]string{clusterv1beta2.ClusterSetLabel: "mcs1"}),
			newCluster:     newClusterWithLabel("c1", map[string]string{clusterv1beta2.ClusterSetLabel: "mcs2"}),
			expectQueueLen: 2,
		},
		{
			name: "add a cluster's clusterset",
			existingClusterSets: []*clusterv1beta2.ManagedClusterSet{
				newManagedClusterSet("mcs1"),
				newManagedClusterSet("mcs2"),
			},
			oldCluster:     newClusterWithLabel("c1", nil),
			newCluster:     newClusterWithLabel("c1", map[string]string{clusterv1beta2.ClusterSetLabel: "mcs2"}),
			expectQueueLen: 1,
		},
		{
			name: "remove a cluster's clusterset",
			existingClusterSets: []*clusterv1beta2.ManagedClusterSet{
				newManagedClusterSet("mcs1"),
				newManagedClusterSet("mcs2"),
			},
			newCluster:     newClusterWithLabel("c1", nil),
			oldCluster:     newClusterWithLabel("c1", map[string]string{clusterv1beta2.ClusterSetLabel: "mcs2"}),
			expectQueueLen: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}

			for _, clusterset := range c.existingClusterSets {
				objects = append(objects, clusterset)
			}
			clusterClient := clusterfake.NewSimpleClientset(objects...)

			informerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, 5*time.Minute)
			for _, clusterset := range c.existingClusterSets {
				err := informerFactory.Cluster().V1beta2().ManagedClusterSets().Informer().GetStore().Add(clusterset)
				if err != nil {
					t.Errorf("Failed to add clusterset: %v, error: %v", clusterset, err)
				}
			}
			syncCtx := testinghelpers.NewFakeSyncContext(t, "fake")

			ctrl := managedClusterSetController{
				clusterClient:    clusterClient,
				clusterSetLister: informerFactory.Cluster().V1beta2().ManagedClusterSets().Lister(),
				eventRecorder:    eventstesting.NewTestingEventRecorder(t),
				queue:            syncCtx.Queue(),
			}

			ctrl.enqueueUpdateClusterClusterSet(c.oldCluster, c.newCluster)
			if syncCtx.Queue().Len() != c.expectQueueLen {
				t.Errorf("Expect len:%v, return len:%v", c.expectQueueLen, syncCtx.Queue().Len())
			}
		})
	}
}

func newManagedCluster(name string, labels map[string]string) *clusterv1.ManagedCluster {
	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	cluster.Labels = labels
	return cluster
}

func newManagedClusterSet(name string) *clusterv1beta2.ManagedClusterSet {
	clusterSet := &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	return clusterSet
}

func hasCondition(conditions []metav1.Condition, expectCondition metav1.Condition) bool {
	for _, condition := range conditions {
		if condition.Type != expectCondition.Type {
			continue
		}
		if condition.Status != expectCondition.Status {
			continue
		}
		if condition.Reason != expectCondition.Reason {
			continue
		}
		if condition.Message != expectCondition.Message {
			continue
		}
		return true
	}
	return false
}

func newClusterWithLabel(name string, labels map[string]string) *clusterv1.ManagedCluster {
	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
	return cluster
}
