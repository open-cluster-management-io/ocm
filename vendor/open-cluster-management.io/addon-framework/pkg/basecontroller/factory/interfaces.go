package factory

import (
	"context"

	"k8s.io/client-go/util/workqueue"
)

// Controller interface represents a runnable Kubernetes controller.
// Cancelling the syncContext passed will cause the controller to shutdown.
// Number of workers determine how much parallel the job processing should be.
type Controller interface {
	// Run runs the controller and blocks until the controller is finished.
	// Number of workers can be specified via workers parameter.
	// This function will return when all internal loops are finished.
	// Note that having more than one worker usually means handing parallelization of Sync().
	Run(ctx context.Context, workers int)

	// Sync contain the main controller logic.
	// This should not be called directly, but can be used in unit tests to exercise the sync.
	Sync(ctx context.Context, controllerContext SyncContext, key string) error

	// Name returns the controller name string.
	Name() string

	// SyncContext returns the SyncContext of this controller
	SyncContext() SyncContext
}

// SyncContext interface represents a context given to the Sync() function where the main controller logic happen.
// SyncContext exposes controller name and give user access to the queue (for manual requeue).
// SyncContext also provides metadata about object that informers observed as changed.
type SyncContext interface {
	// Queue gives access to controller queue. This can be used for manual requeue, although if a Sync() function return
	// an error, the object is automatically re-queued. Use with caution.
	Queue() workqueue.RateLimitingInterface
}

// SyncFunc is a function that contain main controller logic.
// The syncContext.syncContext passed is the main controller syncContext, when cancelled it means the controller is being shut down.
// The syncContext provides access to controller name, queue and event recorder.
type SyncFunc func(ctx context.Context, controllerContext SyncContext, key string) error
