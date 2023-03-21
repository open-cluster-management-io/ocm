package tainttoleration

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/placement/pkg/controllers/framework"
	"open-cluster-management.io/placement/pkg/plugins"
)

var _ plugins.Filter = &TaintToleration{}
var TolerationClock = (clock.Clock)(clock.RealClock{})

const (
	placementLabel = "cluster.open-cluster-management.io/placement"
	description    = "TaintToleration is a plugin that checks if a placement tolerates a managed cluster's taints"
)

type TaintToleration struct {
	handle plugins.Handle
}

func New(handle plugins.Handle) *TaintToleration {
	return &TaintToleration{
		handle: handle,
	}
}

func (p *TaintToleration) Name() string {
	return reflect.TypeOf(*p).Name()
}

func (pl *TaintToleration) Description() string {
	return description
}

func (pl *TaintToleration) Filter(ctx context.Context, placement *clusterapiv1beta1.Placement, clusters []*clusterapiv1.ManagedCluster) (plugins.PluginFilterResult, *framework.Status) {
	status := framework.NewStatus(pl.Name(), framework.Success, "")

	if len(clusters) == 0 {
		return plugins.PluginFilterResult{
			Filtered: clusters,
		}, status
	}

	// do validation on each toleration and return error if necessary
	for _, toleration := range placement.Spec.Tolerations {
		if len(toleration.Key) == 0 && toleration.Operator != clusterapiv1beta1.TolerationOpExists {
			return plugins.PluginFilterResult{}, framework.NewStatus(
				pl.Name(),
				framework.Misconfigured,
				"If the key is empty, operator must be Exists.",
			)
		}
		if toleration.Operator == clusterapiv1beta1.TolerationOpExists && len(toleration.Value) > 0 {
			return plugins.PluginFilterResult{}, framework.NewStatus(
				pl.Name(),
				framework.Misconfigured,
				"If the operator is Exists, the value should be empty.",
			)
		}
	}

	decisionClusterNames := getDecisionClusterNames(pl.handle, placement)

	// filter the clusters
	matched := []*clusterapiv1.ManagedCluster{}
	for _, cluster := range clusters {
		if tolerated, _, _ := isClusterTolerated(cluster, placement.Spec.Tolerations, decisionClusterNames.Has(cluster.Name)); tolerated {
			matched = append(matched, cluster)
		}
	}

	return plugins.PluginFilterResult{
		Filtered: matched,
	}, status
}

func (pl *TaintToleration) RequeueAfter(ctx context.Context, placement *clusterapiv1beta1.Placement) (plugins.PluginRequeueResult, *framework.Status) {
	status := framework.NewStatus(pl.Name(), framework.Success, "")
	// get exist decisions clusters
	decisionClusterNames, decisionClusters := getDecisionClusters(pl.handle, placement)
	if decisionClusterNames == nil || decisionClusters == nil {
		return plugins.PluginRequeueResult{}, status
	}

	var minRequeue *plugins.PluginRequeueResult
	// filter and record pluginRequeueResults
	for _, cluster := range decisionClusters {
		if tolerated, requeue, msg := isClusterTolerated(cluster, placement.Spec.Tolerations, decisionClusterNames.Has(cluster.Name)); tolerated {
			minRequeue = minRequeueTime(minRequeue, requeue)
		} else {
			status.AppendReason(msg)
		}
	}

	if minRequeue == nil {
		return plugins.PluginRequeueResult{}, status
	}

	return *minRequeue, status
}

// isClusterTolerated returns true if a cluster is tolerated by the given toleration array
func isClusterTolerated(cluster *clusterapiv1.ManagedCluster, tolerations []clusterapiv1beta1.Toleration, inDecision bool) (bool, *plugins.PluginRequeueResult, string) {
	var minRequeue *plugins.PluginRequeueResult
	for _, taint := range cluster.Spec.Taints {
		tolerated, requeue, message := isTaintTolerated(taint, tolerations, inDecision)
		if !tolerated {
			return false, nil, message
		}
		minRequeue = minRequeueTime(minRequeue, requeue)
	}

	return true, minRequeue, ""
}

