package helpers

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"

	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

func newFakePlacementDecision(placementName, groupName string, groupIndex int, clusterNames ...string) *clusterv1beta1.PlacementDecision {
	decisions := make([]clusterv1beta1.ClusterDecision, len(clusterNames))
	for i, clusterName := range clusterNames {
		decisions[i] = clusterv1beta1.ClusterDecision{ClusterName: clusterName}
	}

	return &clusterv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Labels: map[string]string{
				clusterv1beta1.PlacementLabel:          placementName,
				clusterv1beta1.DecisionGroupNameLabel:  groupName,
				clusterv1beta1.DecisionGroupIndexLabel: strconv.Itoa(groupIndex),
			},
		},
		Status: clusterv1beta1.PlacementDecisionStatus{
			Decisions: decisions,
		},
	}
}

func TestPlacementDecisionClustersTracker_GetClusterChanges(t *testing.T) {
	tests := []struct {
		name                           string
		placement                      *clusterv1beta1.Placement
		existingScheduledClusters      sets.Set[string]
		updateDecisions                []runtime.Object
		expectAddedScheduledClusters   sets.Set[string]
		expectDeletedScheduledClusters sets.Set[string]
	}{
		{
			name: "test placementdecisions",
			placement: &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "default"},
				Spec:       clusterv1beta1.PlacementSpec{},
			},
			existingScheduledClusters: sets.New[string]("cluster1", "cluster2"),
			updateDecisions: []runtime.Object{
				newFakePlacementDecision("placement1", "", 0, "cluster1", "cluster3"),
			},
			expectAddedScheduledClusters:   sets.New[string]("cluster3"),
			expectDeletedScheduledClusters: sets.New[string]("cluster2"),
		},
		{
			name: "test empty placementdecision",
			placement: &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "default"},
				Spec:       clusterv1beta1.PlacementSpec{},
			},
			existingScheduledClusters: sets.New[string](),
			updateDecisions: []runtime.Object{
				newFakePlacementDecision("placement1", "", 0, "cluster1", "cluster2"),
			},
			expectAddedScheduledClusters:   sets.New[string]("cluster1", "cluster2"),
			expectDeletedScheduledClusters: sets.New[string](),
		},
		{
			name: "test nil exist cluster groups",
			placement: &clusterv1beta1.Placement{
				ObjectMeta: metav1.ObjectMeta{Name: "placement1", Namespace: "default"},
				Spec:       clusterv1beta1.PlacementSpec{},
			},
			existingScheduledClusters: nil,
			updateDecisions: []runtime.Object{
				newFakePlacementDecision("placement1", "", 0, "cluster1", "cluster2"),
			},
			expectAddedScheduledClusters:   sets.New[string]("cluster1", "cluster2"),
			expectDeletedScheduledClusters: sets.New[string](),
		},
	}

	for _, test := range tests {
		fakeClusterClient := fakecluster.NewSimpleClientset()
		clusterInformers := clusterv1informers.NewSharedInformerFactory(fakeClusterClient, 10*time.Minute)

		for _, obj := range test.updateDecisions {
			if err := clusterInformers.Cluster().V1beta1().PlacementDecisions().Informer().GetStore().Add(obj); err != nil {
				t.Fatal(err)
			}
		}

		// check changed decision clusters
		client := clusterInformers.Cluster().V1beta1().PlacementDecisions().Lister()
		addedClusters, deletedClusters, err := GetClusterChanges(client, test.placement, test.existingScheduledClusters)
		if err != nil {
			t.Errorf("Case: %v, Failed to run Get(): %v", test.name, err)
		}
		if !reflect.DeepEqual(addedClusters, test.expectAddedScheduledClusters) {
			t.Errorf("Case: %v, expect added decisions: %v, return decisions: %v", test.name, test.expectAddedScheduledClusters, addedClusters)
			return
		}
		if !reflect.DeepEqual(deletedClusters, test.expectDeletedScheduledClusters) {
			t.Errorf("Case: %v, expect deleted decisions: %v, return decisions: %v", test.name, test.expectDeletedScheduledClusters, deletedClusters)
			return
		}
	}
}

func TestHasFinalizer(t *testing.T) {
	tests := []struct {
		name       string
		finalizers []string
		finalizer  string
		expected   bool
	}{
		{
			name:       "has finalizer",
			finalizers: []string{"abc", "clustername", "test"},
			finalizer:  "clustername",
			expected:   true,
		},
		{
			name:       "has no finalizer",
			finalizers: []string{"abc", "clustername", "test"},
			finalizer:  "name",
			expected:   false,
		},
		{
			name:       "has empty finalizers",
			finalizers: []string{},
			finalizer:  "clustername",
			expected:   false,
		},
	}

	for _, test := range tests {
		rst := HasFinalizer(test.finalizers, test.finalizer)
		if test.expected != rst {
			t.Errorf("expected %v, got %v", test.expected, rst)
		}
	}
}
