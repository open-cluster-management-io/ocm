package random

import (
	"context"
	"math/rand"
	"reflect"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	"open-cluster-management.io/ocm/pkg/placement/controllers/framework"
	"open-cluster-management.io/ocm/pkg/placement/plugins"
)

const (
	description = `
	Random prioritizer assigns random scores to clusters. Each cluster is given a random score between
	MinClusterScore and MaxClusterScore, providing random cluster selection for testing or load
	distribution scenarios.
	`
)

var _ plugins.Prioritizer = &Random{}

type Random struct {
	handle plugins.Handle
}

func New(handle plugins.Handle) *Random {
	return &Random{
		handle: handle,
	}
}

func (r *Random) Name() string {
	return reflect.TypeFor[Random]().Name()
}

func (r *Random) Description() string {
	return description
}

func (r *Random) Score(ctx context.Context, placement *clusterapiv1beta1.Placement,
	clusters []*clusterapiv1.ManagedCluster) (plugins.PluginScoreResult, *framework.Status) {
	scores := map[string]int64{}

	scoreRange := plugins.MaxClusterScore - plugins.MinClusterScore + 1

	for _, cluster := range clusters {
		// Generate a random score between MinClusterScore (-100) and MaxClusterScore (100)
		scores[cluster.Name] = rand.Int63n(scoreRange) + plugins.MinClusterScore
	}

	return plugins.PluginScoreResult{
		Scores: scores,
	}, framework.NewStatus(r.Name(), framework.Success, "")
}

func (r *Random) RequeueAfter(ctx context.Context, placement *clusterapiv1beta1.Placement) (plugins.PluginRequeueResult, *framework.Status) {
	return plugins.PluginRequeueResult{}, framework.NewStatus(r.Name(), framework.Success, "")
}
