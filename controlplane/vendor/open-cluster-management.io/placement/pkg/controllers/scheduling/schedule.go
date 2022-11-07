package scheduling

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	kevents "k8s.io/client-go/tools/events"
	"k8s.io/klog/v2"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1alpha1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/placement/pkg/controllers/framework"
	"open-cluster-management.io/placement/pkg/plugins"
	"open-cluster-management.io/placement/pkg/plugins/addon"
	"open-cluster-management.io/placement/pkg/plugins/balance"
	"open-cluster-management.io/placement/pkg/plugins/predicate"
	"open-cluster-management.io/placement/pkg/plugins/resource"
	"open-cluster-management.io/placement/pkg/plugins/steady"
	"open-cluster-management.io/placement/pkg/plugins/tainttoleration"
)

const (
	PrioritizerBalance                   string = "Balance"
	PrioritizerSteady                    string = "Steady"
	PrioritizerResourceAllocatableCPU    string = "ResourceAllocatableCPU"
	PrioritizerResourceAllocatableMemory string = "ResourceAllocatableMemory"
)

// PrioritizerScore defines the score for each cluster
type PrioritizerScore map[string]int64

// Scheduler is an interface for scheduler, it returs the scheduler results
type Scheduler interface {
	Schedule(
		ctx context.Context,
		placement *clusterapiv1beta1.Placement,
		clusters []*clusterapiv1.ManagedCluster,
	) (ScheduleResult, *framework.Status)
}

type ScheduleResult interface {
	// FilterResults returns results for each filter
	FilterResults() []FilterResult

	// PrioritizerResults returns results for each prioritizer
	PrioritizerResults() []PrioritizerResult

	// PrioritizerScores returns total score for each cluster
	PrioritizerScores() PrioritizerScore

	// Decisions returns the decisions of the schedule
	Decisions() []clusterapiv1beta1.ClusterDecision

	// NumOfUnscheduled returns the number of unscheduled.
	NumOfUnscheduled() int

	// RequeueAfter returns the requeue time interval of the placement
	RequeueAfter() *time.Duration
}

type FilterResult struct {
	Name             string   `json:"name"`
	FilteredClusters []string `json:"filteredClusters"`
}

// PrioritizerResult defines the result of one prioritizer,
// include name, weight, and score of each cluster.
type PrioritizerResult struct {
	Name   string           `json:"name"`
	Weight int32            `json:"weight"`
	Scores PrioritizerScore `json:"scores"`
}

// ScheduleResult is the result for a certain schedule.
type scheduleResult struct {
	feasibleClusters     []*clusterapiv1.ManagedCluster
	scheduledDecisions   []clusterapiv1beta1.ClusterDecision
	unscheduledDecisions int

	filteredRecords map[string][]*clusterapiv1.ManagedCluster
	scoreRecords    []PrioritizerResult
	scoreSum        PrioritizerScore
	requeueAfter    *time.Duration
}

type schedulerHandler struct {
	recorder                kevents.EventRecorder
	placementDecisionLister clusterlisterv1beta1.PlacementDecisionLister
	scoreLister             clusterlisterv1alpha1.AddOnPlacementScoreLister
	clusterLister           clusterlisterv1.ManagedClusterLister
	clusterClient           clusterclient.Interface
}

func NewSchedulerHandler(
	clusterClient clusterclient.Interface, placementDecisionLister clusterlisterv1beta1.PlacementDecisionLister, scoreLister clusterlisterv1alpha1.AddOnPlacementScoreLister, clusterLister clusterlisterv1.ManagedClusterLister, recorder kevents.EventRecorder) plugins.Handle {

	return &schedulerHandler{
		recorder:                recorder,
		placementDecisionLister: placementDecisionLister,
		scoreLister:             scoreLister,
		clusterLister:           clusterLister,
		clusterClient:           clusterClient,
	}
}

func (s *schedulerHandler) EventRecorder() kevents.EventRecorder {
	return s.recorder
}

