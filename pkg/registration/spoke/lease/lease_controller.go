package lease

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	leasev1client "k8s.io/client-go/kubernetes/typed/coordination/v1"

	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

const leaseUpdateJitterFactor = 0.25

// managedClusterLeaseController periodically updates the lease of a managed cluster on hub cluster to keep the heartbeat of a managed cluster.
type managedClusterLeaseController struct {
	clusterName              string
	hubClusterLister         clusterv1listers.ManagedClusterLister
	lastLeaseDurationSeconds int32
	leaseUpdater             leaseUpdaterInterface
}

// NewManagedClusterLeaseController creates a new managed cluster lease controller on the managed cluster.
func NewManagedClusterLeaseController(
	clusterName string,
	leaseClient leasev1client.LeaseInterface,
	hubClusterInformer clusterv1informer.ManagedClusterInformer) factory.Controller {
	c := &managedClusterLeaseController{
		clusterName:      clusterName,
		hubClusterLister: hubClusterInformer.Lister(),
		leaseUpdater: &leaseUpdater{
			leaseClient: leaseClient,
			clusterName: clusterName,
			leaseName:   "managed-cluster-lease",
		},
	}

	return factory.New().
		WithInformers(hubClusterInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterLeaseController")
}

// sync starts a lease update routine with the managed cluster lease duration.
func (c *managedClusterLeaseController) sync(ctx context.Context, syncCtx factory.SyncContext, _ string) error {
	cluster, err := c.hubClusterLister.Get(c.clusterName)
	// unable to get managed cluster, make sure there is no lease update routine.
	if err != nil {
		c.leaseUpdater.stop(ctx, syncCtx)
		return fmt.Errorf("unable to get managed cluster %q from hub: %w", c.clusterName, err)
	}

	// the managed cluster is not accepted, make sure there is no lease update routine.
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
		c.leaseUpdater.stop(ctx, syncCtx)
		return nil
	}

	observedLeaseDurationSeconds := cluster.Spec.LeaseDurationSeconds
	// for backward compatible, release-2.1 has mutating admission webhook to mutate this field,
	// but release-2.0 does not have the mutating admission webhook
	if observedLeaseDurationSeconds == 0 {
		observedLeaseDurationSeconds = 60
	}

	// if lease duration is changed, stop the old lease update routine.
	if c.lastLeaseDurationSeconds != observedLeaseDurationSeconds {
		c.lastLeaseDurationSeconds = observedLeaseDurationSeconds
		c.leaseUpdater.stop(ctx, syncCtx)
	}

	// ensure there is a starting lease update routine.
	c.leaseUpdater.start(ctx, syncCtx, time.Duration(c.lastLeaseDurationSeconds)*time.Second)
	return nil
}

type leaseUpdaterInterface interface {
	start(ctx context.Context, syncCtx factory.SyncContext, leaseDuration time.Duration)
	stop(ctx context.Context, syncCtx factory.SyncContext)
}

// leaseUpdater periodically updates the lease of a managed cluster
type leaseUpdater struct {
	leaseClient leasev1client.LeaseInterface
	clusterName string
	leaseName   string
	lock        sync.Mutex
	cancel      context.CancelFunc
}

// start a lease update routine to update the lease of a managed cluster periodically.
func (u *leaseUpdater) start(ctx context.Context, syncCtx factory.SyncContext, leaseDuration time.Duration) {
	u.lock.Lock()
	defer u.lock.Unlock()

	if u.cancel != nil {
		return
	}

	var updateCtx context.Context
	updateCtx, u.cancel = context.WithCancel(ctx)
	go wait.JitterUntilWithContext(updateCtx, u.update, leaseDuration, leaseUpdateJitterFactor, true)
	syncCtx.Recorder().Eventf(ctx, "ManagedClusterLeaseUpdateStarted", "Start to update lease %q on cluster %q", u.leaseName, u.clusterName)
}

// stop the lease update routine.
func (u *leaseUpdater) stop(ctx context.Context, syncCtx factory.SyncContext) {
	u.lock.Lock()
	defer u.lock.Unlock()

	if u.cancel == nil {
		return
	}
	u.cancel()
	u.cancel = nil
	syncCtx.Recorder().Eventf(ctx, "ManagedClusterLeaseUpdateStoped", "Stop to update lease %q on cluster %q", u.leaseName, u.clusterName)
}

// update the lease of a given managed cluster.
func (u *leaseUpdater) update(ctx context.Context) {
	lease, err := u.leaseClient.Get(ctx, u.leaseName, metav1.GetOptions{})
	if err != nil {
		utilruntime.HandleErrorWithContext(ctx, err, "unable to get cluster lease on hub cluster", "lease", u.leaseName)
		return
	}

	lease.Spec.RenewTime = &metav1.MicroTime{Time: time.Now()}
	if _, err = u.leaseClient.Update(ctx, lease, metav1.UpdateOptions{}); err != nil {
		utilruntime.HandleErrorWithContext(ctx, err, "unable to update cluster lease on hub cluster", "lease", u.leaseName)
	}
}
