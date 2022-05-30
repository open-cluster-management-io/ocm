package testing

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

type placementBuilder struct {
	placement *clusterapiv1beta1.Placement
}

func NewPlacement(namespace, name string) *placementBuilder {
	return &placementBuilder{
		placement: &clusterapiv1beta1.Placement{
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

func (b *placementBuilder) WithPrioritizerPolicy(mode clusterapiv1beta1.PrioritizerPolicyModeType) *placementBuilder {
	b.placement.Spec.PrioritizerPolicy = clusterapiv1beta1.PrioritizerPolicy{
		Mode: mode,
	}
	return b
}

func (b *placementBuilder) WithPrioritizerConfig(name string, weight int32) *placementBuilder {
	if b.placement.Spec.PrioritizerPolicy.Configurations == nil {
		b.placement.Spec.PrioritizerPolicy.Configurations = []clusterapiv1beta1.PrioritizerConfig{}
	}
	if len(name) > 0 {
		b.placement.Spec.PrioritizerPolicy.Configurations = append(b.placement.Spec.PrioritizerPolicy.Configurations, clusterapiv1beta1.PrioritizerConfig{
			ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
				Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
				BuiltIn: name,
			},
			Weight: weight,
		})
	}
	return b
}

func (b *placementBuilder) WithScoreCoordinateAddOn(resourceName, scoreName string, weight int32) *placementBuilder {
	if b.placement.Spec.PrioritizerPolicy.Configurations == nil {
		b.placement.Spec.PrioritizerPolicy.Configurations = []clusterapiv1beta1.PrioritizerConfig{}
	}
	b.placement.Spec.PrioritizerPolicy.Configurations = append(b.placement.Spec.PrioritizerPolicy.Configurations, clusterapiv1beta1.PrioritizerConfig{
		ScoreCoordinate: &clusterapiv1beta1.ScoreCoordinate{
			Type: clusterapiv1beta1.ScoreCoordinateTypeAddOn,
			AddOn: &clusterapiv1beta1.AddOnScore{
				ResourceName: resourceName,
				ScoreName:    scoreName,
			},
		},
		Weight: weight})
	return b
}

func (b *placementBuilder) WithClusterSets(clusterSets ...string) *placementBuilder {
	b.placement.Spec.ClusterSets = clusterSets
	return b
}

func (b *placementBuilder) WithDeletionTimestamp() *placementBuilder {
	now := metav1.Now()
	b.placement.DeletionTimestamp = &now
	return b
}

func (b *placementBuilder) AddPredicate(labelSelector *metav1.LabelSelector, claimSelector *clusterapiv1beta1.ClusterClaimSelector) *placementBuilder {
	if b.placement.Spec.Predicates == nil {
		b.placement.Spec.Predicates = []clusterapiv1beta1.ClusterPredicate{}
	}
	b.placement.Spec.Predicates = append(b.placement.Spec.Predicates, NewClusterPredicate(labelSelector, claimSelector))
	return b
}

func (b *placementBuilder) AddToleration(toleration *clusterapiv1beta1.Toleration) *placementBuilder {
	if b.placement.Spec.Tolerations == nil {
		b.placement.Spec.Tolerations = []clusterapiv1beta1.Toleration{}
	}
	b.placement.Spec.Tolerations = append(b.placement.Spec.Tolerations, *toleration)
	return b
}

func (b *placementBuilder) WithNumOfSelectedClusters(nosc int) *placementBuilder {
	b.placement.Status.NumberOfSelectedClusters = int32(nosc)
	return b
}

func (b *placementBuilder) WithSatisfiedCondition(numbOfScheduledDecisions, numbOfUnscheduledDecisions int) *placementBuilder {
	condition := metav1.Condition{
		Type: clusterapiv1beta1.PlacementConditionSatisfied,
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

func (b *placementBuilder) Build() *clusterapiv1beta1.Placement {
	return b.placement
}

func NewClusterPredicate(labelSelector *metav1.LabelSelector, claimSelector *clusterapiv1beta1.ClusterClaimSelector) clusterapiv1beta1.ClusterPredicate {
	predicate := clusterapiv1beta1.ClusterPredicate{
		RequiredClusterSelector: clusterapiv1beta1.ClusterSelector{},
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
	placementDecision *clusterapiv1beta1.PlacementDecision
}

func NewPlacementDecision(namespace, name string) *placementDecisionBuilder {
	return &placementDecisionBuilder{
		placementDecision: &clusterapiv1beta1.PlacementDecision{
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
	decisions := []clusterapiv1beta1.ClusterDecision{}
	for _, clusterName := range clusterNames {
		decisions = append(decisions, clusterapiv1beta1.ClusterDecision{
			ClusterName: clusterName,
		})
	}
	b.placementDecision.Status.Decisions = decisions
	return b
}

func (b *placementDecisionBuilder) Build() *clusterapiv1beta1.PlacementDecision {
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

func (b *managedClusterBuilder) WithResource(resourceName clusterapiv1.ResourceName, allocatable, capacity string) *managedClusterBuilder {
	if b.cluster.Status.Allocatable == nil {
		b.cluster.Status.Allocatable = make(map[clusterapiv1.ResourceName]resource.Quantity)
	}
	if b.cluster.Status.Capacity == nil {
		b.cluster.Status.Capacity = make(map[clusterapiv1.ResourceName]resource.Quantity)
	}

	b.cluster.Status.Allocatable[resourceName], _ = resource.ParseQuantity(allocatable)
	b.cluster.Status.Capacity[resourceName], _ = resource.ParseQuantity(capacity)
	return b
}

func (b *managedClusterBuilder) WithTaint(taint *clusterapiv1.Taint) *managedClusterBuilder {
	if b.cluster.Spec.Taints == nil {
		b.cluster.Spec.Taints = []clusterapiv1.Taint{}
	}
	b.cluster.Spec.Taints = append(b.cluster.Spec.Taints, *taint)
	return b
}

func (b *managedClusterBuilder) Build() *clusterapiv1.ManagedCluster {
	return b.cluster
}

type managedClusterSetBuilder struct {
	clusterset *clusterapiv1beta1.ManagedClusterSet
}

func NewClusterSet(clusterSetName string) *managedClusterSetBuilder {
	return &managedClusterSetBuilder{
		clusterset: &clusterapiv1beta1.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
		},
	}
}

func (b *managedClusterSetBuilder) WithClusterSelector(clusterSelector clusterapiv1beta1.ManagedClusterSelector) *managedClusterSetBuilder {
	b.clusterset.Spec.ClusterSelector = clusterSelector
	return b
}

func (b *managedClusterSetBuilder) Build() *clusterapiv1beta1.ManagedClusterSet {
	return b.clusterset
}

func NewClusterSetBinding(namespace, clusterSetName string) *clusterapiv1beta1.ManagedClusterSetBinding {
	return &clusterapiv1beta1.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      clusterSetName,
		},
		Spec: clusterapiv1beta1.ManagedClusterSetBindingSpec{
			ClusterSet: clusterSetName,
		},
	}
}

type addOnPlacementScoreBuilder struct {
	addOnPlacementScore *clusterapiv1alpha1.AddOnPlacementScore
}

func NewAddOnPlacementScore(clusternamespace, name string) *addOnPlacementScoreBuilder {
	return &addOnPlacementScoreBuilder{
		addOnPlacementScore: &clusterapiv1alpha1.AddOnPlacementScore{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: clusternamespace,
				Name:      name,
			},
		},
	}
}

func (a *addOnPlacementScoreBuilder) WithScore(name string, score int32) *addOnPlacementScoreBuilder {
	if a.addOnPlacementScore.Status.Scores == nil {
		a.addOnPlacementScore.Status.Scores = []clusterapiv1alpha1.AddOnPlacementScoreItem{}
	}

	a.addOnPlacementScore.Status.Scores = append(a.addOnPlacementScore.Status.Scores, clusterapiv1alpha1.AddOnPlacementScoreItem{
		Name:  name,
		Value: score,
	})
	return a
}

func (a *addOnPlacementScoreBuilder) WithValidUntil(validUntil time.Time) *addOnPlacementScoreBuilder {
	vu := metav1.NewTime(validUntil)
	a.addOnPlacementScore.Status.ValidUntil = &vu
	return a
}

func (a *addOnPlacementScoreBuilder) Build() *clusterapiv1alpha1.AddOnPlacementScore {
	return a.addOnPlacementScore
}
