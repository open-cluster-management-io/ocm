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
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

type PlacementBuilder struct {
	placement *clusterapiv1beta1.Placement
}

func NewPlacement(namespace, name string) *PlacementBuilder {
	return &PlacementBuilder{
		placement: &clusterapiv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		},
	}
}

func NewPlacementWithAnnotations(namespace, name string, annotations map[string]string) *PlacementBuilder {
	return &PlacementBuilder{
		placement: &clusterapiv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   namespace,
				Name:        name,
				Annotations: annotations,
			},
		},
	}
}

func (b *PlacementBuilder) WithUID(uid string) *PlacementBuilder {
	b.placement.UID = types.UID(uid)
	return b
}

func (b *PlacementBuilder) WithNOC(noc int32) *PlacementBuilder {
	b.placement.Spec.NumberOfClusters = &noc
	return b
}

func (b *PlacementBuilder) WithGroupStrategy(groupStrategy clusterapiv1beta1.GroupStrategy) *PlacementBuilder {
	b.placement.Spec.DecisionStrategy.GroupStrategy = groupStrategy
	return b
}

func (b *PlacementBuilder) WithPrioritizerPolicy(mode clusterapiv1beta1.PrioritizerPolicyModeType) *PlacementBuilder {
	b.placement.Spec.PrioritizerPolicy = clusterapiv1beta1.PrioritizerPolicy{
		Mode: mode,
	}
	return b
}

func (b *PlacementBuilder) WithPrioritizerConfig(name string, weight int32) *PlacementBuilder {
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

func (b *PlacementBuilder) WithScoreCoordinateAddOn(resourceName, scoreName string, weight int32) *PlacementBuilder {
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

func (b *PlacementBuilder) WithClusterSets(clusterSets ...string) *PlacementBuilder {
	b.placement.Spec.ClusterSets = clusterSets
	return b
}

func (b *PlacementBuilder) WithDeletionTimestamp() *PlacementBuilder {
	now := metav1.Now()
	b.placement.DeletionTimestamp = &now
	return b
}

func (b *PlacementBuilder) AddPredicate(labelSelector *metav1.LabelSelector, claimSelector *clusterapiv1beta1.ClusterClaimSelector) *PlacementBuilder {
	if b.placement.Spec.Predicates == nil {
		b.placement.Spec.Predicates = []clusterapiv1beta1.ClusterPredicate{}
	}
	b.placement.Spec.Predicates = append(b.placement.Spec.Predicates, NewClusterPredicate(labelSelector, claimSelector))
	return b
}

func (b *PlacementBuilder) AddToleration(toleration *clusterapiv1beta1.Toleration) *PlacementBuilder {
	if b.placement.Spec.Tolerations == nil {
		b.placement.Spec.Tolerations = []clusterapiv1beta1.Toleration{}
	}
	b.placement.Spec.Tolerations = append(b.placement.Spec.Tolerations, *toleration)
	return b
}

func (b *PlacementBuilder) WithNumOfSelectedClusters(nosc int, placementName string) *PlacementBuilder {
	b.placement.Status.NumberOfSelectedClusters = int32(nosc)
	b.placement.Status.DecisionGroups = []clusterapiv1beta1.DecisionGroupStatus{
		{
			DecisionGroupIndex: 0,
			DecisionGroupName:  "",
			ClustersCount:      int32(nosc),
			Decisions:          []string{PlacementDecisionName(placementName, 1)},
		},
	}
	return b
}

func (b *PlacementBuilder) WithSatisfiedCondition(numbOfScheduledDecisions, numbOfUnscheduledDecisions int) *PlacementBuilder {
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

func (b *PlacementBuilder) WithMisconfiguredCondition(status metav1.ConditionStatus) *PlacementBuilder {
	condition := metav1.Condition{
		Type:    clusterapiv1beta1.PlacementConditionMisconfigured,
		Status:  status,
		Reason:  "Succeedconfigured",
		Message: "Placement configurations check pass",
	}
	meta.SetStatusCondition(&b.placement.Status.Conditions, condition)
	return b
}

func (b *PlacementBuilder) Build() *clusterapiv1beta1.Placement {
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

type PlacementDecisionBuilder struct {
	placementDecision *clusterapiv1beta1.PlacementDecision
}

func NewPlacementDecision(namespace, name string) *PlacementDecisionBuilder {
	return &PlacementDecisionBuilder{
		placementDecision: &clusterapiv1beta1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		},
	}
}

func (b *PlacementDecisionBuilder) WithController(uid string) *PlacementDecisionBuilder {
	controller := true
	b.placementDecision.OwnerReferences = append(b.placementDecision.OwnerReferences, metav1.OwnerReference{
		Controller: &controller,
		UID:        types.UID(uid),
	})
	return b
}

func (b *PlacementDecisionBuilder) WithLabel(name, value string) *PlacementDecisionBuilder {
	if b.placementDecision.Labels == nil {
		b.placementDecision.Labels = map[string]string{}
	}
	b.placementDecision.Labels[name] = value
	return b
}

func (b *PlacementDecisionBuilder) WithDeletionTimestamp() *PlacementDecisionBuilder {
	now := metav1.Now()
	b.placementDecision.DeletionTimestamp = &now
	return b
}

func (b *PlacementDecisionBuilder) WithDecisions(clusterNames ...string) *PlacementDecisionBuilder {
	var decisions []clusterapiv1beta1.ClusterDecision
	for _, clusterName := range clusterNames {
		decisions = append(decisions, clusterapiv1beta1.ClusterDecision{
			ClusterName: clusterName,
		})
	}
	b.placementDecision.Status.Decisions = decisions
	return b
}

func (b *PlacementDecisionBuilder) Build() *clusterapiv1beta1.PlacementDecision {
	return b.placementDecision
}

type ManagedClusterBuilder struct {
	cluster *clusterapiv1.ManagedCluster
}

func NewManagedCluster(clusterName string) *ManagedClusterBuilder {
	return &ManagedClusterBuilder{
		cluster: &clusterapiv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		},
	}
}

func (b *ManagedClusterBuilder) WithLabel(name, value string) *ManagedClusterBuilder {
	if b.cluster.Labels == nil {
		b.cluster.Labels = map[string]string{}
	}
	b.cluster.Labels[name] = value
	return b
}

func (b *ManagedClusterBuilder) WithClaim(name, value string) *ManagedClusterBuilder {
	claimMap := map[string]string{}
	for _, claim := range b.cluster.Status.ClusterClaims {
		claimMap[claim.Name] = claim.Value
	}
	claimMap[name] = value

	var clusterClaims []clusterapiv1.ManagedClusterClaim
	for k, v := range claimMap {
		clusterClaims = append(clusterClaims, clusterapiv1.ManagedClusterClaim{
			Name:  k,
			Value: v,
		})
	}

	b.cluster.Status.ClusterClaims = clusterClaims
	return b
}

func (b *ManagedClusterBuilder) WithResource(resourceName clusterapiv1.ResourceName, allocatable, capacity string) *ManagedClusterBuilder {
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

func (b *ManagedClusterBuilder) WithTaint(taint *clusterapiv1.Taint) *ManagedClusterBuilder {
	if b.cluster.Spec.Taints == nil {
		b.cluster.Spec.Taints = []clusterapiv1.Taint{}
	}
	b.cluster.Spec.Taints = append(b.cluster.Spec.Taints, *taint)
	return b
}

func (b *ManagedClusterBuilder) WithDeletionTimestamp() *ManagedClusterBuilder {
	if b.cluster.DeletionTimestamp.IsZero() {
		t := metav1.Now()
		b.cluster.DeletionTimestamp = &t
	}

	return b
}

func (b *ManagedClusterBuilder) Build() *clusterapiv1.ManagedCluster {
	return b.cluster
}

type ManagedClusterSetBuilder struct {
	clusterset *clusterapiv1beta2.ManagedClusterSet
}

func NewClusterSet(clusterSetName string) *ManagedClusterSetBuilder {
	return &ManagedClusterSetBuilder{
		clusterset: &clusterapiv1beta2.ManagedClusterSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterSetName,
			},
		},
	}
}

func (b *ManagedClusterSetBuilder) WithClusterSelector(clusterSelector clusterapiv1beta2.ManagedClusterSelector) *ManagedClusterSetBuilder {
	b.clusterset.Spec.ClusterSelector = clusterSelector
	return b
}

func (b *ManagedClusterSetBuilder) Build() *clusterapiv1beta2.ManagedClusterSet {
	return b.clusterset
}

func NewClusterSetBinding(namespace, clusterSetName string) *clusterapiv1beta2.ManagedClusterSetBinding {
	return &clusterapiv1beta2.ManagedClusterSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      clusterSetName,
		},
		Spec: clusterapiv1beta2.ManagedClusterSetBindingSpec{
			ClusterSet: clusterSetName,
		},
	}
}

