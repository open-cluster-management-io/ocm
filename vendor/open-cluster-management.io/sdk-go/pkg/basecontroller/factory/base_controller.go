package factory

import (
	"context"
	"fmt"
	"sync"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

var defaultCacheSyncTimeout = 10 * time.Minute

// baseController represents generic Kubernetes controller boiler-plate
type baseController struct {
	name             string
	cachesToSync     []cache.InformerSynced
	sync             func(ctx context.Context, controllerContext SyncContext, key string) error
	syncContext      SyncContext
	resyncEvery      time.Duration
	cacheSyncTimeout time.Duration
}

var _ Controller = &baseController{}

func (c baseController) Name() string {
	return c.name
}

func waitForNamedCacheSync(ctx context.Context, controllerName string, stopCh <-chan struct{}, cacheSyncs ...cache.InformerSynced) error {
	logger := klog.FromContext(ctx)

	logger.Info("Waiting for caches to sync")

	if !cache.WaitForCacheSync(stopCh, cacheSyncs...) {
		return fmt.Errorf("unable to sync caches for %s", controllerName)
	}

	logger.Info("Caches are synced")

	return nil
}

func (c *baseController) SyncContext() SyncContext {
	return c.syncContext
}

func (c *baseController) Run(ctx context.Context, workers int) {
	logger := klog.FromContext(ctx).WithName(c.name)
	ctx = klog.NewContext(ctx, logger)

	// give caches 10 minutes to sync
	cacheSyncCtx, cacheSyncCancel := context.WithTimeout(ctx, c.cacheSyncTimeout)
	defer cacheSyncCancel()
	err := waitForNamedCacheSync(ctx, c.name, cacheSyncCtx.Done(), c.cachesToSync...)
	if err != nil {
		select {
		case <-ctx.Done():
			// Exit gracefully because the controller was requested to stop.
			return
		default:
			// If caches did not sync after 10 minutes, it has taken oddly long and
			// we should provide feedback. Since the control loops will never start,
			// it is safer to exit with a good message than to continue with a dead loop.
			// TODO: Consider making this behavior configurable.
			klog.Exit(err)
		}
	}

	var workerWg sync.WaitGroup
	defer func() {
		defer logger.Info("All workers have been terminated")
		workerWg.Wait()
	}()

	// queueContext is used to track and initiate queue shutdown
	queueContext, queueContextCancel := context.WithCancel(ctx)

	for i := 1; i <= workers; i++ {
		logger.Info("Starting worker of controller ...", "worker-ID", i)
		workerWg.Add(1)
		go func() {
			defer func() {
				logger.Info("Shutting down worker of controller ...")
				workerWg.Done()
			}()
			c.runWorker(queueContext)
		}()
	}

	// runPeriodicalResync is independent from queue
	if c.resyncEvery > 0 {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			c.runPeriodicalResync(ctx, c.resyncEvery)
		}()
	}

	// Handle controller shutdown

	<-ctx.Done()                     // wait for controller context to be cancelled
	c.syncContext.Queue().ShutDown() // shutdown the controller queue first
	queueContextCancel()             // cancel the queue context, which tell workers to initiate shutdown

	// Wait for all workers to finish their job.
	// at this point the Run() can hang and caller have to implement the logic that will kill
	// this controller (SIGKILL).
	logger.Info("Shutting down ...")
}

func (c *baseController) Sync(ctx context.Context, syncCtx SyncContext, key string) error {
	return c.sync(ctx, syncCtx, key)
}

func (c *baseController) runPeriodicalResync(ctx context.Context, interval time.Duration) {
	if interval == 0 {
		return
	}
	go wait.UntilWithContext(ctx, func(ctx context.Context) {
		c.syncContext.Queue().Add(DefaultQueueKey)
	}, interval)
}

// runWorker runs a single worker
// The worker is asked to terminate when the passed context is cancelled and is given terminationGraceDuration time
// to complete its shutdown.
func (c *baseController) runWorker(queueCtx context.Context) {
	wait.UntilWithContext(
		queueCtx,
		func(queueCtx context.Context) {
			for {
				select {
				case <-queueCtx.Done():
					return
				default:
					c.processNextWorkItem(queueCtx)
				}
			}
		},
		1*time.Second)
}

func (c *baseController) processNextWorkItem(queueCtx context.Context) {
	logger := klog.FromContext(queueCtx)

	key, quit := c.syncContext.Queue().Get()
	if quit {
		return
	}
	defer c.syncContext.Queue().Done(key)

	syncCtx := c.syncContext.(syncContext)
	queueKey := key

	if err := c.sync(queueCtx, syncCtx, queueKey); err != nil {
		if logger.V(4).Enabled() || key != DefaultQueueKey {
			utilruntime.HandleErrorWithContext(queueCtx, err, "controller failed to sync", "key", key)
		} else {
			utilruntime.HandleErrorWithContext(queueCtx, err, "reconciliation failed")
		}
		c.syncContext.Queue().AddRateLimited(key)
		return
	}

	c.syncContext.Queue().Forget(key)
}
