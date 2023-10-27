package testing

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	kevents "k8s.io/client-go/tools/events"
	"k8s.io/utils/clock"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1alpha1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"

	"open-cluster-management.io/ocm/pkg/placement/controllers/metrics"
)

type FakePluginHandle struct {
	recorder                kevents.EventRecorder
	placementDecisionLister clusterlisterv1beta1.PlacementDecisionLister
	scoreLister             clusterlisterv1alpha1.AddOnPlacementScoreLister
	clusterLister           clusterlisterv1.ManagedClusterLister
	client                  clusterclient.Interface
	metricsRecorder         *metrics.ScheduleMetrics
}

func (f *FakePluginHandle) EventRecorder() kevents.EventRecorder { return f.recorder }
func (f *FakePluginHandle) DecisionLister() clusterlisterv1beta1.PlacementDecisionLister {
	return f.placementDecisionLister
}
func (f *FakePluginHandle) ScoreLister() clusterlisterv1alpha1.AddOnPlacementScoreLister {
	return f.scoreLister
}
func (f *FakePluginHandle) ClusterLister() clusterlisterv1.ManagedClusterLister {
	return f.clusterLister
}
func (f *FakePluginHandle) ClusterClient() clusterclient.Interface {
	return f.client
}
func (f *FakePluginHandle) MetricsRecorder() *metrics.ScheduleMetrics {
	return f.metricsRecorder
}

func NewFakePluginHandle(
	t *testing.T, client *clusterfake.Clientset, objects ...runtime.Object) *FakePluginHandle {
	informers := NewClusterInformerFactory(client, objects...)
	return &FakePluginHandle{
		recorder:                kevents.NewFakeRecorder(100),
		client:                  client,
		placementDecisionLister: informers.Cluster().V1beta1().PlacementDecisions().Lister(),
		scoreLister:             informers.Cluster().V1alpha1().AddOnPlacementScores().Lister(),
		clusterLister:           informers.Cluster().V1().ManagedClusters().Lister(),
		metricsRecorder:         metrics.NewScheduleMetrics(clock.RealClock{}),
	}
}
