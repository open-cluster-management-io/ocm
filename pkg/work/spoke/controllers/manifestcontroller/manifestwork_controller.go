package manifestcontroller

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/logging"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/work/spoke/apply"
	"open-cluster-management.io/ocm/pkg/work/spoke/auth"
)

var (
	// ResyncInterval defines the base interval for periodic reconciliation via AddAfter.
	// Used to requeue a ManifestWork after it has been successfully reconciled.
	ResyncInterval = 4 * time.Minute
)

const controllerName = "ManifestWorkController"

type workReconcile interface {
	reconcile(ctx context.Context,
		controllerContext factory.SyncContext,
		mw *workapiv1.ManifestWork,
		amw *workapiv1.AppliedManifestWork,
		results []applyResult) (*workapiv1.ManifestWork, *workapiv1.AppliedManifestWork, []applyResult, error)
}

// ManifestWorkController is to reconcile the workload resources
// fetched from hub cluster on spoke cluster.
type ManifestWorkController struct {
	manifestWorkPatcher        patcher.Patcher[*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus]
	manifestWorkLister         worklister.ManifestWorkNamespaceLister
	appliedManifestWorkClient  workv1client.AppliedManifestWorkInterface
	appliedManifestWorkPatcher patcher.Patcher[*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus]
	appliedManifestWorkLister  worklister.AppliedManifestWorkLister
	hubHash                    string
	agentID                    string
	reconcilers                []workReconcile
}

// NewManifestWorkController returns a ManifestWorkController
func NewManifestWorkController(
	spokeDynamicClient dynamic.Interface,
	spokeKubeClient kubernetes.Interface,
	spokeAPIExtensionClient apiextensionsclient.Interface,
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	appliedManifestWorkInformer workinformer.AppliedManifestWorkInformer,
	hubHash, agentID string,
	restMapper meta.RESTMapper,
	validator auth.ExecutorValidator) factory.Controller {

	syncCtx := factory.NewSyncContext("manifestwork-controller")

	controller := &ManifestWorkController{
		manifestWorkPatcher: patcher.NewPatcher[
			*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
			manifestWorkClient),
		manifestWorkLister:        manifestWorkLister,
		appliedManifestWorkClient: appliedManifestWorkClient,
		appliedManifestWorkPatcher: patcher.NewPatcher[
			*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus](
			appliedManifestWorkClient),
		appliedManifestWorkLister: appliedManifestWorkInformer.Lister(),
		hubHash:                   hubHash,
		agentID:                   agentID,
		reconcilers: []workReconcile{
			&manifestworkReconciler{
				restMapper: restMapper,
				appliers:   apply.NewAppliers(spokeDynamicClient, spokeKubeClient, spokeAPIExtensionClient),
				validator:  validator,
			},
			&appliedManifestWorkReconciler{
				spokeDynamicClient: spokeDynamicClient,
				rateLimiter:        workqueue.NewTypedItemExponentialFailureRateLimiter[string](5*time.Millisecond, 1000*time.Second),
			},
		},
	}

	_, err := manifestWorkInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    onAddFunc(syncCtx.Queue()),
		UpdateFunc: onUpdateFunc(syncCtx.Queue()),
	})
	utilruntime.Must(err)

	return factory.New().
		WithBareInformers(
			manifestWorkInformer.Informer(),
			// we do not need to reconcile with event of appliedemanifestwork, just ensure cache is synced.
			appliedManifestWorkInformer.Informer(),
		).
		WithSyncContext(syncCtx).
		WithSync(controller.sync).ToController(controllerName)
}