// isTaintTolerated returns true if a taint is tolerated by the given toleration array
func isTaintTolerated(taint clusterapiv1.Taint, tolerations []clusterapiv1beta1.Toleration, inDecision bool) (bool, *plugins.PluginRequeueResult, string) {
	message := ""
	if taint.Effect == clusterapiv1.TaintEffectPreferNoSelect {
		return true, nil, message
	}

	if (taint.Effect == clusterapiv1.TaintEffectNoSelectIfNew) && inDecision {
		return true, nil, message
	}

	for _, toleration := range tolerations {
		if tolerated, requeue, msg := isTolerated(taint, toleration); tolerated {
			return true, requeue, msg
		} else {
			message = msg
		}
	}

	return false, nil, message
}

// isTolerated returns true if a taint is tolerated by the given toleration
func isTolerated(taint clusterapiv1.Taint, toleration clusterapiv1beta1.Toleration) (bool, *plugins.PluginRequeueResult, string) {
	if len(toleration.Effect) > 0 && toleration.Effect != taint.Effect {
		return false, nil, ""
	}

	if len(toleration.Key) > 0 && toleration.Key != taint.Key {
		return false, nil, ""
	}

	taintMatched := false
	switch toleration.Operator {
	// empty operator means Equal
	case "", clusterapiv1beta1.TolerationOpEqual:
		taintMatched = (toleration.Value == taint.Value)
	case clusterapiv1beta1.TolerationOpExists:
		taintMatched = true
	}

	if taintMatched {
		return isTolerationTimeExpired(taint, toleration)
	}

	return false, nil, ""

}

// isTolerationTimeExpired returns true if TolerationSeconds is nil or not expired
func isTolerationTimeExpired(taint clusterapiv1.Taint, toleration clusterapiv1beta1.Toleration) (bool, *plugins.PluginRequeueResult, string) {
	// TolerationSeconds is nil means it never expire
	if toleration.TolerationSeconds == nil {
		return true, nil, ""
	}

	requeueTime := taint.TimeAdded.Add(time.Duration(*toleration.TolerationSeconds) * time.Second)

	if TolerationClock.Now().Before(requeueTime) {
		message := fmt.Sprintf(
			"Cluster %s taint is added at %v, placement toleration seconds is %d",
			"clustername",
			taint.TimeAdded,
			*toleration.TolerationSeconds,
		)
		p := plugins.PluginRequeueResult{
			RequeueTime: &requeueTime,
		}
		return true, &p, message
	}

	return false, nil, ""
}

func getDecisionClusterNames(handle plugins.Handle, placement *clusterapiv1beta1.Placement) sets.String {
	existingDecisions := sets.String{}

	// query placementdecisions with label selector
	requirement, err := labels.NewRequirement(placementLabel, selection.Equals, []string{placement.Name})
	if err != nil {
		return existingDecisions
	}

	labelSelector := labels.NewSelector().Add(*requirement)
	decisions, err := handle.DecisionLister().PlacementDecisions(placement.Namespace).List(labelSelector)
	if err != nil {
		return existingDecisions
	}

	for _, decision := range decisions {
		for _, d := range decision.Status.Decisions {
			existingDecisions.Insert(d.ClusterName)
		}
	}

	return existingDecisions
}

func getDecisionClusters(handle plugins.Handle, placement *clusterapiv1beta1.Placement) (sets.String, []*clusterapiv1.ManagedCluster) {
	// get existing decision cluster name
	decisionClusterNames := getDecisionClusterNames(handle, placement)

	// get existing decision clusters
	decisionClusters := []*clusterapiv1.ManagedCluster{}
	for c := range decisionClusterNames {
		if managedCluser, err := handle.ClusterLister().Get(c); err != nil {
			klog.Warningf("Failed to get ManagedCluster: %s", err)
		} else {
			decisionClusters = append(decisionClusters, managedCluser)
		}
	}

	return decisionClusterNames, decisionClusters
}

// return the PluginRequeueResult with minimal requeue time
func minRequeueTime(x, y *plugins.PluginRequeueResult) *plugins.PluginRequeueResult {
	if x == nil {
		return y
	}
	if y == nil {
		return x
	}

	t1 := x.RequeueTime
	t2 := y.RequeueTime
	if t1 == nil || t2 == nil {
		return nil
	}
	if t1.Before(*t2) {
		return x
	} else {
		return y
	}
}
