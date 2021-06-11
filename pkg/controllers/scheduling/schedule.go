package scheduling

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	errorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterclient "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterlisterv1alpha1 "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1alpha1"
	clusterapiv1 "github.com/open-cluster-management/api/cluster/v1"
	clusterapiv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
)

const (
	maxNumOfClusterDecisions = 100
)

type scheduleFunc func(
	ctx context.Context,
	placement *clusterapiv1alpha1.Placement,
	clusters []*clusterapiv1.ManagedCluster,
	clusterClient clusterclient.Interface,
	placementDecisionLister clusterlisterv1alpha1.PlacementDecisionLister,
) (*scheduleResult, error)

type scheduleResult struct {
	feasibleClusters     int
	scheduledDecisions   int
	unscheduledDecisions int
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
	// TODO: sort the feasible clusters and make sure the selection stable
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
		feasibleClusters:     len(feasibleClusters),
		scheduledDecisions:   scheduled,
		unscheduledDecisions: unscheduled,
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
// cluster decision slice. New placementdecisions will be created if no one exists.
func bind(
	ctx context.Context,
	placement *clusterapiv1alpha1.Placement,
	clusterDecisions []clusterapiv1alpha1.ClusterDecision,
	clusterClient clusterclient.Interface,
	placementDecisionLister clusterlisterv1alpha1.PlacementDecisionLister,
) error {
	// sort clusterdecisions by cluster name
	sort.SliceStable(clusterDecisions, func(i, j int) bool {
		return clusterDecisions[i].ClusterName < clusterDecisions[j].ClusterName
	})

	// split the cluster decisions into slices, the size of each slice cannot exceed
	// maxNumOfClusterDecisions.
	decisionSlices := [][]clusterapiv1alpha1.ClusterDecision{}
	remainingDecisions := clusterDecisions
	for index := 0; len(remainingDecisions) > 0; index++ {
		var decisionSlice []clusterapiv1alpha1.ClusterDecision
		switch {
		case len(remainingDecisions) > maxNumOfClusterDecisions:
			decisionSlice = remainingDecisions[0:maxNumOfClusterDecisions]
			remainingDecisions = remainingDecisions[maxNumOfClusterDecisions:]
		default:
			decisionSlice = remainingDecisions
			remainingDecisions = nil
		}
		decisionSlices = append(decisionSlices, decisionSlice)
	}

	// bind cluster decision slices to placementdecisions.
	errs := []error{}
	placementDecisionNames := sets.NewString()
	for index, decisionSlice := range decisionSlices {
		placementDecisionName := fmt.Sprintf("%s-decision-%d", placement.Name, index+1)
		placementDecisionNames.Insert(placementDecisionName)
		err := createOrUpdatePlacementDecision(ctx, placement, placementDecisionName, decisionSlice, clusterClient, placementDecisionLister)
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) != 0 {
		return errorhelpers.NewMultiLineAggregate(errs)
	}

	// query all placementdecisions of the placement
	requirement, err := labels.NewRequirement(placementLabel, selection.Equals, []string{placement.Name})
	if err != nil {
		return err
	}
	labelSelector := labels.NewSelector().Add(*requirement)
	placementDecisions, err := placementDecisionLister.PlacementDecisions(placement.Namespace).List(labelSelector)
	if err != nil {
		return err
	}

	// delete redundant placementdecisions
	errs = []error{}
	for _, placementDecision := range placementDecisions {
		if placementDecisionNames.Has(placementDecision.Name) {
			continue
		}
		err := clusterClient.ClusterV1alpha1().PlacementDecisions(placementDecision.Namespace).Delete(ctx, placementDecision.Name, metav1.DeleteOptions{})
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errorhelpers.NewMultiLineAggregate(errs)
}

// createOrUpdatePlacementDecision creates a new PlacementDecision if it does not exist and
// then updates the status with the given ClusterDecision slice if necessary
func createOrUpdatePlacementDecision(
	ctx context.Context,
	placement *clusterapiv1alpha1.Placement,
	placementDecisionName string,
	clusterDecisions []clusterapiv1alpha1.ClusterDecision,
	clusterClient clusterclient.Interface,
	placementDecisionLister clusterlisterv1alpha1.PlacementDecisionLister,
) error {
	if len(clusterDecisions) > maxNumOfClusterDecisions {
		return fmt.Errorf("the number of clusterdecisions %q exceeds the max limitation %q", len(clusterDecisions), maxNumOfClusterDecisions)
	}

	placementDecision, err := placementDecisionLister.PlacementDecisions(placement.Namespace).Get(placementDecisionName)
	switch {
	case errors.IsNotFound(err):
		// create the placementdecision if not exists
		owner := metav1.NewControllerRef(placement, clusterapiv1alpha1.GroupVersion.WithKind("Placement"))
		placementDecision = &clusterapiv1alpha1.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      placementDecisionName,
				Namespace: placement.Namespace,
				Labels: map[string]string{
					placementLabel: placement.Name,
				},
				OwnerReferences: []metav1.OwnerReference{*owner},
			},
		}
		var err error
		placementDecision, err = clusterClient.ClusterV1alpha1().PlacementDecisions(placement.Namespace).Create(ctx, placementDecision, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	case err != nil:
		return err
	}

	// update the status of the placementdecision if decisions change
	if reflect.DeepEqual(placementDecision.Status.Decisions, clusterDecisions) {
		return nil
	}

	newPlacementDecision := placementDecision.DeepCopy()
	newPlacementDecision.Status.Decisions = clusterDecisions
	_, err = clusterClient.ClusterV1alpha1().PlacementDecisions(newPlacementDecision.Namespace).
		UpdateStatus(ctx, newPlacementDecision, metav1.UpdateOptions{})
	return err
}
