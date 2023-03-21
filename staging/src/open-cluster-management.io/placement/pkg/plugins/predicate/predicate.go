package predicate

import (
	"context"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/placement/pkg/controllers/framework"
	"open-cluster-management.io/placement/pkg/plugins"
)

var _ plugins.Filter = &Predicate{}

const description = "Predicate filter filters the clusters based on predicate defined in placement"

type Predicate struct{}

type predicateSelector struct {
	labelSelector labels.Selector
	claimSelector labels.Selector
}

func New(handle plugins.Handle) *Predicate {
	return &Predicate{}
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

	// prebuild label/claim selectors for each predicate
	predicateSelectors := []predicateSelector{}
	for _, predicate := range placement.Spec.Predicates {
		// build label selector
		labelSelector, err := convertLabelSelector(predicate.RequiredClusterSelector.LabelSelector)
		if err != nil {
			return plugins.PluginFilterResult{}, framework.NewStatus(
				p.Name(),
				framework.Misconfigured,
				err.Error(),
			)
		}
		// build claim selector
		claimSelector, err := convertClaimSelector(predicate.RequiredClusterSelector.ClaimSelector)
		if err != nil {
			return plugins.PluginFilterResult{}, framework.NewStatus(
				p.Name(),
				framework.Misconfigured,
				err.Error(),
			)
		}
		predicateSelectors = append(predicateSelectors, predicateSelector{
			labelSelector: labelSelector,
			claimSelector: claimSelector,
		})
	}

	// match cluster with selectors one by one
	matched := []*clusterapiv1.ManagedCluster{}
	for _, cluster := range clusters {
		claims := getClusterClaims(cluster)
		for _, ps := range predicateSelectors {
			// match with label selector
			if ok := ps.labelSelector.Matches(labels.Set(cluster.Labels)); !ok {
				continue
			}
			// match with claim selector
			if ok := ps.claimSelector.Matches(labels.Set(claims)); !ok {
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

// getClusterClaims returns a map containing cluster claims from the status of cluster
func getClusterClaims(cluster *clusterapiv1.ManagedCluster) map[string]string {
	claims := map[string]string{}
	for _, claim := range cluster.Status.ClusterClaims {
		claims[claim.Name] = claim.Value
	}
	return claims
}

// convertLabelSelector converts metav1.LabelSelector to labels.Selector
func convertLabelSelector(labelSelector metav1.LabelSelector) (labels.Selector, error) {
	selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
	if err != nil {
		return labels.Nothing(), err
	}

	return selector, nil
}

// convertClaimSelector converts ClusterClaimSelector to labels.Selector
func convertClaimSelector(clusterClaimSelector clusterapiv1beta1.ClusterClaimSelector) (labels.Selector, error) {
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: clusterClaimSelector.MatchExpressions,
	})
	if err != nil {
		return labels.Nothing(), err
	}

	return selector, nil
}
