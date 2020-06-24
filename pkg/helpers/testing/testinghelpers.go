package testing

import (
	"testing"
	"time"

	"k8s.io/client-go/util/workqueue"

	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FakeSyncContext struct {
	spokeName string
	recorder  events.Recorder
	queue     workqueue.RateLimitingInterface
}

func NewFakeSyncContext(t *testing.T, clusterName string) *FakeSyncContext {
	return &FakeSyncContext{
		spokeName: clusterName,
		recorder:  eventstesting.NewTestingEventRecorder(t),
		queue:     workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}
}

func (f FakeSyncContext) Queue() workqueue.RateLimitingInterface { return f.queue }
func (f FakeSyncContext) QueueKey() string                       { return f.spokeName }
func (f FakeSyncContext) Recorder() events.Recorder              { return f.recorder }

func NewManifestWork(namespace, name string, finalizers []string, deletionTimestamp *metav1.Time) *workapiv1.ManifestWork {
	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              name,
			Finalizers:        finalizers,
			DeletionTimestamp: deletionTimestamp,
		},
	}

	return work
}

func NewDeletionTimestamp(offset time.Duration) *metav1.Time {
	return &metav1.Time{
		Time: metav1.Now().Add(offset),
	}
}

func NewManagedCluster(name string, deletionTime *metav1.Time) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			DeletionTimestamp: deletionTime,
		},
	}
}
