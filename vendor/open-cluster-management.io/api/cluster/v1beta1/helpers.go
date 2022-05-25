package v1beta1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	v1 "open-cluster-management.io/api/cluster/v1"
)

type ManagedClusterGetter interface {
	List(selector labels.Selector) (ret []*v1.ManagedCluster, err error)
}

type ManagedClusterSetGetter interface {
	List(selector labels.Selector) (ret []*ManagedClusterSet, err error)
}

// GetClustersFromClusterSet return the ManagedClusterSet's managedClusters
func GetClustersFromClusterSet(clusterSet *ManagedClusterSet, clusterGetter ManagedClusterGetter) ([]*v1.ManagedCluster, error) {
	var clusters []*v1.ManagedCluster

	if clusterSet == nil {
		return nil, nil
	}

	clusterSelector, err := BuildClusterSelector(clusterSet)
	if err != nil {
		return nil, err
	}
	if clusterSelector == nil {
		return nil, fmt.Errorf("failed to build ClusterSelector with clusterSet: %v", clusterSet)
	}
	clusters, err = clusterGetter.List(clusterSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to list ManagedClusters: %w", err)
	}
	return clusters, nil
}

// GetClusterSetsOfClusterByCluster return the managedClusterSets of a managedCluster
func GetClusterSetsOfCluster(cluster *v1.ManagedCluster, clusterSetGetter ManagedClusterSetGetter) ([]*ManagedClusterSet, error) {
	var returnClusterSets []*ManagedClusterSet

	if cluster == nil {
		return nil, nil
	}

	allClusterSets, err := clusterSetGetter.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, clusterSet := range allClusterSets {
		clusterSelector, err := BuildClusterSelector(clusterSet)
		if err != nil {
			return nil, err
		}
		if clusterSelector == nil {
			return nil, fmt.Errorf("failed to build ClusterSelector with clusterSet: %v", clusterSet)
		}
		if clusterSelector.Matches(labels.Set(cluster.Labels)) {
			returnClusterSets = append(returnClusterSets, clusterSet)
		}
	}
	return returnClusterSets, nil
}

func BuildClusterSelector(clusterSet *ManagedClusterSet) (labels.Selector, error) {
	if clusterSet == nil {
		return nil, nil
	}
	selectorType := clusterSet.Spec.ClusterSelector.SelectorType

	switch selectorType {
	case "", LegacyClusterSetLabel:
		return labels.SelectorFromSet(labels.Set{
			ClusterSetLabel: clusterSet.Name,
		}), nil
	case LabelSelector:
		return metav1.LabelSelectorAsSelector(clusterSet.Spec.ClusterSelector.LabelSelector)
	default:
		return nil, fmt.Errorf("selectorType is not right: %s", clusterSet.Spec.ClusterSelector.SelectorType)
	}
}
