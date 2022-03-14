package testing

import (
	"testing"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	kevents "k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1alpha1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
)

type FakeSyncContext struct {
	queueKey string
	queue    workqueue.RateLimitingInterface
	recorder events.Recorder
}

func (f FakeSyncContext) Queue() workqueue.RateLimitingInterface { return f.queue }
func (f FakeSyncContext) QueueKey() string                       { return f.queueKey }
func (f FakeSyncContext) Recorder() events.Recorder              { return f.recorder }

func NewFakeSyncContext(t *testing.T, queueKey string) *FakeSyncContext {
	return &FakeSyncContext{
		queueKey: queueKey,
		queue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		recorder: eventstesting.NewTestingEventRecorder(t),
	}
}

type FakePluginHandle struct {
	recorder                kevents.EventRecorder
	placementDecisionLister clusterlisterv1beta1.PlacementDecisionLister
	scoreLister             clusterlisterv1alpha1.AddOnPlacementScoreLister
	clusterLister           clusterlisterv1.ManagedClusterLister
	client                  clusterclient.Interface
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

func NewFakePluginHandle(
	t *testing.T, client *clusterfake.Clientset, objects ...runtime.Object) *FakePluginHandle {
	informers := NewClusterInformerFactory(client, objects...)
	return &FakePluginHandle{
		recorder:                kevents.NewFakeRecorder(100),
		client:                  client,
		placementDecisionLister: informers.Cluster().V1beta1().PlacementDecisions().Lister(),
		scoreLister:             informers.Cluster().V1alpha1().AddOnPlacementScores().Lister(),
		clusterLister:           informers.Cluster().V1().ManagedClusters().Lister(),
	}
}

// AssertActions asserts the actual actions have the expected action verb
func AssertActions(t *testing.T, actualActions []clienttesting.Action, expectedVerbs ...string) {
	if len(actualActions) != len(expectedVerbs) {
		t.Fatalf("expected %d call but got: %#v", len(expectedVerbs), actualActions)
	}
	for i, expected := range expectedVerbs {
		if actualActions[i].GetVerb() != expected {
			t.Errorf("expected %s action but got: %#v", expected, actualActions[i])
		}
	}
}

// AssertNoActions asserts no actions are happened
func AssertNoActions(t *testing.T, actualActions []clienttesting.Action) {
	AssertActions(t, actualActions)
}

func HasCondition(conditions []metav1.Condition, expectedType, expectedReason string, expectedStatus metav1.ConditionStatus) bool {
	found := false
	for _, condition := range conditions {
		if condition.Type != expectedType {
			continue
		}
		found = true

		if condition.Status != expectedStatus {
			return false
		}

		if condition.Reason != expectedReason {
			return false
		}

		return true
	}

	return found
}