type AddOnPlacementScoreBuilder struct {
	addOnPlacementScore *clusterapiv1alpha1.AddOnPlacementScore
}

func NewAddOnPlacementScore(clusternamespace, name string) *AddOnPlacementScoreBuilder {
	return &AddOnPlacementScoreBuilder{
		addOnPlacementScore: &clusterapiv1alpha1.AddOnPlacementScore{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: clusternamespace,
				Name:      name,
			},
		},
	}
}

func (a *AddOnPlacementScoreBuilder) WithScore(name string, score int32) *AddOnPlacementScoreBuilder {
	if a.addOnPlacementScore.Status.Scores == nil {
		a.addOnPlacementScore.Status.Scores = []clusterapiv1alpha1.AddOnPlacementScoreItem{}
	}

	a.addOnPlacementScore.Status.Scores = append(a.addOnPlacementScore.Status.Scores, clusterapiv1alpha1.AddOnPlacementScoreItem{
		Name:  name,
		Value: score,
	})
	return a
}

func (a *AddOnPlacementScoreBuilder) WithValidUntil(validUntil time.Time) *AddOnPlacementScoreBuilder {
	vu := metav1.NewTime(validUntil)
	a.addOnPlacementScore.Status.ValidUntil = &vu
	return a
}

func (a *AddOnPlacementScoreBuilder) Build() *clusterapiv1alpha1.AddOnPlacementScore {
	return a.addOnPlacementScore
}

func PlacementDecisionName(placementName string, index int) string {
	return fmt.Sprintf("%s-decision-%d", placementName, index)
}
