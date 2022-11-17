package v1beta2

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	v1 "open-cluster-management.io/api/cluster/v1"
)

type ManagedClustersGetter interface {
	List(selector labels.Selector) (ret []*v1.ManagedCluster, err error)
}

type ManagedClusterSetsGetter interface {
	List(selector labels.Selector) (ret []*ManagedClusterSet, err error)
}

type ManagedClusterSetBindingsGetter interface {
	List(namespace string, selector labels.Selector) (ret []*ManagedClusterSetBinding, err error)
}

// GetClustersFromClusterSet return the ManagedClusterSet's managedClusters
func GetClustersFromClusterSet(clusterSet *ManagedClusterSet,
	clustersGetter ManagedClustersGetter) ([]*v1.ManagedCluster, error) {
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
	clusters, err = clustersGetter.List(clusterSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to list ManagedClusters: %w", err)
	}
	return clusters, nil
}

// GetClusterSetsOfClusterByCluster return the managedClusterSets of a managedCluster
func GetClusterSetsOfCluster(cluster *v1.ManagedCluster,
	clusterSetsGetter ManagedClusterSetsGetter) ([]*ManagedClusterSet, error) {
	var returnClusterSets []*ManagedClusterSet

	if cluster == nil {
		return nil, nil
	}

	allClusterSets, err := clusterSetsGetter.List(labels.Everything())
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
	case "", ExclusiveClusterSetLabel:
		return labels.SelectorFromSet(labels.Set{
			ClusterSetLabel: clusterSet.Name,
		}), nil
	case LabelSelector:
		return metav1.LabelSelectorAsSelector(clusterSet.Spec.ClusterSelector.LabelSelector)
	default:
		return nil, fmt.Errorf("selectorType is not right: %s", clusterSet.Spec.ClusterSelector.SelectorType)
	}
}

// GetBoundManagedClusterSetBindings returns all bindings that are bounded to clustersets in the given namespace.
func GetBoundManagedClusterSetBindings(namespace string,
	clusterSetBindingsGetter ManagedClusterSetBindingsGetter) ([]*ManagedClusterSetBinding, error) {
	// get all clusterset bindings under the namespace
	bindings, err := clusterSetBindingsGetter.List(namespace, labels.Everything())
	if err != nil {
		return nil, err
	}

	boundBindings := []*ManagedClusterSetBinding{}
	for _, binding := range bindings {
		if meta.IsStatusConditionTrue(binding.Status.Conditions, ClusterSetBindingBoundType) {
			boundBindings = append(boundBindings, binding)
		}
	}

	return boundBindings, nil
}
