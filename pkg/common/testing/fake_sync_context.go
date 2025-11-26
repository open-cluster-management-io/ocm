package testing

import (
	"testing"

	openshiftevents "github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/client-go/util/workqueue"

	sdkevents "open-cluster-management.io/sdk-go/pkg/basecontroller/events"
)

type FakeSyncContext struct {
	spokeName string
	recorder  openshiftevents.Recorder
	queue     workqueue.RateLimitingInterface
}

func (f FakeSyncContext) Queue() workqueue.RateLimitingInterface { return f.queue }
func (f FakeSyncContext) QueueKey() string                       { return f.spokeName }
func (f FakeSyncContext) Recorder() openshiftevents.Recorder     { return f.recorder }

func NewFakeSyncContext(t *testing.T, clusterName string) *FakeSyncContext {
	return &FakeSyncContext{
		spokeName: clusterName,
		recorder:  eventstesting.NewTestingEventRecorder(t),
		queue:     workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}
}

type FakeSDKSyncContext struct {
	spokeName string
	recorder  sdkevents.Recorder
	queue     workqueue.TypedRateLimitingInterface[string]
}

func (f FakeSDKSyncContext) Queue() workqueue.TypedRateLimitingInterface[string] { return f.queue }
func (f FakeSDKSyncContext) Recorder() sdkevents.Recorder {
	return f.recorder
}

func NewFakeSDKSyncContext(t *testing.T, clusterName string) *FakeSDKSyncContext {
	return &FakeSDKSyncContext{
		spokeName: clusterName,
		recorder:  sdkevents.NewContextualLoggingEventRecorder(t.Name()),
		queue:     workqueue.NewTypedRateLimitingQueue[string](workqueue.DefaultTypedControllerRateLimiter[string]()),
	}
}
