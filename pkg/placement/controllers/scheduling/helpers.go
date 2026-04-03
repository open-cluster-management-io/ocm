package scheduling

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	clustersdkv1beta2 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1beta2"
)

// GetValidManagedClusterSetBindings returns all valid bindings in the placement namespace.
func GetValidManagedClusterSetBindings(
	placementNamespace string,
	clusterSetBindingLister clusterlisterv1beta2.ManagedClusterSetBindingLister,
	clusterSetLister clusterlisterv1beta2.ManagedClusterSetLister,
) ([]*clusterapiv1beta2.ManagedClusterSetBinding, error) {
	// get all clusterset bindings under the placement namespace
	bindings, err := clusterSetBindingLister.ManagedClusterSetBindings(placementNamespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return nil, nil
	}

	var validBindings []*clusterapiv1beta2.ManagedClusterSetBinding
	for _, binding := range bindings {
		// ignore clustersetbinding refers to a non-existent clusterset
		_, err := clusterSetLister.Get(binding.Name)
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		validBindings = append(validBindings, binding)
	}

	return validBindings, nil
}

// GetEligibleClusterSets returns the names of clusterset that are eligible for the placement.
func GetEligibleClusterSets(
	placement *clusterapiv1beta1.Placement,
	bindings []*clusterapiv1beta2.ManagedClusterSetBinding,
) []string {
	// collect names from valid bindings
	clusterSetNames := sets.New[string]()
	for _, binding := range bindings {
		clusterSetNames.Insert(binding.Name)
	}

	// get intersection of clustersets bound to placement namespace and clustersets specified
	// in placement spec
	if len(placement.Spec.ClusterSets) != 0 {
		clusterSetNames = clusterSetNames.Intersection(sets.New[string](placement.Spec.ClusterSets...))
	}

	return sets.List(clusterSetNames)
}

// GetAvailableClusters returns available clusters for the given placement. The clusters must
// 1) Be from clustersets bound to the placement namespace;
// 2) Belong to one of particular clustersets if .spec.clusterSets is specified;
// 3) Not in terminating state;
func GetAvailableClusters(
	clusterSetNames []string,
	clusterSetLister clusterlisterv1beta2.ManagedClusterSetLister,
	clusterLister clusterlisterv1.ManagedClusterLister,
) ([]*clusterapiv1.ManagedCluster, error) {
	if len(clusterSetNames) == 0 {
		return nil, nil
	}
	// all available clusters
	availableClusters := map[string]*clusterapiv1.ManagedCluster{}

	for _, name := range clusterSetNames {
		// ignore clusterset if failed to get
		clusterSet, err := clusterSetLister.Get(name)
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		clusters, err := clustersdkv1beta2.GetClustersFromClusterSet(clusterSet, clusterLister)
		if err != nil {
			return nil, fmt.Errorf("failed to get clusters from ClusterSet %q: %w", clusterSet.Name, err)
		}
		for i := range clusters {
			if clusters[i].DeletionTimestamp.IsZero() {
				availableClusters[clusters[i].Name] = clusters[i]
			}
		}
	}

	if len(availableClusters) == 0 {
		return nil, nil
	}

	var result []*clusterapiv1.ManagedCluster
	for _, c := range availableClusters {
		result = append(result, c)
	}

	return result, nil
}
