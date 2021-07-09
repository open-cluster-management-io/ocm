package scheduling

import (
	"context"
	"fmt"
	"sort"

	errorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	"open-cluster-management.io/placement/pkg/plugins"
	"open-cluster-management.io/placement/pkg/plugins/balance"
	"open-cluster-management.io/placement/pkg/plugins/predicate"
	"open-cluster-management.io/placement/pkg/plugins/steady"
)

const (
	maxNumOfClusterDecisions = 100
)

type Scheduler interface {
	schedule(
		ctx context.Context,
		placement *clusterapiv1alpha1.Placement,
		clusters []*clusterapiv1.ManagedCluster,
	) (*scheduleResult, error)
}

type scheduleResult struct {
	feasibleClusters     int
	scheduledDecisions   int
	unscheduledDecisions int
}

type pluginScore struct {
	sumScore int64
	scores   map[string]int64
}

func newPluginScore() *pluginScore {
	return &pluginScore{
		sumScore: 0,
		scores:   map[string]int64{},
	}
}

func (p *pluginScore) add(pluginName string, score int64) {
	p.sumScore = p.sumScore + score
	p.scores[pluginName] = score
}

func (p *pluginScore) sum() int64 {
	return p.sumScore
}

func (p *pluginScore) string() string {
	output := ""

	for name, score := range p.scores {
		output = fmt.Sprintf("%splugin: %s, score: %d; ", output, name, score)
	}

	return output
}

type pluginScheduler struct {
	filters       []plugins.Filter
	prioritizers  []plugins.Prioritizer
	clientWrapper plugins.Handle
}

func newPluginScheduler(handle plugins.Handle) *pluginScheduler {
	return &pluginScheduler{
		filters: []plugins.Filter{
			predicate.New(handle),
		},
		prioritizers: []plugins.Prioritizer{
			steady.New(handle),
			balance.New(handle),
		},
		clientWrapper: handle,
	}
}

