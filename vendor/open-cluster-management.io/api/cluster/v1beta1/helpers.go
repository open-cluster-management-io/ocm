package v1beta1

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
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

type PlacementDecisionGetter interface {
	List(selector labels.Selector, namespace string) (ret []*PlacementDecision, err error)
}

// +k8s:deepcopy-gen=false
type PlacementDecisionClustersTracker struct {
	placement                 *Placement
	placementDecisionGetter   PlacementDecisionGetter
	existingScheduledClusters sets.String
	lock                      sync.RWMutex
}

func NewPlacementDecisionClustersTracker(placement *Placement, pdl PlacementDecisionGetter, existingScheduledClusters sets.String) *PlacementDecisionClustersTracker {
	pdct := &PlacementDecisionClustersTracker{
		placement:                 placement,
		placementDecisionGetter:   pdl,
		existingScheduledClusters: existingScheduledClusters,
	}
	return pdct
}

// Get() update the tracker's decisionClusters and return the added and deleted cluster names.
func (pdct *PlacementDecisionClustersTracker) Get() (sets.String, sets.String, error) {
	pdct.lock.Lock()
	defer pdct.lock.Unlock()

	if pdct.placement == nil || pdct.placementDecisionGetter == nil {
		return nil, nil, nil
	}

	// Get the generated PlacementDecisions
	decisionSelector := labels.SelectorFromSet(labels.Set{
		PlacementLabel: pdct.placement.Name,
	})
	decisions, err := pdct.placementDecisionGetter.List(decisionSelector, pdct.placement.Namespace)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list PlacementDecisions: %w", err)
	}

	// Get the decision cluster names
	newScheduledClusters := sets.NewString()
	for _, d := range decisions {
		for _, sd := range d.Status.Decisions {
			newScheduledClusters.Insert(sd.ClusterName)
		}
	}

	// Compare the difference
	added := newScheduledClusters.Difference(pdct.existingScheduledClusters)
	deleted := pdct.existingScheduledClusters.Difference(newScheduledClusters)

	// Update the existing decision cluster names
	pdct.existingScheduledClusters = newScheduledClusters

	return added, deleted, nil
}

// Existing() returns the tracker's existing decision cluster names.
func (pdct *PlacementDecisionClustersTracker) Existing() sets.String {
	pdct.lock.RLock()
	defer pdct.lock.RUnlock()

	return pdct.existingScheduledClusters
}
