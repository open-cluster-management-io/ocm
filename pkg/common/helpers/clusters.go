package helpers

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
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
	pdtracker := clusterv1beta1.NewPlacementDecisionClustersTracker(
		placement, PlacementDecisionGetter{Client: client}, existingClusters)

	return pdtracker.GetClusterChanges()
}
