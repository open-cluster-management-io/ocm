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

func waitForNamedCacheSync(controllerName string, stopCh <-chan struct{}, cacheSyncs ...cache.InformerSynced) error {
	klog.Infof("Waiting for caches to sync for %s", controllerName)

	if !cache.WaitForCacheSync(stopCh, cacheSyncs...) {
		return fmt.Errorf("unable to sync caches for %s", controllerName)
	}

	klog.Infof("Caches are synced for %s ", controllerName)

	return nil
}

func (c *baseController) SyncContext() SyncContext {
	return c.syncContext
}

func (c *baseController) Run(ctx context.Context, workers int) {
	// give caches 10 minutes to sync
	cacheSyncCtx, cacheSyncCancel := context.WithTimeout(ctx, c.cacheSyncTimeout)
	defer cacheSyncCancel()
	err := waitForNamedCacheSync(c.name, cacheSyncCtx.Done(), c.cachesToSync...)
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
		defer klog.Infof("All %s workers have been terminated", c.name)
		workerWg.Wait()
	}()

	// queueContext is used to track and initiate queue shutdown
	queueContext, queueContextCancel := context.WithCancel(context.TODO())

	for i := 1; i <= workers; i++ {
		klog.Infof("Starting #%d worker of %s controller ...", i, c.name)
		workerWg.Add(1)
		go func() {
			defer func() {
				klog.Infof("Shutting down worker of %s controller ...", c.name)
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
	klog.Infof("Shutting down %s ...", c.name)
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
	key, quit := c.syncContext.Queue().Get()
	if quit {
		return
	}
	defer c.syncContext.Queue().Done(key)

	syncCtx := c.syncContext.(syncContext)
	var ok bool
	queueKey, ok := key.(string)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("%q controller failed to process key %q (not a string)", c.name, key))
		return
	}

	if err := c.sync(queueCtx, syncCtx, queueKey); err != nil {
		if klog.V(4).Enabled() || key != "key" {
			utilruntime.HandleError(fmt.Errorf("%q controller failed to sync %q, err: %w", c.name, key, err))
		} else {
			utilruntime.HandleError(fmt.Errorf("%s reconciliation failed: %w", c.name, err))
		}
		c.syncContext.Queue().AddRateLimited(key)
		return
	}

	c.syncContext.Queue().Forget(key)
}