func (s *schedulerHandler) DecisionLister() clusterlisterv1beta1.PlacementDecisionLister {
	return s.placementDecisionLister
}

func (s *schedulerHandler) ScoreLister() clusterlisterv1alpha1.AddOnPlacementScoreLister {
	return s.scoreLister
}

func (s *schedulerHandler) ClusterLister() clusterlisterv1.ManagedClusterLister {
	return s.clusterLister
}

func (s *schedulerHandler) ClusterClient() clusterclient.Interface {
	return s.clusterClient
}

// Initialize the default prioritizer weight.
// Balane and Steady weight 1, others weight 0.
// The default weight can be replaced by each placement's PrioritizerConfigs.
var defaultPrioritizerConfig = map[clusterapiv1beta1.ScoreCoordinate]int32{
	{
		Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
		BuiltIn: PrioritizerBalance,
	}: 1,
	{
		Type:    clusterapiv1beta1.ScoreCoordinateTypeBuiltIn,
		BuiltIn: PrioritizerSteady,
	}: 1,
}

type pluginScheduler struct {
	handle             plugins.Handle
	filters            []plugins.Filter
	prioritizerWeights map[clusterapiv1beta1.ScoreCoordinate]int32
}

func NewPluginScheduler(handle plugins.Handle) *pluginScheduler {
	return &pluginScheduler{
		handle: handle,
		filters: []plugins.Filter{
			predicate.New(handle),
			tainttoleration.New(handle),
		},
		prioritizerWeights: defaultPrioritizerConfig,
	}
}

func (s *pluginScheduler) Schedule(
	ctx context.Context,
	placement *clusterapiv1beta1.Placement,
	clusters []*clusterapiv1.ManagedCluster,
) (ScheduleResult, *framework.Status) {
	filtered := clusters
	finalStatus := framework.NewStatus("", framework.Success, "")

	results := &scheduleResult{
		filteredRecords: map[string][]*clusterapiv1.ManagedCluster{},
		scoreRecords:    []PrioritizerResult{},
	}

	// filter clusters
	filterPipline := []string{}

	for _, f := range s.filters {
		filterResult, status := f.Filter(ctx, placement, filtered)
		filtered = filterResult.Filtered

		switch {
		case status.IsError():
			return results, status
		case status.Code() == framework.Warning:
			klog.Warningf("%v", status.Message())
			finalStatus = status
		}

		filterPipline = append(filterPipline, f.Name())

		results.filteredRecords[strings.Join(filterPipline, ",")] = filtered
	}

	// Prioritize clusters
	// 1. Get weight for each prioritizers.
	// For example, weights is {"Steady": 1, "Balance":1, "AddOn/default/ratio":3}.
	weights, status := getWeights(s.prioritizerWeights, placement)
	switch {
	case status.IsError():
		return results, status
	case status.Code() == framework.Warning:
		klog.Warningf("%v", status.Message())
		finalStatus = status
	}

	// 2. Generate prioritizers for each placement whose weight != 0.
	prioritizers, status := getPrioritizers(weights, s.handle)
	switch {
	case status.IsError():
		return results, status
	case status.Code() == framework.Warning:
		klog.Warningf("%v", status.Message())
		finalStatus = status
	}

	// 3. Calculate clusters scores.
	scoreSum := PrioritizerScore{}
	for _, cluster := range filtered {
		scoreSum[cluster.Name] = 0
	}
	for sc, p := range prioritizers {
		// Get cluster score.
		scoreResult, status := p.Score(ctx, placement, filtered)
		score := scoreResult.Scores

		switch {
		case status.IsError():
			return results, status
		case status.Code() == framework.Warning:
			klog.Warningf("%v", status.Message())
			finalStatus = status
		}

		// Record prioritizer score and weight
		weight := weights[sc]
		results.scoreRecords = append(results.scoreRecords, PrioritizerResult{Name: p.Name(), Weight: weight, Scores: score})

		// The final score is a sum of each prioritizer score * weight.
		// A higher weight indicates that the prioritizer weights more in the cluster selection,
		// while 0 weight indicate thats the prioritizer is disabled.
		for name, val := range score {
			scoreSum[name] = scoreSum[name] + val*int64(weight)
		}

	}

	// 4. Sort clusters by score, if score is equal, sort by name
	sort.SliceStable(filtered, func(i, j int) bool {
		if scoreSum[filtered[i].Name] == scoreSum[filtered[j].Name] {
			return filtered[i].Name < filtered[j].Name
		} else {
			return scoreSum[filtered[i].Name] > scoreSum[filtered[j].Name]
		}
	})

	results.feasibleClusters = filtered
	results.scoreSum = scoreSum

	// select clusters and generate cluster decisions
	decisions := selectClusters(placement, filtered)
	scheduled, unscheduled := len(decisions), 0
	if placement.Spec.NumberOfClusters != nil {
		unscheduled = int(*placement.Spec.NumberOfClusters) - scheduled
	}
	results.scheduledDecisions = decisions
	results.unscheduledDecisions = unscheduled

	// set placement requeue time
	for _, f := range s.filters {
		if r, _ := f.RequeueAfter(ctx, placement); r.RequeueTime != nil {
			newRequeueAfter := time.Until(*r.RequeueTime)
			results.requeueAfter = setRequeueAfter(results.requeueAfter, &newRequeueAfter)
		}
	}
	for _, p := range prioritizers {
		if r, _ := p.RequeueAfter(ctx, placement); r.RequeueTime != nil {
			newRequeueAfter := time.Until(*r.RequeueTime)
			results.requeueAfter = setRequeueAfter(results.requeueAfter, &newRequeueAfter)
		}
	}

	return results, finalStatus
}

