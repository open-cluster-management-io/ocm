package testing

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterapiv1 "github.com/open-cluster-management/api/cluster/v1"
	clusterapiv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
)

type placementBuilder struct {
	placement *clusterapiv1alpha1.Placement
}

func NewPlacement(namespace, name string) *placementBuilder {
	return &placementBuilder{
		placement: &clusterapiv1alpha1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		},
	}
}

func (b *placementBuilder) WithUID(uid string) *placementBuilder {
	b.placement.UID = types.UID(uid)
	return b
}

func (b *placementBuilder) WithNOC(noc int32) *placementBuilder {
	b.placement.Spec.NumberOfClusters = &noc
	return b
}

func (b *placementBuilder) WithClusterSets(clusterSets []string) *placementBuilder {
	b.placement.Spec.ClusterSets = clusterSets
	return b
}

func (b *placementBuilder) WithDeletionTimestamp() *placementBuilder {
	now := metav1.Now()
	b.placement.DeletionTimestamp = &now
	return b
}

func (b *placementBuilder) AddPredicate(labelSelector *metav1.LabelSelector, claimSelector *clusterapiv1alpha1.ClusterClaimSelector) *placementBuilder {
	if b.placement.Spec.Predicates == nil {
		b.placement.Spec.Predicates = []clusterapiv1alpha1.ClusterPredicate{}
	}
	b.placement.Spec.Predicates = append(b.placement.Spec.Predicates, NewClusterPredicate(labelSelector, claimSelector))
	return b
}

func (b *placementBuilder) WithNumOfSelectedClusters(nosc int) *placementBuilder {
	b.placement.Status.NumberOfSelectedClusters = int32(nosc)
	return b
}

func (b *placementBuilder) WithSatisfiedCondition(numbOfScheduledDecisions, numbOfUnscheduledDecisions int) *placementBuilder {
	condition := metav1.Condition{
		Type: clusterapiv1alpha1.PlacementConditionSatisfied,
	}
	switch {
	case numbOfUnscheduledDecisions == 0:
		condition.Status = metav1.ConditionTrue
		condition.Reason = "AllDecisionsScheduled"
		condition.Message = "All cluster decisions scheduled"
	default:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "NotAllDecisionsScheduled"
		condition.Message = fmt.Sprintf("%d cluster decisions unscheduled", numbOfUnscheduledDecisions)
	}
	meta.SetStatusCondition(&b.placement.Status.Conditions, condition)
	return b
}

func (b *placementBuilder) Build() *clusterapiv1alpha1.Placement {
	return b.placement
}

func NewClusterPredicate(labelSelector *metav1.LabelSelector, claimSelector *clusterapiv1alpha1.ClusterClaimSelector) clusterapiv1alpha1.ClusterPredicate {
	predicate := clusterapiv1alpha1.ClusterPredicate{
		RequiredClusterSelector: clusterapiv1alpha1.ClusterSelector{},
	}

	if labelSelector != nil {
		predicate.RequiredClusterSelector.LabelSelector = *labelSelector
	}

	if claimSelector != nil {
		predicate.RequiredClusterSelector.ClaimSelector = *claimSelector
	}

	return predicate
}

type placementDecisionBuilder struct {
	placementDecision *clusterapiv1alpha1.PlacementDecision
}

func NewPlacementDecision(namespace, name string) *placementDecisionBuilder {
	return &placementDecisionBuilder{
		placementDecision: &clusterapiv1alpha1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		},
	}
}

func (b *placementDecisionBuilder) WithController(uid string) *placementDecisionBuilder {
	controller := true
	b.placementDecision.OwnerReferences = append(b.placementDecision.OwnerReferences, metav1.OwnerReference{
		Controller: &controller,
		UID:        types.UID(uid),
	})
	return b
}

func (b *placementDecisionBuilder) WithLabel(name, value string) *placementDecisionBuilder {
	if b.placementDecision.Labels == nil {
		b.placementDecision.Labels = map[string]string{}
	}
	b.placementDecision.Labels[name] = value
	return b
}

func (b *placementDecisionBuilder) WithDeletionTimestamp() *placementDecisionBuilder {
	now := metav1.Now()
	b.placementDecision.DeletionTimestamp = &now
	return b
}

func (b *placementDecisionBuilder) WithDecisions(clusterNames ...string) *placementDecisionBuilder {
	decisions := []clusterapiv1alpha1.ClusterDecision{}
	for _, clusterName := range clusterNames {
		decisions = append(decisions, clusterapiv1alpha1.ClusterDecision{
			ClusterName: clusterName,
		})
	}
	b.placementDecision.Status.Decisions = decisions
	return b
}

func (b *placementDecisionBuilder) Build() *clusterapiv1alpha1.PlacementDecision {
	return b.placementDecision
}

type managedClusterBuilder struct {
	cluster *clusterapiv1.ManagedCluster
}

func NewManagedCluster(clusterName string) *managedClusterBuilder {
	return &managedClusterBuilder{
		cluster: &clusterapiv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		},
	}
}

func (b *managedClusterBuilder) WithLabel(name, value string) *managedClusterBuilder {
	if b.cluster.Labels == nil {
		b.cluster.Labels = map[string]string{}
	}
	b.cluster.Labels[name] = value
	return b
}

func (b *managedClusterBuilder) WithClaim(name, value string) *managedClusterBuilder {
	claimMap := map[string]string{}
	for _, claim := range b.cluster.Status.ClusterClaims {
		claimMap[claim.Name] = claim.Value
	}
	claimMap[name] = value

	clusterClaims := []clusterapiv1.ManagedClusterClaim{}
	for k, v := range claimMap {
		clusterClaims = append(clusterClaims, clusterapiv1.ManagedClusterClaim{
			Name:  k,
			Value: v,
		})
	}

	b.cluster.Status.ClusterClaims = clusterClaims
	return b
}

func (b *managedClusterBuilder) Build() *clusterapiv1.ManagedCluster {
	return b.cluster
}

func NewClusterSet(clusterSetName string) *clusterapiv1alpha1.ManagedClusterSet {
	return &clusterapiv1alpha1.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterSetName,
		},
	}
}

func NewClusterSetBinding(namespace, clusterSetName string) *clusterapiv1alpha1.ManagedClusterSetBinding {
	return &clusterapiv1alpha1.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      clusterSetName,
		},
		Spec: clusterapiv1alpha1.ManagedClusterSetBindingSpec{
			ClusterSet: clusterSetName,
		},
	}
}
