package scheduling

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	clusterclient "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterlisterv1alpha1 "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1alpha1"
	clusterapiv1 "github.com/open-cluster-management/api/cluster/v1"
	clusterapiv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
)

type scheduleFunc func(
	ctx context.Context,
	placement *clusterapiv1alpha1.Placement,
	clusters []*clusterapiv1.ManagedCluster,
	clusterClient clusterclient.Interface,
	placementDecisionLister clusterlisterv1alpha1.PlacementDecisionLister,
) (*scheduleResult, error)

type scheduleResult struct {
	scheduled   int
	unscheduled int
}

func schedule(
	ctx context.Context,
	placement *clusterapiv1alpha1.Placement,
	clusters []*clusterapiv1.ManagedCluster,
	clusterClient clusterclient.Interface,
	placementDecisionLister clusterlisterv1alpha1.PlacementDecisionLister,
) (*scheduleResult, error) {
	// filter clusters with cluster predicates
	feasibleClusters, err := matchWithClusterPredicates(placement.Spec.Predicates, clusters)
	if err != nil {
		return nil, err
	}

	// select clusters and generate cluster decisions
	decisions := selectClusters(placement, feasibleClusters)
	scheduled, unscheduled := len(decisions), 0
	if placement.Spec.NumberOfClusters != nil {
		unscheduled = int(*placement.Spec.NumberOfClusters) - scheduled
	}

	// bind the cluster decisions into placementdecisions
	err = bind(ctx, placement, decisions, clusterClient, placementDecisionLister)
	if err != nil {
		return nil, err
	}

	return &scheduleResult{
		scheduled:   scheduled,
		unscheduled: unscheduled,
	}, nil
}

// makeClusterDecisions selects clusters based on given cluster slice and then creates
// cluster decisions.
func selectClusters(placement *clusterapiv1alpha1.Placement, clusters []*clusterapiv1.ManagedCluster) []clusterapiv1alpha1.ClusterDecision {
	numOfDecisions := len(clusters)
	if placement.Spec.NumberOfClusters != nil {
		numOfDecisions = int(*placement.Spec.NumberOfClusters)
	}

	// truncate the cluster slice if the desired number of decisions is less than
	// the number of the candidate clusters
	if numOfDecisions < len(clusters) {
		clusters = clusters[:numOfDecisions]
	}

	decisions := []clusterapiv1alpha1.ClusterDecision{}
	for _, cluster := range clusters {
		decisions = append(decisions, clusterapiv1alpha1.ClusterDecision{
			ClusterName: cluster.Name,
		})
	}
	return decisions
}

// bind updates the cluster decisions in the status of the placementdecisions with the given
// cluster decision slice. New placementdecision will be created if no one exists.
func bind(
	ctx context.Context,
	placement *clusterapiv1alpha1.Placement,
	clusterDecisions []clusterapiv1alpha1.ClusterDecision,
	clusterClient clusterclient.Interface,
	placementDecisionLister clusterlisterv1alpha1.PlacementDecisionLister,
) error {
	// query placementdecisions with label selector
	requirement, err := labels.NewRequirement(placementLabel, selection.Equals, []string{placement.Name})
	if err != nil {
		return err
	}
	labelSelector := labels.NewSelector().Add(*requirement)
	placementDecisions, err := placementDecisionLister.PlacementDecisions(placement.Namespace).List(labelSelector)
	if err != nil {
		return err
	}

	// TODO: support multiple placementdecisions for a placement
	var placementDecision *clusterapiv1alpha1.PlacementDecision
	switch {
	case len(placementDecisions) > 0:
		placementDecision = placementDecisions[0]
	default:
		// create a placementdecision if not exists
		owner := metav1.NewControllerRef(placement, clusterapiv1alpha1.GroupVersion.WithKind("Placement"))
		placementDecision = &clusterapiv1alpha1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: fmt.Sprintf("%s-", placement.Name),
				Namespace:    placement.Namespace,
				Labels: map[string]string{
					placementLabel: placement.Name,
				},
				OwnerReferences: []metav1.OwnerReference{*owner},
			},
		}
		placementDecision, err = clusterClient.ClusterV1alpha1().PlacementDecisions(placement.Namespace).Create(ctx, placementDecision, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}

	// sort by cluster name
	sort.SliceStable(clusterDecisions, func(i, j int) bool {
		return clusterDecisions[i].ClusterName < clusterDecisions[j].ClusterName
	})

	// update the status of the placementdecision if necessary
	if reflect.DeepEqual(placementDecision.Status.Decisions, clusterDecisions) {
		return nil
	}
	newPlacementDecision := placementDecision.DeepCopy()
	newPlacementDecision.Status.Decisions = clusterDecisions
	_, err = clusterClient.ClusterV1alpha1().PlacementDecisions(newPlacementDecision.Namespace).
		UpdateStatus(ctx, newPlacementDecision, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}