// makeClusterDecisions selects clusters based on given cluster slice and then creates
// cluster decisions.
func selectClusters(placement *clusterapiv1beta1.Placement, clusters []*clusterapiv1.ManagedCluster) []clusterapiv1beta1.ClusterDecision {
	numOfDecisions := len(clusters)
	if placement.Spec.NumberOfClusters != nil {
		numOfDecisions = int(*placement.Spec.NumberOfClusters)
	}

	// truncate the cluster slice if the desired number of decisions is less than
	// the number of the candidate clusters
	if numOfDecisions < len(clusters) {
		clusters = clusters[:numOfDecisions]
	}

	decisions := []clusterapiv1beta1.ClusterDecision{}
	for _, cluster := range clusters {
		decisions = append(decisions, clusterapiv1beta1.ClusterDecision{
			ClusterName: cluster.Name,
		})
	}
	return decisions
}

// setRequeueAfter selects minimal time.Duration as requeue time
func setRequeueAfter(requeueAfter, newRequeueAfter *time.Duration) *time.Duration {
	if newRequeueAfter == nil {
		return requeueAfter
	}

	if requeueAfter == nil || *newRequeueAfter < *requeueAfter {
		return newRequeueAfter
	}

	return requeueAfter
}

// Get prioritizer weight for the placement.
// In Additive and "" mode, will override defaultWeight with what placement has defined and return.
// In Exact mode, will return the name and weight defined in placement.
func getWeights(defaultWeight map[clusterapiv1beta1.ScoreCoordinate]int32, placement *clusterapiv1beta1.Placement) (map[clusterapiv1beta1.ScoreCoordinate]int32, *framework.Status) {
	mode := placement.Spec.PrioritizerPolicy.Mode
	switch {
	case mode == clusterapiv1beta1.PrioritizerPolicyModeExact:
		return mergeWeights(nil, placement.Spec.PrioritizerPolicy.Configurations)
	case mode == clusterapiv1beta1.PrioritizerPolicyModeAdditive || mode == "":
		return mergeWeights(defaultWeight, placement.Spec.PrioritizerPolicy.Configurations)
	default:
		msg := fmt.Sprintf("incorrect prioritizer policy mode: %s", mode)
		return nil, framework.NewStatus("", framework.Misconfigured, msg)
	}
}

