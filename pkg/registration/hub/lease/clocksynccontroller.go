package lease

import (
	"context"
	"time"

	coordv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	coordinformers "k8s.io/client-go/informers/coordination/v1"
	coordlisters "k8s.io/client-go/listers/coordination/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

type clockSyncController struct {
	patcher       patcher.Patcher[*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus]
	clusterLister clusterv1listers.ManagedClusterLister
	leaseLister   coordlisters.LeaseLister
}

const (
	clockSyncControllerName = "ClockSyncController"
)

func NewClockSyncController(
	clusterClient clientset.Interface,
	clusterInformer clusterv1informer.ManagedClusterInformer,
	leaseInformer coordinformers.LeaseInformer,
) factory.Controller {
	c := &clockSyncController{
		patcher: patcher.NewPatcher[
			*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
			clusterClient.ClusterV1().ManagedClusters()),
		clusterLister: clusterInformer.Lister(),
		leaseLister:   leaseInformer.Lister(),
	}

	syncCtx := factory.NewSyncContext(clockSyncControllerName)
	leaseRenewTimeUpdateInformer := renewUpdateInfomer(syncCtx.Queue(), leaseInformer)

	return factory.New().WithSyncContext(syncCtx).
		WithBareInformers(leaseRenewTimeUpdateInformer).
		WithSync(c.sync).
		ToController(clockSyncControllerName)
}

func renewUpdateInfomer(q workqueue.TypedRateLimitingInterface[string], leaseInformer coordinformers.LeaseInformer) factory.Informer {
	leaseRenewTimeUpdateInformer := leaseInformer.Informer()
	queueKeyByLabel := queue.QueueKeyByLabel(clusterv1.ClusterNameLabelKey)
	_, err := leaseRenewTimeUpdateInformer.AddEventHandler(&cache.FilteringResourceEventHandler{
		FilterFunc: queue.UnionFilter(queue.FileterByLabel(clusterv1.ClusterNameLabelKey), queue.FilterByNames(leaseName)),
		Handler: &cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				// only renew field update event will be added to queue
				oldLease := oldObj.(*coordv1.Lease)
				newLease := newObj.(*coordv1.Lease)
				if !oldLease.Spec.RenewTime.Equal(newLease.Spec.RenewTime) {
					for _, queueKey := range queueKeyByLabel(newLease) {
						q.Add(queueKey)
					}
				}
			},
		},
	})
	if err != nil {
		runtime.HandleError(err)
	}
	return leaseRenewTimeUpdateInformer
}

func (c *clockSyncController) sync(ctx context.Context, syncCtx factory.SyncContext, clusterName string) error {
	// the event caused by resync will be filtered because the cluster is not found
	cluster, err := c.clusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		// the cluster is not found, do nothing
		return nil
	}
	if err != nil {
		return err
	}

	now := time.Now()
	observedLease, err := c.leaseLister.Leases(cluster.Name).Get(leaseName)
	if err != nil {
		return err
	}
	// When the agent's lease get renewed, the "now" on hub should close to the RenewTime on agent.
	// If the two time are not close(the same duration in the lease controller), we assume the clock is out of sync.
	// Then, if the Clock is out of sync, the agent will not be able to update the status of managed cluster.
	leaseDuration := time.Duration(leaseDurationTimes*cluster.Spec.LeaseDurationSeconds) * time.Second
	if leaseDuration == 0 {
		leaseDuration = time.Duration(LeaseDurationSeconds*leaseDurationTimes) * time.Second
	}

	if err := c.updateClusterStatusClockSynced(ctx, syncCtx, cluster,
		now.Sub(observedLease.Spec.RenewTime.Time) < leaseDuration && observedLease.Spec.RenewTime.Time.Sub(now) < leaseDuration); err != nil {
		return err
	}
	return nil
}

func (c *clockSyncController) updateClusterStatusClockSynced(ctx context.Context, syncCtx factory.SyncContext, cluster *clusterv1.ManagedCluster, synced bool) error {
	var desiredStatus metav1.ConditionStatus
	var condition metav1.Condition
	if synced {
		desiredStatus = metav1.ConditionTrue
		condition = metav1.Condition{
			Type:    clusterv1.ManagedClusterConditionClockSynced,
			Status:  metav1.ConditionTrue,
			Reason:  "ManagedClusterClockSynced",
			Message: "The clock of the managed cluster is synced with the hub.",
		}
	} else {
		desiredStatus = metav1.ConditionFalse
		condition = metav1.Condition{
			Type:    clusterv1.ManagedClusterConditionClockSynced,
			Status:  metav1.ConditionFalse,
			Reason:  "ManagedClusterClockOutOfSync",
			Message: "The clock of hub and agent is out of sync. This may cause the Unknown status and affect agent functionalities.",
		}
	}

	if meta.IsStatusConditionPresentAndEqual(cluster.Status.Conditions, clusterv1.ManagedClusterConditionClockSynced, desiredStatus) {
		// the managed cluster clock synced condition alreay is desired status, do nothing
		return nil
	}

	newCluster := cluster.DeepCopy()
	meta.SetStatusCondition(&newCluster.Status.Conditions, condition)

	updated, err := c.patcher.PatchStatus(ctx, newCluster, newCluster.Status, cluster.Status)
	if updated {
		syncCtx.Recorder().Eventf(ctx, "ManagedClusterClockSyncedConditionUpdated",
			"update managed cluster %q clock synced condition to %v.", cluster.Name, desiredStatus)
	}
	return err
}
