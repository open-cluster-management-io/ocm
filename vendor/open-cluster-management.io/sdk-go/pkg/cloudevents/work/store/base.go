package store

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/common"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/utils"
)

const ManifestWorkFinalizer = "cloudevents.open-cluster-management.io/manifest-work-cleanup"

type baseStore struct {
	sync.RWMutex

	result chan watch.Event
	done   chan struct{}

	store cache.Store

	initiated bool

	// a queue to save the received work events
	receivedWorks workqueue.RateLimitingInterface
}

// ResultChan implements watch interface.
func (b *baseStore) ResultChan() <-chan watch.Event {
	return b.result
}

// Stop implements watch interface.
func (b *baseStore) Stop() {
	// Call Close() exactly once by locking and setting a flag.
	b.Lock()
	defer b.Unlock()

	// closing a closed channel always panics, therefore check before closing
	select {
	case <-b.done:
		close(b.result)
	default:
		close(b.done)
	}
}

// List the works from the store with the list options
func (b *baseStore) List(opts metav1.ListOptions) ([]*workv1.ManifestWork, error) {
	b.RLock()
	defer b.RUnlock()

	return utils.ListWorksWithOptions(b.store, opts)
}

// Get a works from the store
func (b *baseStore) Get(namespace, name string) (*workv1.ManifestWork, error) {
	b.RLock()
	defer b.RUnlock()

	obj, exists, err := b.store.GetByKey(fmt.Sprintf("%s/%s", namespace, name))
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, errors.NewNotFound(common.ManifestWorkGR, name)
	}

	work, ok := obj.(*workv1.ManifestWork)
	if !ok {
		return nil, fmt.Errorf("unknown type %T", obj)
	}

	return work, nil
}

// List all of works from the store
func (b *baseStore) ListAll() ([]*workv1.ManifestWork, error) {
	b.RLock()
	defer b.RUnlock()

	works := []*workv1.ManifestWork{}
	for _, obj := range b.store.List() {
		if work, ok := obj.(*workv1.ManifestWork); ok {
			works = append(works, work)
		}
	}

	return works, nil
}

func (b *baseStore) HandleReceivedWork(action types.ResourceAction, work *workv1.ManifestWork) error {
	switch action {
	case types.StatusModified:
		b.receivedWorks.Add(work)
	default:
		return fmt.Errorf("unsupported resource action %s", action)
	}
	return nil
}

// workProcessor process the received works from given work queue with a specific store
type workProcessor struct {
	works workqueue.RateLimitingInterface
	store WorkClientWatcherStore
}

func newWorkProcessor(works workqueue.RateLimitingInterface, store WorkClientWatcherStore) *workProcessor {
	return &workProcessor{
		works: works,
		store: store,
	}
}

func (b *workProcessor) run(stopCh <-chan struct{}) {
	defer b.works.ShutDown()

	// start a goroutine to handle the works from the queue
	// the .Until will re-kick the runWorker one second after the runWorker completes
	go wait.Until(b.runWorker, time.Second, stopCh)

	// wait until we're told to stop
	<-stopCh
}

func (b *workProcessor) runWorker() {
	// hot loop until we're told to stop. processNextEvent will automatically wait until there's work available, so
	// we don't worry about secondary waits
	for b.processNextWork() {
	}
}

// processNextWork deals with one key off the queue.
func (b *workProcessor) processNextWork() bool {
	// pull the next event item from queue.
	// events queue blocks until it can return an item to be processed
	key, quit := b.works.Get()
	if quit {
		// the current queue is shutdown and becomes empty, quit this process
		return false
	}
	defer b.works.Done(key)

	if err := b.handleWork(key.(*workv1.ManifestWork)); err != nil {
		// we failed to handle the work, we should requeue the item to work on later
		// this method will add a backoff to avoid hotlooping on particular items
		b.works.AddRateLimited(key)
		return true
	}

	// we handle the event successfully, tell the queue to stop tracking history for this event
	b.works.Forget(key)
	return true
}

func (b *workProcessor) handleWork(work *workv1.ManifestWork) error {
	lastWork := b.getWork(work.UID)
	if lastWork == nil {
		// the work is not found from the local cache and it has been deleted by the agent,
		// ignore this work.
		if meta.IsStatusConditionTrue(work.Status.Conditions, common.ManifestsDeleted) {
			return nil
		}

		// the work is not found, there are two cases:
		// 1) the source is restarted and the local cache is not ready, requeue this work.
		// 2) (TODO) during the source restart, the work is deleted forcibly, we may need an
		//    eviction mechanism for this.
		return errors.NewNotFound(common.ManifestWorkGR, string(work.UID))
	}

	updatedWork := lastWork.DeepCopy()
	if meta.IsStatusConditionTrue(work.Status.Conditions, common.ManifestsDeleted) {
		updatedWork.Finalizers = []string{}
		// delete the work from the local cache.
		return b.store.Delete(updatedWork)
	}

	lastResourceVersion, err := strconv.Atoi(lastWork.ResourceVersion)
	if err != nil {
		klog.Errorf("invalid resource version for work %s/%s, %v", lastWork.Namespace, lastWork.Name, err)
		return nil
	}

	resourceVersion, err := strconv.Atoi(work.ResourceVersion)
	if err != nil {
		klog.Errorf("invalid resource version for work %s/%s, %v", lastWork.Namespace, lastWork.Name, err)
		return nil
	}

	// the current work's version is maintained on source and the agent's work is newer than source, ignore
	if lastResourceVersion != 0 && resourceVersion > lastResourceVersion {
		klog.Warningf("the work %s/%s resource version %d is great than its generation %d, ignore",
			lastWork.Namespace, lastWork.Name, resourceVersion, lastResourceVersion)
		return nil
	}

	// no status change
	if equality.Semantic.DeepEqual(lastWork.Status, work.Status) {
		return nil
	}

	// the work has been handled by agent, we ensure a finalizer on the work
	updatedWork.Finalizers = ensureFinalizers(updatedWork.Finalizers)
	updatedWork.Status = work.Status
	// update the work with status in the local cache.
	return b.store.Update(updatedWork)
}

func (b *workProcessor) getWork(uid kubetypes.UID) *workv1.ManifestWork {
	works, err := b.store.ListAll()
	if err != nil {
		klog.Errorf("failed to lists works, %v", err)
		return nil
	}

	for _, work := range works {
		if work.UID == uid {
			return work
		}
	}

	return nil
}

func ensureFinalizers(workFinalizers []string) []string {
	has := false
	for _, f := range workFinalizers {
		if f == ManifestWorkFinalizer {
			has = true
			break
		}
	}

	if !has {
		workFinalizers = append(workFinalizers, ManifestWorkFinalizer)
	}

	return workFinalizers
}