func (s *pluginScheduler) schedule(
	ctx context.Context,
	placement *clusterapiv1alpha1.Placement,
	clusters []*clusterapiv1.ManagedCluster,
) (*scheduleResult, error) {
	var err error
	filtered := clusters

	// filter clusters
	for _, f := range s.filters {
		filtered, err = f.Filter(ctx, placement, filtered)

		if err != nil {
			return nil, err
		}
	}

	// score clusters
	// Score the cluster
	scoreSum := map[string]*pluginScore{}
	for _, cluster := range filtered {
		scoreSum[cluster.Name] = newPluginScore()
	}
	for _, p := range s.prioritizers {
		score, err := p.Score(ctx, placement, filtered)
		if err != nil {
			return nil, err
		}

		// TODO we currently weigh each prioritizer as equal. We should consider
		// importance factor for each priotizer when caculating the final score.
		// Since currently balance plugin has a score range of +/- 100 while the score range of
		// balacne is 0/100, the balance plugin will trigger the reschedule for rebalancing when
		// a cluster's decision count is larger than average.
		for name, val := range score {
			scoreSum[name].add(p.Name(), val)
		}
	}

	// Sort cluster by score
	sort.SliceStable(filtered, func(i, j int) bool {
		return scoreSum[clusters[i].Name].sum() > scoreSum[clusters[j].Name].sum()
	})

	// select clusters and generate cluster decisions
	// TODO: sort the feasible clusters and make sure the selection stable
	decisions := selectClusters(placement, filtered)
	scheduled, unscheduled := len(decisions), 0
	if placement.Spec.NumberOfClusters != nil {
		unscheduled = int(*placement.Spec.NumberOfClusters) - scheduled
	}

	// bind the cluster decisions into placementdecisions
	err = s.bind(ctx, placement, decisions, scoreSum)
	if err != nil {
		return nil, err
	}

	return &scheduleResult{
		feasibleClusters:     len(filtered),
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
func (s *pluginScheduler) bind(
	ctx context.Context,
	placement *clusterapiv1alpha1.Placement,
	clusterDecisions []clusterapiv1alpha1.ClusterDecision,
	score map[string]*pluginScore,
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
		err := s.createOrUpdatePlacementDecision(
			ctx, placement, placementDecisionName, decisionSlice, score)
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
	placementDecisions, err := s.clientWrapper.DecisionLister().PlacementDecisions(placement.Namespace).List(labelSelector)
	if err != nil {
		return err
	}

	// delete redundant placementdecisions
	errs = []error{}
	for _, placementDecision := range placementDecisions {
		if placementDecisionNames.Has(placementDecision.Name) {
			continue
		}
		err := s.clientWrapper.ClusterClient().ClusterV1alpha1().PlacementDecisions(
			placementDecision.Namespace).Delete(ctx, placementDecision.Name, metav1.DeleteOptions{})
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
		}
		s.clientWrapper.EventRecorder().Eventf(
			placement, placementDecision, corev1.EventTypeNormal,
			"DecisionDelete", "DecisionDeleted",
			"Decision %s is deleted with placement %s in namespace %s", placementDecision.Name, placement.Name, placement.Namespace)
	}
	return errorhelpers.NewMultiLineAggregate(errs)
}

// createOrUpdatePlacementDecision creates a new PlacementDecision if it does not exist and
// then updates the status with the given ClusterDecision slice if necessary
func (s *pluginScheduler) createOrUpdatePlacementDecision(
	ctx context.Context,
	placement *clusterapiv1alpha1.Placement,
	placementDecisionName string,
	clusterDecisions []clusterapiv1alpha1.ClusterDecision,
	scores map[string]*pluginScore,
) error {
	if len(clusterDecisions) > maxNumOfClusterDecisions {
		return fmt.Errorf("the number of clusterdecisions %q exceeds the max limitation %q", len(clusterDecisions), maxNumOfClusterDecisions)
	}

	placementDecision, err := s.clientWrapper.DecisionLister().PlacementDecisions(placement.Namespace).Get(placementDecisionName)
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
		placementDecision, err = s.clientWrapper.ClusterClient().ClusterV1alpha1().PlacementDecisions(
			placement.Namespace).Create(ctx, placementDecision, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		s.clientWrapper.EventRecorder().Eventf(
			placement, placementDecision, corev1.EventTypeNormal,
			"DecisionCreate", "DecisionCreated",
			"Decision %s is created with placement %s in namespace %s", placementDecision.Name, placement.Name, placement.Namespace)
	case err != nil:
		return err
	}

	// update the status of the placementdecision if decisions change
	added, deleted, updated := s.compareDecision(placementDecision.Status.Decisions, clusterDecisions)
	if !updated {
		return nil
	}

	newPlacementDecision := placementDecision.DeepCopy()
	newPlacementDecision.Status.Decisions = clusterDecisions
	newPlacementDecision, err = s.clientWrapper.ClusterClient().ClusterV1alpha1().PlacementDecisions(newPlacementDecision.Namespace).
		UpdateStatus(ctx, newPlacementDecision, metav1.UpdateOptions{})

	if err != nil {
		return err
	}

	for clusterName := range added {
		score, ok := scores[clusterName]
		if !ok {
			continue
		}
		s.clientWrapper.EventRecorder().Eventf(
			placement, newPlacementDecision, corev1.EventTypeNormal,
			"DecisionUpdate", "DecisionUpdated",
			"cluster %s is added into placementDecision %s in namespace %s with score %s ",
			clusterName, placementDecision.Name, placement.Namespace, score.string())
	}

	for clusterName := range deleted {
		score, ok := scores[clusterName]
		if !ok {
			continue
		}
		s.clientWrapper.EventRecorder().Eventf(
			placement, newPlacementDecision, corev1.EventTypeNormal,
			"DecisionUpdate", "DecisionUpdated",
			"cluster %s is removed from placementDecision %s in namespace %s with score %s ",
			clusterName, placementDecision.Name, placement.Namespace, score.string())
	}

	return nil
}

// compareDecision compare the existing decision with desired decision. It outputs a result on why
// a decision is chosen, and whether the decision results should be updated.
func (s *pluginScheduler) compareDecision(
	existingDecisions, desiredDecisions []clusterapiv1alpha1.ClusterDecision) (sets.String, sets.String, bool) {

	existing := sets.NewString()

	desired := sets.NewString()

	for _, d := range existingDecisions {
		existing.Insert(d.ClusterName)
	}

	for _, d := range desiredDecisions {
		desired.Insert(d.ClusterName)
	}

	if existing.Equal(desired) {
		return nil, nil, false
	}

	added := desired.Difference(existing)

	deleted := existing.Difference(desired)

	return added, deleted, true
}
