package predicate

import (
	"context"
	"reflect"

	"k8s.io/klog/v2"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	"open-cluster-management.io/ocm/pkg/placement/controllers/framework"
	"open-cluster-management.io/ocm/pkg/placement/helpers"
	"open-cluster-management.io/ocm/pkg/placement/plugins"
)

var _ plugins.Filter = &Predicate{}

const description = "Predicate filter filters the clusters based on predicate defined in placement"

type Predicate struct {
	handle plugins.Handle
}

func New(handle plugins.Handle) *Predicate {
	return &Predicate{handle: handle}
}

func (p *Predicate) Name() string {
	return reflect.TypeOf(*p).Name()
}

func (p *Predicate) Description() string {
	return description
}

func (p *Predicate) Filter(
	ctx context.Context, placement *clusterapiv1beta1.Placement, clusters []*clusterapiv1.ManagedCluster) (plugins.PluginFilterResult, *framework.Status) {
	status := framework.NewStatus(p.Name(), framework.Success, "")

	if len(placement.Spec.Predicates) == 0 {
		return plugins.PluginFilterResult{
			Filtered: clusters,
		}, status
	}
	if len(clusters) == 0 {
		return plugins.PluginFilterResult{
			Filtered: clusters,
		}, status
	}

	env, err := helpers.NewEnv(p.handle.ScoreLister())
	if err != nil {
		klog.Error(err)
		return plugins.PluginFilterResult{}, framework.NewStatus(
			p.Name(),
			framework.Error,
			err.Error(),
		)
	}

	// prebuild label/claim selectors for each predicate
	clusterSelectors := []*helpers.ClusterSelector{}
	for _, predicate := range placement.Spec.Predicates {
		clusterSelector, err := helpers.NewClusterSelector(predicate.RequiredClusterSelector, env)
		if err != nil {
			return plugins.PluginFilterResult{}, framework.NewStatus(
				p.Name(),
				framework.Misconfigured,
				err.Error(),
			)
		}
		compilationResults := clusterSelector.Compile()
		for _, compiled := range compilationResults {
			if compiled.Error != nil {
				return plugins.PluginFilterResult{}, framework.NewStatus(
					p.Name(),
					framework.Misconfigured,
					compiled.Error.Error(),
				)
			}
		}
		clusterSelectors = append(clusterSelectors, clusterSelector)
	}

	// match cluster with selectors one by one
	matched := []*clusterapiv1.ManagedCluster{}
	for _, cluster := range clusters {
		for _, cs := range clusterSelectors {
			if ok := cs.Matches(cluster); !ok {
				continue
			}
			matched = append(matched, cluster)
			break
		}
	}

	return plugins.PluginFilterResult{
		Filtered: matched,
	}, status
}

func (p *Predicate) RequeueAfter(ctx context.Context, placement *clusterapiv1beta1.Placement) (plugins.PluginRequeueResult, *framework.Status) {
	return plugins.PluginRequeueResult{}, framework.NewStatus(p.Name(), framework.Success, "")
}