// sync is the main reconcile loop for manifest work. It is triggered in two scenarios
// 1. ManifestWork API changes
// 2. Resources defined in manifest changed on spoke
func (m *ManifestWorkController) sync(ctx context.Context, controllerContext factory.SyncContext, manifestWorkName string) error {
	logger := klog.FromContext(ctx).WithValues("manifestWorkName", manifestWorkName)
	logger.V(5).Info("Reconciling ManifestWork")

	oldManifestWork, err := m.manifestWorkLister.Get(manifestWorkName)
	if apierrors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	manifestWork := oldManifestWork.DeepCopy()

	// set tracing key from work if there is any
	logger = logging.SetLogTracingByObject(logger, manifestWork)
	ctx = klog.NewContext(ctx, logger)

	// no work to do if we're deleted
	if !manifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	// don't do work if the finalizer is not present
	// it ensures all maintained resources will be cleaned once manifestwork is deleted
	if !commonhelper.HasFinalizer(manifestWork.Finalizers, workapiv1.ManifestWorkFinalizer) {
		return nil
	}

	// work that is completed does not receive any updates
	if meta.IsStatusConditionTrue(manifestWork.Status.Conditions, workapiv1.WorkComplete) {
		return nil
	}

	// Apply appliedManifestWork
	appliedManifestWork, err := m.applyAppliedManifestWork(ctx, manifestWork.Name, m.hubHash, m.agentID, manifestWork.ObjectMeta.Labels)
	if err != nil {
		return err
	}
	newAppliedManifestWork := appliedManifestWork.DeepCopy()

	var requeueTime = wait.Jitter(ResyncInterval, 0.5)
	var errs []error
	var results []applyResult
	for _, reconciler := range m.reconcilers {
		manifestWork, newAppliedManifestWork, results, err = reconciler.reconcile(
			ctx, controllerContext, manifestWork, newAppliedManifestWork, results)
		var rqe commonhelper.RequeueError
		if err != nil && errors.As(err, &rqe) {
			if requeueTime > rqe.RequeueTime {
				requeueTime = rqe.RequeueTime
			}
		} else if err != nil {
			errs = append(errs, err)
		}
	}

	// Update work status
	_, err = m.manifestWorkPatcher.PatchStatus(ctx, manifestWork, manifestWork.Status, oldManifestWork.Status)
	if err != nil {
		return err
	}

	_, err = m.appliedManifestWorkPatcher.PatchStatus(
		ctx, newAppliedManifestWork, newAppliedManifestWork.Status, appliedManifestWork.Status)
	if err != nil {
		return err
	}

	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}

	logger.V(2).Info("Requeue manifestwork", "requeue time", requeueTime)
	controllerContext.Queue().AddAfter(manifestWorkName, requeueTime)

	return nil
}

func (m *ManifestWorkController) applyAppliedManifestWork(ctx context.Context, workName,
	hubHash, agentID string, labels map[string]string) (*workapiv1.AppliedManifestWork, error) {
	appliedManifestWorkName := fmt.Sprintf("%s-%s", m.hubHash, workName)
	requiredAppliedWork := &workapiv1.AppliedManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       appliedManifestWorkName,
			Labels:     labels,
			Finalizers: []string{workapiv1.AppliedManifestWorkFinalizer},
		},
		Spec: workapiv1.AppliedManifestWorkSpec{
			HubHash:          hubHash,
			ManifestWorkName: workName,
			AgentID:          agentID,
		},
	}

	appliedManifestWork, err := m.appliedManifestWorkLister.Get(appliedManifestWorkName)
	switch {
	case apierrors.IsNotFound(err):
		return m.appliedManifestWorkClient.Create(ctx, requiredAppliedWork, metav1.CreateOptions{})

	case err != nil:
		return nil, err
	}

	_, err = m.appliedManifestWorkPatcher.PatchSpec(ctx, appliedManifestWork, requiredAppliedWork.Spec, appliedManifestWork.Spec)
	return appliedManifestWork, err
}

func onAddFunc(queue workqueue.TypedRateLimitingInterface[string]) func(obj interface{}) {
	return func(obj interface{}) {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			utilruntime.HandleError(err)
			return
		}
		if commonhelper.HasFinalizer(accessor.GetFinalizers(), workapiv1.ManifestWorkFinalizer) {
			queue.Add(accessor.GetName())
		}
	}
}

func onUpdateFunc(queue workqueue.TypedRateLimitingInterface[string]) func(oldObj, newObj interface{}) {
	return func(oldObj, newObj interface{}) {
		newWork, ok := newObj.(*workapiv1.ManifestWork)
		if !ok {
			return
		}
		oldWork, ok := oldObj.(*workapiv1.ManifestWork)
		if !ok {
			return
		}
		// enqueue when finalizer is added, spec or label is changed.
		if !commonhelper.HasFinalizer(newWork.GetFinalizers(), workapiv1.ManifestWorkFinalizer) {
			return
		}
		if !commonhelper.HasFinalizer(oldWork.GetFinalizers(), workapiv1.ManifestWorkFinalizer) {
			queue.Add(newWork.GetName())
			return
		}
		if !apiequality.Semantic.DeepEqual(newWork.Spec, oldWork.Spec) ||
			!apiequality.Semantic.DeepEqual(newWork.Labels, oldWork.Labels) {

			// Reset the rate limiter to process the work immediately when the spec or labels change.
			// Without this reset, if the work was previously failing and being rate-limited (exponential backoff),
			// it would continue to wait for the backoff delay before processing the new spec change.
			// By calling Forget(), we clear the rate limiter's failure count and backoff state,
			// ensuring the updated work is reconciled immediately if meet failure rather than
			// waiting for a long time rate-limited retry.
			queue.Forget(newWork.GetName())
			queue.Add(newWork.GetName())
		}
	}
}
