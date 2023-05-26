package managedcluster

import (
	"context"
	"fmt"
	"sync"
	"time"

	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

const leaseUpdateJitterFactor = 0.25

// managedClusterLeaseController periodically updates the lease of a managed cluster on hub cluster to keep the heartbeat of a managed cluster.
type managedClusterLeaseController struct {
	clusterName              string
	hubClusterLister         clusterv1listers.ManagedClusterLister
	lastLeaseDurationSeconds int32
	leaseUpdater             *leaseUpdater
}

// NewManagedClusterLeaseController creates a new managed cluster lease controller on the managed cluster.
func NewManagedClusterLeaseController(
	clusterName string,
	hubClient clientset.Interface,
	hubClusterInformer clusterv1informer.ManagedClusterInformer,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterLeaseController{
		clusterName:      clusterName,
		hubClusterLister: hubClusterInformer.Lister(),
		leaseUpdater: &leaseUpdater{
			hubClient:   hubClient,
			clusterName: clusterName,
			leaseName:   "managed-cluster-lease",
			recorder:    recorder,
		},
	}

	return factory.New().
		WithInformers(hubClusterInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterLeaseController", recorder)
}

// sync starts a lease update routine with the managed cluster lease duration.
func (c *managedClusterLeaseController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	cluster, err := c.hubClusterLister.Get(c.clusterName)
	// unable to get managed cluster, make sure there is no lease update routine.
	if err != nil {
		c.leaseUpdater.stop()
		return fmt.Errorf("unable to get managed cluster %q from hub: %w", c.clusterName, err)
	}

	// the managed cluster is not accepted, make sure there is no lease update routine.
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
		c.leaseUpdater.stop()
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
		c.leaseUpdater.stop()
	}

	// ensure there is a starting lease update routine.
	c.leaseUpdater.start(ctx, time.Duration(c.lastLeaseDurationSeconds)*time.Second)
	return nil
}

// leaseUpdater periodically updates the lease of a managed cluster
type leaseUpdater struct {
	hubClient   clientset.Interface
	clusterName string
	leaseName   string
	lock        sync.Mutex
	cancel      context.CancelFunc
	recorder    events.Recorder
}

// start a lease update routine to update the lease of a managed cluster periodically.
func (u *leaseUpdater) start(ctx context.Context, leaseDuration time.Duration) {
	u.lock.Lock()
	defer u.lock.Unlock()

	if u.cancel != nil {
		return
	}

	var updateCtx context.Context
	updateCtx, u.cancel = context.WithCancel(ctx)
	go wait.JitterUntilWithContext(updateCtx, u.update, leaseDuration, leaseUpdateJitterFactor, true)
	u.recorder.Eventf("ManagedClusterLeaseUpdateStarted", "Start to update lease %q on cluster %q", u.leaseName, u.clusterName)
}

// stop the lease update routine.
func (u *leaseUpdater) stop() {
	u.lock.Lock()
	defer u.lock.Unlock()

	if u.cancel == nil {
		return
	}
	u.cancel()
	u.cancel = nil
	u.recorder.Eventf("ManagedClusterLeaseUpdateStoped", "Stop to update lease %q on cluster %q", u.leaseName, u.clusterName)
}

// update the lease of a given managed cluster.
func (u *leaseUpdater) update(ctx context.Context) {
	lease, err := u.hubClient.CoordinationV1().Leases(u.clusterName).Get(ctx, u.leaseName, metav1.GetOptions{})
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to get cluster lease %q on hub cluster: %w", u.leaseName, err))
		return
	}

	lease.Spec.RenewTime = &metav1.MicroTime{Time: time.Now()}
	if _, err = u.hubClient.CoordinationV1().Leases(u.clusterName).Update(ctx, lease, metav1.UpdateOptions{}); err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to update cluster lease %q on hub cluster: %w", u.leaseName, err))
	}
}
