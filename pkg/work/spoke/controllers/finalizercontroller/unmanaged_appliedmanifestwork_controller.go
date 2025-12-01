package finalizercontroller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/work/helper"
)

const (
	// the upper bound of the AppliedManifestWork eviction grace period
	// If the AppliedManifestWork eviction grace period is set with a value that is larger than or equal to
	// the bound, the eviction feature will be disabled.
	EvictionGracePeriodBound = 100 * 365 * 24 * time.Hour

	unManagedAppliedManifestWork = "UnManagedAppliedManifestWork"
)

type unmanagedAppliedWorkController struct {
	manifestWorkLister        worklister.ManifestWorkNamespaceLister
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	patcher                   patcher.Patcher[*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus]
	appliedManifestWorkLister worklister.AppliedManifestWorkLister
	hubHash                   string
	agentID                   string
	evictionGracePeriod       time.Duration
	rateLimiter               workqueue.RateLimiter
}

// NewUnManagedAppliedWorkController returns a controller to evict the unmanaged appliedmanifestworks.
//
// An appliedmanifestwork will be considered unmanaged in the following scenarios:
//   - the manifestwork of the current appliedmanifestwork is missing on the hub, or
//   - the appliedmanifestwork hub hash does not match the current hub hash of the work agent.
//
// One unmanaged appliedmanifestwork will be evicted from the managed cluster after a grace period (by
// default, 60 minutes), after one appliedmanifestwork is evicted from the managed cluster, its owned
// resources will also be evicted from the managed cluster with Kubernetes garbage collection.
func NewUnManagedAppliedWorkController(
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	appliedManifestWorkInformer workinformer.AppliedManifestWorkInformer,
	evictionGracePeriod time.Duration,
	hubHash, agentID string,
) factory.Controller {
	controller := &unmanagedAppliedWorkController{
		manifestWorkLister:        manifestWorkLister,
		appliedManifestWorkClient: appliedManifestWorkClient,
		patcher: patcher.NewPatcher[
			*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus](
			appliedManifestWorkClient),
		appliedManifestWorkLister: appliedManifestWorkInformer.Lister(),
		hubHash:                   hubHash,
		agentID:                   agentID,
		evictionGracePeriod:       evictionGracePeriod,
		rateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(1*time.Minute, evictionGracePeriod),
	}

	return factory.New().
		WithInformersQueueKeysFunc(func(obj runtime.Object) []string {
			accessor, _ := meta.Accessor(obj)
			return []string{fmt.Sprintf("%s-%s", hubHash, accessor.GetName())}
		}, manifestWorkInformer.Informer()).
		WithFilteredEventsInformersQueueKeysFunc(
			queue.QueueKeyByMetaName,
			helper.AppliedManifestworkAgentIDFilter(agentID), appliedManifestWorkInformer.Informer()).
		WithSync(controller.sync).ToController(unManagedAppliedManifestWork)
}

func (m *unmanagedAppliedWorkController) sync(ctx context.Context, controllerContext factory.SyncContext, appliedManifestWorkName string) error {
	logger := klog.FromContext(ctx).WithName(appliedManifestWorkFinalizer).
		WithValues("appliedManifestWorkName", appliedManifestWorkName)
	ctx = klog.NewContext(ctx, logger)

	logger.V(5).Info("Reconciling AppliedManifestWork")

	appliedManifestWork, err := m.appliedManifestWorkLister.Get(appliedManifestWorkName)
	if errors.IsNotFound(err) {
		// appliedmanifestwork not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	// skip if appliedManifestWork is already in deleting state
	if appliedManifestWork.DeletionTimestamp != nil {
		return nil
	}

	// Do NOT evict the AppliedManifestWorks if the eviction grace period reaches or exceeds the
	// EvictionGracePeriodBound indicating the eviction is disabled for all the appliedmanifestworks.
	if m.evictionGracePeriod >= EvictionGracePeriodBound {
		return m.stopToEvictAppliedManifestWork(ctx, appliedManifestWork)
	}

	_, err = m.manifestWorkLister.Get(appliedManifestWork.Spec.ManifestWorkName)
	if errors.IsNotFound(err) {
		// evict the current appliedmanifestwork when its relating manifestwork is missing on the hub
		return m.evictAppliedManifestWork(ctx, controllerContext, appliedManifestWork)
	}
	if err != nil {
		return err
	}

	// manifestwork exists but hub changed
	if !strings.HasPrefix(appliedManifestWork.Name, m.hubHash) {
		return m.evictAppliedManifestWork(ctx, controllerContext, appliedManifestWork)
	}

	// stop to evict the current appliedmanifestwork when its relating manifestwork is recreated on the hub
	return m.stopToEvictAppliedManifestWork(ctx, appliedManifestWork)
}

func (m *unmanagedAppliedWorkController) evictAppliedManifestWork(ctx context.Context,
	controllerContext factory.SyncContext, appliedManifestWork *workapiv1.AppliedManifestWork) error {
	now := time.Now()

	logger := klog.FromContext(ctx)

	evictionStartTime := appliedManifestWork.Status.EvictionStartTime
	if evictionStartTime == nil {
		return m.patchEvictionStartTime(ctx, appliedManifestWork, &metav1.Time{Time: now})
	}

	if now.Before(evictionStartTime.Add(m.evictionGracePeriod)) {
		controllerContext.Queue().AddAfter(appliedManifestWork.Name, m.rateLimiter.When(appliedManifestWork.Name))
		return nil
	}

	err := m.appliedManifestWorkClient.Delete(ctx, appliedManifestWork.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	logger.Info("AppliedManifestWork evicted after eviction grace period", "agentID", m.agentID)
	return nil
}

func (m *unmanagedAppliedWorkController) stopToEvictAppliedManifestWork(
	ctx context.Context, appliedManifestWork *workapiv1.AppliedManifestWork) error {
	if appliedManifestWork.Status.EvictionStartTime == nil {
		return nil
	}

	m.rateLimiter.Forget(appliedManifestWork.Name)
	return m.patchEvictionStartTime(ctx, appliedManifestWork, nil)
}

func (m *unmanagedAppliedWorkController) patchEvictionStartTime(ctx context.Context,
	appliedManifestWork *workapiv1.AppliedManifestWork, evictionStartTime *metav1.Time) error {
	newAppliedWork := appliedManifestWork.DeepCopy()
	newAppliedWork.Status.EvictionStartTime = evictionStartTime
	_, err := m.patcher.PatchStatus(ctx, newAppliedWork, newAppliedWork.Status, appliedManifestWork.Status)
	return err
}
