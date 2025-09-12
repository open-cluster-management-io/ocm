package manifestcontroller

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/pkg/errors"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	commonhelper "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/spoke/apply"
	"open-cluster-management.io/ocm/pkg/work/spoke/auth"
)

var (
	// ResyncInterval defines the maximum interval for resyncing a ManifestWork. It is used to:
	//   1) Set the `ResyncEvery` for the `ManifestWorkAgent` controller;
	//   2) Requeue a ManifestWork after it has been successfully reconciled.
	ResyncInterval = 5 * time.Minute
)

type workReconcile interface {
	reconcile(ctx context.Context, controllerContext factory.SyncContext, mw *workapiv1.ManifestWork,
		amw *workapiv1.AppliedManifestWork) (*workapiv1.ManifestWork, *workapiv1.AppliedManifestWork, error)
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
	recorder events.Recorder,
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
				rateLimiter:        workqueue.NewItemExponentialFailureRateLimiter(5*time.Millisecond, 1000*time.Second),
			},
		},
	}

	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, manifestWorkInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(
			helper.AppliedManifestworkQueueKeyFunc(hubHash),
			helper.AppliedManifestworkHubHashFilter(hubHash),
			appliedManifestWorkInformer.Informer()).
		WithSync(controller.sync).ResyncEvery(ResyncInterval).ToController("ManifestWorkAgent", recorder)
}

// sync is the main reconcile loop for manifest work. It is triggered in two scenarios
// 1. ManifestWork API changes
// 2. Resources defined in manifest changed on spoke
func (m *ManifestWorkController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	manifestWorkName := controllerContext.QueueKey()
	klog.V(5).Infof("Reconciling ManifestWork %q", manifestWorkName)

	oldManifestWork, err := m.manifestWorkLister.Get(manifestWorkName)
	if apierrors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	manifestWork := oldManifestWork.DeepCopy()

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

	var requeueTime = ResyncInterval
	var errs []error
	for _, reconciler := range m.reconcilers {
		manifestWork, newAppliedManifestWork, err = reconciler.reconcile(
			ctx, controllerContext, manifestWork, newAppliedManifestWork)
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
	mwUpdated, err := m.manifestWorkPatcher.PatchStatus(ctx, manifestWork, manifestWork.Status, oldManifestWork.Status)
	if err != nil {
		return err
	}

	amwUpdated, err := m.appliedManifestWorkPatcher.PatchStatus(
		ctx, newAppliedManifestWork, newAppliedManifestWork.Status, appliedManifestWork.Status)
	if err != nil {
		return err
	}

	if len(errs) > 0 {
		klog.Errorf("Reconcile work %s fails with err: %v", manifestWorkName, errs)
		return utilerrors.NewAggregate(errs)
	}

	// we do not need to requeue when manifestwork/appliedmanifestwork are updated, since a following
	// reconcile will be executed with update event, and the requeue can be set in this following reconcile
	// if needed.
	if !mwUpdated && !amwUpdated {
		controllerContext.Queue().AddAfter(manifestWorkName, requeueTime)
	}

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
