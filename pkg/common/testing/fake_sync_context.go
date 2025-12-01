package testing

import (
	"testing"

	"k8s.io/client-go/util/workqueue"

	sdkevents "open-cluster-management.io/sdk-go/pkg/basecontroller/events"
)

type FakeSyncContext struct {
	spokeName string
	recorder  sdkevents.Recorder
	queue     workqueue.TypedRateLimitingInterface[string]
}

func (f FakeSyncContext) Queue() workqueue.TypedRateLimitingInterface[string] { return f.queue }
func (f FakeSyncContext) Recorder() sdkevents.Recorder {
	return f.recorder
}

func NewFakeSyncContext(t *testing.T, clusterName string) *FakeSyncContext {
	return &FakeSyncContext{
		spokeName: clusterName,
		recorder:  sdkevents.NewContextualLoggingEventRecorder(t.Name()),
		queue:     workqueue.NewTypedRateLimitingQueue[string](workqueue.DefaultTypedControllerRateLimiter[string]()),
	}
}
