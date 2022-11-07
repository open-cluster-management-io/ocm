package balance

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/labels"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/placement/pkg/controllers/framework"
	"open-cluster-management.io/placement/pkg/plugins"
)

const (
	placementLabel = "cluster.open-cluster-management.io/placement"
	description    = `
	Balance prioritizer balance the number of decisions among the clusters. The cluster
	with the highest number of decison is given the lowest score, while the empty cluster is given
	the highest score.
	`
)

var _ plugins.Prioritizer = &Balance{}

type Balance struct {
	handle plugins.Handle
}

func New(handle plugins.Handle) *Balance {
	return &Balance{
		handle: handle,
	}
}

func (b *Balance) Name() string {
	return reflect.TypeOf(*b).Name()
}

func (b *Balance) Description() string {
	return description
}

func (b *Balance) Score(ctx context.Context, placement *clusterapiv1beta1.Placement, clusters []*clusterapiv1.ManagedCluster) (plugins.PluginScoreResult, *framework.Status) {
	scores := map[string]int64{}
	for _, cluster := range clusters {
		scores[cluster.Name] = plugins.MaxClusterScore
	}

	decisions, err := b.handle.DecisionLister().List(labels.Everything())
	if err != nil {
		return plugins.PluginScoreResult{}, framework.NewStatus(
			b.Name(),
			framework.Error,
			err.Error(),
		)
	}

	var maxCount int64
	decisionCount := map[string]int64{}
	for _, decision := range decisions {
		// Do not count the decision that is being scheduled.
		if decision.Labels[placementLabel] == placement.Name && decision.Namespace == placement.Namespace {
			continue
		}
		for _, d := range decision.Status.Decisions {
			decisionCount[d.ClusterName] = decisionCount[d.ClusterName] + 1
			if decisionCount[d.ClusterName] > maxCount {
				maxCount = decisionCount[d.ClusterName]
			}
		}
	}

	for clusterName := range scores {
		if count, ok := decisionCount[clusterName]; ok {
			usage := float64(count) / float64(maxCount)

			// Negate the usage and substracted by 0.5, then we double it and muliply by maxCount,
			// which normalize the score to value between 100 and -100
			scores[clusterName] = 2 * int64(float64(plugins.MaxClusterScore)*(0.5-usage))
		}
	}

	return plugins.PluginScoreResult{
		Scores: scores,
	}, framework.NewStatus(b.Name(), framework.Success, "")
}

func (b *Balance) RequeueAfter(ctx context.Context, placement *clusterapiv1beta1.Placement) (plugins.PluginRequeueResult, *framework.Status) {
	return plugins.PluginRequeueResult{}, framework.NewStatus(b.Name(), framework.Success, "")
}
