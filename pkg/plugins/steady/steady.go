package steady

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/placement/pkg/controllers/framework"
	"open-cluster-management.io/placement/pkg/plugins"
)

const (
	placementLabel = "cluster.open-cluster-management.io/placement"
	description    = `
	Steady prioritizer ensure the existing decision is stabilized. The clusters that existing decisions
	choose are given the highest score while the clusters with no existing decisions are given the lowest
	score.
	`
)

var _ plugins.Prioritizer = &Steady{}

type Steady struct {
	handle plugins.Handle
}

func New(handle plugins.Handle) *Steady {
	return &Steady{
		handle: handle,
	}
}

func (s *Steady) Name() string {
	return reflect.TypeOf(*s).Name()
}

func (s *Steady) Description() string {
	return description
}

func (s *Steady) Score(
	ctx context.Context, placement *clusterapiv1beta1.Placement, clusters []*clusterapiv1.ManagedCluster) (plugins.PluginScoreResult, *framework.Status) {
	// query placementdecisions with label selector
	scores := map[string]int64{}
	requirement, err := labels.NewRequirement(placementLabel, selection.Equals, []string{placement.Name})

	if err != nil {
		return plugins.PluginScoreResult{}, framework.NewStatus(
			s.Name(),
			framework.Error,
			err.Error(),
		)
	}

	labelSelector := labels.NewSelector().Add(*requirement)
	decisions, err := s.handle.DecisionLister().PlacementDecisions(placement.Namespace).List(labelSelector)

	if err != nil {
		return plugins.PluginScoreResult{}, framework.NewStatus(
			s.Name(),
			framework.Error,
			err.Error(),
		)
	}

	existingDecisions := sets.String{}
	for _, decision := range decisions {
		for _, d := range decision.Status.Decisions {
			existingDecisions.Insert(d.ClusterName)
		}
	}

	for _, cluster := range clusters {
		if existingDecisions.Has(cluster.Name) {
			scores[cluster.Name] = plugins.MaxClusterScore
		} else {
			scores[cluster.Name] = 0
		}
	}

	return plugins.PluginScoreResult{
		Scores: scores,
	}, framework.NewStatus(s.Name(), framework.Success, "")
}

func (s *Steady) RequeueAfter(ctx context.Context, placement *clusterapiv1beta1.Placement) (plugins.PluginRequeueResult, *framework.Status) {
	return plugins.PluginRequeueResult{}, framework.NewStatus(s.Name(), framework.Success, "")
}
