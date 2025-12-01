package registration

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	leasev1client "k8s.io/client-go/kubernetes/typed/coordination/v1"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

const leaseName = "managed-cluster-lease"

// The timeout controller is used to handle the case that the hub is not connectable.
// TODO: it should finally be part of the lease controller. @xuezhaojun
type hubTimeoutController struct {
	clusterName        string
	leaseClient        leasev1client.LeaseInterface
	timeoutSeconds     int32
	lastLeaseRenewTime time.Time
	handleTimeout      func(ctx context.Context) error

	startTime time.Time
}

func NewHubTimeoutController(
	clusterName string,
	leaseClient leasev1client.LeaseInterface,
	timeoutSeconds int32,
	handleTimeout func(ctx context.Context) error,
) factory.Controller {
	c := &hubTimeoutController{
		clusterName:    clusterName,
		timeoutSeconds: timeoutSeconds,
		handleTimeout:  handleTimeout,
		leaseClient:    leaseClient,
		startTime:      time.Now(),
	}
	return factory.New().WithSync(c.sync).ResyncEvery(time.Minute).
		ToController("HubTimeoutController")
}

func (c *hubTimeoutController) sync(ctx context.Context, _ factory.SyncContext, _ string) error {
	logger := klog.FromContext(ctx)
	if c.handleTimeout == nil {
		return nil
	}

	lease, err := c.leaseClient.Get(ctx, leaseName, metav1.GetOptions{})
	if err != nil {
		logger.Error(err, "Failed to get lease", "cluster", c.clusterName, "lease", leaseName)
		// This means to handle the case which the hub is not connectable from the beginning.
		if c.lastLeaseRenewTime.IsZero() {
			c.lastLeaseRenewTime = time.Now()
		}
	} else {
		c.lastLeaseRenewTime = lease.Spec.RenewTime.Time
	}

	// If `startTime` within 10s, skip the timeout check.
	// This handles cases where old leases remain due to incomplete cleanup.
	//
	// Example scenario:
	// 1. ManagedCluster-A is connected to Hub1 with an active lease
	// 2. Hub1 unexpectedly fails (power outage) - no cleanup opportunity
	// 3. ManagedCluster-A detects timeout and switches to Hub2
	// 4. Hub1 comes back online with the old stale lease still present
	// 5. ManagedCluster-A migrates back to Hub1 (which has the expired lease)
	// 6. With this grace period: lease controller gets time to update the lease
	//    before timeout checks begin, preventing false timeouts. Otherwise,
	//    timeout controller runs immediately and detects the stale lease as
	//    expired, triggering an unwanted timeout
	//
	// This also applies to migration scenarios where cleanup is incomplete.
	if time.Since(c.startTime) < time.Second*10 {
		return nil
	}

	if isTimeout(time.Now(), c.lastLeaseRenewTime, c.timeoutSeconds) {
		logger.Info("Lease timeout", "cluster", c.clusterName, "lease", leaseName)
		err := c.handleTimeout(ctx)
		if err != nil {
			logger.Error(err, "Failed to handle lease timeout", "cluster", c.clusterName, "lease", leaseName)
		}
	}

	return nil
}

func isTimeout(reconcileTime, leaseRenewTime time.Time, timeoutSeconds int32) bool {
	return reconcileTime.After(leaseRenewTime.Add(time.Duration(timeoutSeconds) * time.Second))
}
