package handler

import (
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	workv1lister "open-cluster-management.io/api/client/work/listers/work/v1"

	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/common"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/watcher"
)

const ManifestWorkFinalizer = "cloudevents.open-cluster-management.io/manifest-work-cleanup"

type ManifestWorkSourceHandler struct {
	works   workqueue.RateLimitingInterface
	lister  workv1lister.ManifestWorkLister
	watcher *watcher.ManifestWorkWatcher
}

// NewManifestWorkSourceHandler returns a ResourceHandler for a ManifestWork source client. It sends the kube events
// with ManifestWorWatcher after CloudEventSourceClient received the ManifestWork status from agent, then the
// ManifestWorkInformer handles the kube events in its local cache.
func NewManifestWorkSourceHandler(lister workv1lister.ManifestWorkLister, watcher *watcher.ManifestWorkWatcher) *ManifestWorkSourceHandler {
	return &ManifestWorkSourceHandler{
		works:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "manifestwork-source-handler"),
		lister:  lister,
		watcher: watcher,
	}
}

func (h *ManifestWorkSourceHandler) Run(stopCh <-chan struct{}) {
	defer h.works.ShutDown()

	// start a goroutine to handle the works from the queue
	// the .Until will re-kick the runWorker one second after the runWorker completes
	go wait.Until(h.runWorker, time.Second, stopCh)

	// wait until we're told to stop
	<-stopCh
}

func (h *ManifestWorkSourceHandler) HandlerFunc() generic.ResourceHandler[*workv1.ManifestWork] {
	return func(action types.ResourceAction, obj *workv1.ManifestWork) error {
		switch action {
		case types.StatusModified:
			h.works.Add(obj)
		default:
			return fmt.Errorf("unsupported resource action %s", action)
		}
		return nil
	}
}

func (h *ManifestWorkSourceHandler) runWorker() {
	// hot loop until we're told to stop. processNextEvent will automatically wait until there's work available, so
	// we don't worry about secondary waits
	for h.processNextWork() {
	}
}

// processNextWork deals with one key off the queue.
func (h *ManifestWorkSourceHandler) processNextWork() bool {
	// pull the next event item from queue.
	// events queue blocks until it can return an item to be processed
	key, quit := h.works.Get()
	if quit {
		// the current queue is shutdown and becomes empty, quit this process
		return false
	}
	defer h.works.Done(key)

	if err := h.handleWork(key.(*workv1.ManifestWork)); err != nil {
		// we failed to handle the work, we should requeue the item to work on later
		// this method will add a backoff to avoid hotlooping on particular items
		h.works.AddRateLimited(key)
		return true
	}

	// we handle the event successfully, tell the queue to stop tracking history for this event
	h.works.Forget(key)
	return true
}

func (h *ManifestWorkSourceHandler) handleWork(work *workv1.ManifestWork) error {
	lastWork := h.getWorkByUID(work.UID)
	if lastWork == nil {
		// the work is not found, this may be the client is restarted and the local cache is not ready, requeue this
		// work
		return errors.NewNotFound(common.ManifestWorkGR, string(work.UID))
	}

	updatedWork := lastWork.DeepCopy()
	if meta.IsStatusConditionTrue(work.Status.Conditions, common.ManifestsDeleted) {
		updatedWork.Finalizers = []string{}
		h.watcher.Receive(watch.Event{Type: watch.Deleted, Object: updatedWork})
		return nil
	}

	resourceVersion, err := strconv.Atoi(work.ResourceVersion)
	if err != nil {
		klog.Errorf("invalid resource version for work %s/%s, %v", lastWork.Namespace, lastWork.Name, err)
		return nil
	}

	if int64(resourceVersion) > lastWork.Generation {
		klog.Warningf("the work %s/%s resource version %d is great than its generation %d, ignore",
			lastWork.Namespace, lastWork.Name, resourceVersion, work.Generation)
		return nil
	}

	// no status change
	if equality.Semantic.DeepEqual(lastWork.Status, work.Status) {
		return nil
	}

	// the work has been handled by agent, we ensure a finalizer on the work
	updatedWork.Finalizers = ensureFinalizers(updatedWork.Finalizers)
	updatedWork.Status = work.Status
	h.watcher.Receive(watch.Event{Type: watch.Modified, Object: updatedWork})
	return nil
}

func (h *ManifestWorkSourceHandler) getWorkByUID(uid kubetypes.UID) *workv1.ManifestWork {
	works, err := h.lister.List(labels.Everything())
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
