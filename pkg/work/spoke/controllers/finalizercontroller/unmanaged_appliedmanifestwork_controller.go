package finalizercontroller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
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

	// massEvictionWarnMinCount is the minimum number of this agent's AppliedManifestWorks that must
	// already carry EvictionStartTime before we emit the mass-eviction warning.
	massEvictionWarnMinCount = 10
	// massEvictionWarnRatio is the fraction of this agent's AppliedManifestWorks with
	// EvictionStartTime set that triggers the warning (together with massEvictionWarnMinCount).
	massEvictionWarnRatio = 0.5
	// massEvictionWarnCooldown limits how often the warning is emitted.
	massEvictionWarnCooldown = 5 * time.Minute
)

type unmanagedAppliedWorkController struct {
	manifestWorkLister         worklister.ManifestWorkNamespaceLister
	appliedManifestWorkClient  workv1client.AppliedManifestWorkInterface
	patcher                    patcher.Patcher[*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus]
	appliedManifestWorkLister  worklister.AppliedManifestWorkLister
	manifestWorkInformerSynced cache.InformerSynced
	hubHash                    string
	agentID                    string
	evictionGracePeriod        time.Duration

	massEvictionWarnMu     sync.Mutex
	lastMassEvictionWarnAt time.Time
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
//
// Eviction based on a ManifestWork NotFound is gated on the ManifestWork informer having
// synced at least once. With the kube driver that matches the usual controller start barrier;
// with the cloudevents driver it avoids treating an empty/partial local store during
// (re)subscribe replay as "hub deleted the work" (see issue #1786).
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
		appliedManifestWorkLister:  appliedManifestWorkInformer.Lister(),
		manifestWorkInformerSynced: manifestWorkInformer.Informer().HasSynced,
		hubHash:                    hubHash,
		agentID:                    agentID,
		evictionGracePeriod:        evictionGracePeriod,
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

	// Do not treat ManifestWork NotFound as hub deletion until the work informer has
	// completed its initial sync. An unsynced/partial local store (common during
	// cloudevents resubscribe replay) would otherwise stamp evictionStartTime on
	// every AppliedManifestWork whose ManifestWork has not been replayed yet.
	// Also clear any eviction timer already stamped so a later sync does not inherit
	// time spent waiting for the store to fill.
	if m.manifestWorkInformerSynced != nil && !m.manifestWorkInformerSynced() {
		logger.V(4).Info("Skipping AppliedManifestWork eviction until ManifestWork informer has synced")
		controllerContext.Queue().AddAfter(appliedManifestWorkName, time.Second)
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
		if err := m.patchEvictionStartTime(ctx, appliedManifestWork, &metav1.Time{Time: now}); err != nil {
			return err
		}
		// Mass simultaneous stamps usually mean the local ManifestWork store is empty/partial
		// (cloudevents replay), not that the hub deleted most of the estate (#1786).
		m.warnIfMassEviction(ctx, controllerContext)
		return nil
	}

	evictionTime := evictionStartTime.Add(m.evictionGracePeriod)
	if now.Before(evictionTime) {
		// Calculate the exact remaining time until eviction
		remainingTime := evictionTime.Sub(now)
		controllerContext.Queue().AddAfter(appliedManifestWork.Name, remainingTime)
		logger.V(4).Info("AppliedManifestWork scheduled for eviction",
			"evictionTime", evictionTime, "remainingTime", remainingTime)
		return nil
	}

	err := m.appliedManifestWorkClient.Delete(ctx, appliedManifestWork.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	logger.Info("AppliedManifestWork evicted after eviction grace period", "agentID", m.agentID)
	return nil
}

// warnIfMassEviction logs and records a warning when a large share of this agent's
// AppliedManifestWorks already have EvictionStartTime set. With the cloudevents driver the
// ManifestWork informer can report HasSynced before resync replay fills the local store, so
// NotFound-based eviction can stamp many works at once; a true "store reconciled" signal must
// come from sdk-go (#1786).
func (m *unmanagedAppliedWorkController) warnIfMassEviction(ctx context.Context, controllerContext factory.SyncContext) {
	stamped, total := countAgentEvictionStamps(m.appliedManifestWorkLister, m.agentID)
	// The lister may not yet reflect the EvictionStartTime we just patched on the current object.
	stamped++
	if total < stamped {
		total = stamped
	}
	if !shouldWarnMassEviction(stamped, total) {
		return
	}

	m.massEvictionWarnMu.Lock()
	defer m.massEvictionWarnMu.Unlock()
	now := time.Now()
	if !m.lastMassEvictionWarnAt.IsZero() && now.Sub(m.lastMassEvictionWarnAt) < massEvictionWarnCooldown {
		return
	}
	m.lastMassEvictionWarnAt = now

	msg := fmt.Sprintf(
		"EvictionStartTime is set on %d/%d AppliedManifestWorks for agent %q; "+
			"this often means the ManifestWork store is empty or still replaying rather than hub deletion. "+
			"Consider increasing appliedManifestWorkEvictionGracePeriod or verifying work-driver sync health.",
		stamped, total, m.agentID)
	klog.FromContext(ctx).Info(msg)
	if recorder := controllerContext.Recorder(); recorder != nil {
		recorder.Warning(ctx, "MassAppliedManifestWorkEviction", msg)
	}
}

func shouldWarnMassEviction(stamped, total int) bool {
	if total == 0 || stamped < massEvictionWarnMinCount {
		return false
	}
	return float64(stamped)/float64(total) >= massEvictionWarnRatio
}

func countAgentEvictionStamps(lister worklister.AppliedManifestWorkLister, agentID string) (stamped, total int) {
	if lister == nil {
		return 0, 0
	}
	appliedWorks, err := lister.List(labels.Everything())
	if err != nil {
		return 0, 0
	}
	for _, aw := range appliedWorks {
		if aw.Spec.AgentID != agentID {
			continue
		}
		total++
		if aw.Status.EvictionStartTime != nil {
			stamped++
		}
	}
	return stamped, total
}

func (m *unmanagedAppliedWorkController) stopToEvictAppliedManifestWork(
	ctx context.Context, appliedManifestWork *workapiv1.AppliedManifestWork) error {
	if appliedManifestWork.Status.EvictionStartTime == nil {
		return nil
	}

	return m.patchEvictionStartTime(ctx, appliedManifestWork, nil)
}

func (m *unmanagedAppliedWorkController) patchEvictionStartTime(ctx context.Context,
	appliedManifestWork *workapiv1.AppliedManifestWork, evictionStartTime *metav1.Time) error {
	newAppliedWork := appliedManifestWork.DeepCopy()
	newAppliedWork.Status.EvictionStartTime = evictionStartTime
	_, err := m.patcher.PatchStatus(ctx, newAppliedWork, newAppliedWork.Status, appliedManifestWork.Status)
	return err
}
