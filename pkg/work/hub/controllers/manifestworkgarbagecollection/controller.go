package manifestworkgarbagecollection

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklisters "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

// ManifestWorkGarbageCollectionController is to delete the manifestworks when it has the completed condition.
type ManifestWorkGarbageCollectionController struct {
	workClient workclientset.Interface
	workLister worklisters.ManifestWorkLister
}

// NewManifestWorkGarbageCollectionController creates a new ManifestWorkGarbageCollectionController
func NewManifestWorkGarbageCollectionController(
	recorder events.Recorder,
	workClient workclientset.Interface,
	manifestWorkInformer workinformers.ManifestWorkInformer,
) factory.Controller {
	controller := &ManifestWorkGarbageCollectionController{
		workClient: workClient,
		workLister: manifestWorkInformer.Lister(),
	}

	return factory.New().
		WithInformersQueueKeysFunc(
			queue.QueueKeyByMetaNamespaceName,
			manifestWorkInformer.Informer(),
		).
		WithSync(controller.sync).
		ToController("ManifestWorkGarbageCollectionController", recorder)
}

// sync is the main reconcile loop for completed ManifestWork TTL
func (c *ManifestWorkGarbageCollectionController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	key := controllerContext.QueueKey()
	logger := klog.FromContext(ctx)
	logger.V(4).Info("Reconciling ManifestWork for TTL processing", "key", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(err)
		return nil
	}

	manifestWork, err := c.workLister.ManifestWorks(namespace).Get(name)
	switch {
	case apierrors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	if manifestWork.DeletionTimestamp != nil {
		return nil
	}

	// Check if ManifestWork has TTLSecondsAfterFinished configured
	if manifestWork.Spec.DeleteOption == nil || manifestWork.Spec.DeleteOption.TTLSecondsAfterFinished == nil {
		return nil
	}

	// Find the Complete condition
	completedCondition := meta.FindStatusCondition(manifestWork.Status.Conditions, workapiv1.WorkComplete)
	if completedCondition == nil || completedCondition.Status != metav1.ConditionTrue {
		return nil
	}

	ttlSeconds := *manifestWork.Spec.DeleteOption.TTLSecondsAfterFinished
	if ttlSeconds > 0 {
		// Calculate time elapsed since completion
		// Compute deadline precisely using durations and handle clock skew.
		completedTime := completedCondition.LastTransitionTime.Time
		ttl := time.Duration(ttlSeconds) * time.Second
		deadline := completedTime.Add(ttl)
		now := time.Now()
		if now.Before(deadline) {
			requeueAfter := time.Until(deadline)
			logger.V(4).Info("ManifestWork completed; will be deleted after remaining TTL",
				"namespace", namespace, "name", name, "remaining", requeueAfter)
			controllerContext.Queue().AddAfter(key, requeueAfter)
			return nil
		}
	}

	// Time to delete the ManifestWork
	logger.Info("Deleting completed ManifestWork after TTL expiry",
		"namespace", namespace, "name", name, "ttlSeconds", ttlSeconds)
	err = c.workClient.WorkV1().ManifestWorks(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete completed ManifestWork %s/%s: %w", namespace, name, err)
	}

	return nil
}
