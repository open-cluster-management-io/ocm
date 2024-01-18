package taint

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	informerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	listerv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/registration/helpers"
)

var (
	UnavailableTaint = v1.Taint{
		Key:    v1.ManagedClusterTaintUnavailable,
		Effect: v1.TaintEffectNoSelect,
	}

	UnreachableTaint = v1.Taint{
		Key:    v1.ManagedClusterTaintUnreachable,
		Effect: v1.TaintEffectNoSelect,
	}
)

// taintController
type taintController struct {
	patcher       patcher.Patcher[*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus]
	clusterLister listerv1.ManagedClusterLister
	eventRecorder events.Recorder
}

// NewTaintController creates a new taint controller
func NewTaintController(
	clusterClient clientset.Interface,
	clusterInformer informerv1.ManagedClusterInformer,
	recorder events.Recorder) factory.Controller {
	c := &taintController{
		patcher: patcher.NewPatcher[
			*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus](
			clusterClient.ClusterV1().ManagedClusters()),
		clusterLister: clusterInformer.Lister(),
		eventRecorder: recorder.WithComponentSuffix("taint-controller"),
	}
	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, clusterInformer.Informer()).
		WithSync(c.sync).
		ToController("taintController", recorder)
}

func (c *taintController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	logger := klog.FromContext(ctx)
	managedClusterName := syncCtx.QueueKey()
	logger.V(4).Info("Reconciling ManagedCluster", "managedClusterName", managedClusterName)
	managedCluster, err := c.clusterLister.Get(managedClusterName)
	if errors.IsNotFound(err) {
		// Spoke cluster not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	if !managedCluster.DeletionTimestamp.IsZero() {
		return nil
	}

	newManagedCluster := managedCluster.DeepCopy()
	newTaints := newManagedCluster.Spec.Taints
	cond := meta.FindStatusCondition(newManagedCluster.Status.Conditions, v1.ManagedClusterConditionAvailable)
	var updated bool

	switch {
	case cond == nil || cond.Status == metav1.ConditionUnknown:
		updated = helpers.RemoveTaints(&newTaints, UnavailableTaint)
		updated = helpers.AddTaints(&newTaints, UnreachableTaint) || updated
	case cond.Status == metav1.ConditionFalse:
		updated = helpers.RemoveTaints(&newTaints, UnreachableTaint)
		updated = helpers.AddTaints(&newTaints, UnavailableTaint) || updated
	case cond.Status == metav1.ConditionTrue:
		updated = helpers.RemoveTaints(&newTaints, UnavailableTaint, UnreachableTaint)
	}

	if updated {
		newManagedCluster.Spec.Taints = newTaints
		if _, err = c.patcher.PatchSpec(ctx, newManagedCluster, newManagedCluster.Spec, managedCluster.Spec); err != nil {
			return err
		}
		c.eventRecorder.Eventf("ManagedClusterConditionAvailableUpdated", "Update the original taints to the %+v", newTaints)
	}
	return nil
}