func mergeWeights(defaultWeight map[clusterapiv1beta1.ScoreCoordinate]int32, customizedWeight []clusterapiv1beta1.PrioritizerConfig) (map[clusterapiv1beta1.ScoreCoordinate]int32, *framework.Status) {
	weights := make(map[clusterapiv1beta1.ScoreCoordinate]int32)
	status := framework.NewStatus("", framework.Success, "")
	// copy the default weight
	for sc, w := range defaultWeight {
		weights[sc] = w
	}

	// override default weight
	for _, c := range customizedWeight {
		if c.ScoreCoordinate != nil {
			weights[*c.ScoreCoordinate] = c.Weight
		} else {
			return nil, framework.NewStatus("", framework.Misconfigured, "scoreCoordinate field is required")
		}
	}
	return weights, status
}

// Generate prioritizers for the placement.
func getPrioritizers(weights map[clusterapiv1beta1.ScoreCoordinate]int32, handle plugins.Handle) (map[clusterapiv1beta1.ScoreCoordinate]plugins.Prioritizer, *framework.Status) {
	result := make(map[clusterapiv1beta1.ScoreCoordinate]plugins.Prioritizer)
	status := framework.NewStatus("", framework.Success, "")
	for k, v := range weights {
		if v == 0 {
			continue
		}
		if k.Type == clusterapiv1beta1.ScoreCoordinateTypeBuiltIn {
			switch {
			case k.BuiltIn == PrioritizerBalance:
				result[k] = balance.New(handle)
			case k.BuiltIn == PrioritizerSteady:
				result[k] = steady.New(handle)
			case k.BuiltIn == PrioritizerResourceAllocatableCPU || k.BuiltIn == PrioritizerResourceAllocatableMemory:
				result[k] = resource.NewResourcePrioritizerBuilder(handle).WithPrioritizerName(k.BuiltIn).Build()
			default:
				msg := fmt.Sprintf("incorrect builtin prioritizer: %s", k.BuiltIn)
				return nil, framework.NewStatus("", framework.Misconfigured, msg)
			}
		} else {
			if k.AddOn == nil {
				return nil, framework.NewStatus("", framework.Misconfigured, "addOn should not be empty")
			}
			result[k] = addon.NewAddOnPrioritizerBuilder(handle).WithResourceName(k.AddOn.ResourceName).WithScoreName(k.AddOn.ScoreName).Build()
		}
	}
	return result, status
}

func (r *scheduleResult) FilterResults() []FilterResult {
	results := []FilterResult{}

	// order the FilterResults by key length
	filteredRecordsKey := []string{}
	for name := range r.filteredRecords {
		filteredRecordsKey = append(filteredRecordsKey, name)
	}
	sort.SliceStable(filteredRecordsKey, func(i, j int) bool {
		return len(filteredRecordsKey[i]) < len(filteredRecordsKey[j])
	})

	// go through the FilterResults by key length
	for _, name := range filteredRecordsKey {
		result := FilterResult{Name: name, FilteredClusters: []string{}}

		for _, c := range r.filteredRecords[name] {
			result.FilteredClusters = append(result.FilteredClusters, c.Name)
		}
		results = append(results, result)
	}
	return results
}

func (r *scheduleResult) PrioritizerResults() []PrioritizerResult {
	return r.scoreRecords
}

func (r *scheduleResult) PrioritizerScores() PrioritizerScore {
	return r.scoreSum
}

func (r *scheduleResult) Decisions() []clusterapiv1beta1.ClusterDecision {
	return r.scheduledDecisions
}

func (r *scheduleResult) NumOfUnscheduled() int {
	return r.unscheduledDecisions
}

func (r *scheduleResult) RequeueAfter() *time.Duration {
	return r.requeueAfter
}
