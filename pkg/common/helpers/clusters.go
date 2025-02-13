package helpers

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clustersdkv1beta1 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1beta1"
)

const (
	// GcFinalizer is added to the managedCluster for resource cleanup, which maintained by gc controller.
	GcFinalizer = "cluster.open-cluster-management.io/resource-cleanup"
)

type PlacementDecisionGetter struct {
	Client clusterlister.PlacementDecisionLister
}

func (pdl PlacementDecisionGetter) List(selector labels.Selector, namespace string) ([]*clusterv1beta1.PlacementDecision, error) {
	return pdl.Client.PlacementDecisions(namespace).List(selector)
}

// Get added and deleted clusters names
func GetClusterChanges(client clusterlister.PlacementDecisionLister, placement *clusterv1beta1.Placement,
	existingClusters sets.Set[string]) (sets.Set[string], sets.Set[string], error) {
	pdtracker := clustersdkv1beta1.NewPlacementDecisionClustersTracker(
		placement, PlacementDecisionGetter{Client: client}, existingClusters)

	return pdtracker.GetClusterChanges()
}

func HasFinalizer(finalizers []string, finalizer string) bool {
	if len(finalizers) == 0 || len(finalizer) == 0 {
		return false
	}

	for i := range finalizers {
		if finalizers[i] == finalizer {
			return true
		}
	}
	return false
}
