package testing

import (
	"testing"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/client-go/util/workqueue"
)

type FakeSyncContext struct {
	spokeName string
	recorder  events.Recorder
	queue     workqueue.RateLimitingInterface
}

func (f FakeSyncContext) Queue() workqueue.RateLimitingInterface { return f.queue }
func (f FakeSyncContext) QueueKey() string                       { return f.spokeName }
func (f FakeSyncContext) Recorder() events.Recorder              { return f.recorder }

func NewFakeSyncContext(t *testing.T, clusterName string) *FakeSyncContext {
	return &FakeSyncContext{
		spokeName: clusterName,
		recorder:  eventstesting.NewTestingEventRecorder(t),
		queue:     workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}
}

type FakeSDKSyncContext struct {
	spokeName string
	recorder  events.Recorder
	queue     workqueue.TypedRateLimitingInterface[string]
}

func (f FakeSDKSyncContext) Queue() workqueue.TypedRateLimitingInterface[string] { return f.queue }

func NewFakeSDKSyncContext(t *testing.T, clusterName string) *FakeSDKSyncContext {
	return &FakeSDKSyncContext{
		spokeName: clusterName,
		recorder:  eventstesting.NewTestingEventRecorder(t),
		queue:     workqueue.NewTypedRateLimitingQueue[string](workqueue.DefaultTypedControllerRateLimiter[string]()),
	}
}
