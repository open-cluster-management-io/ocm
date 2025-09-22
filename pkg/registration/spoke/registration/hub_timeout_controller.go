package registration

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	leasev1client "k8s.io/client-go/kubernetes/typed/coordination/v1"
	"k8s.io/klog/v2"
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
	recorder events.Recorder,
) factory.Controller {
	c := &hubTimeoutController{
		clusterName:    clusterName,
		timeoutSeconds: timeoutSeconds,
		handleTimeout:  handleTimeout,
		leaseClient:    leaseClient,
		startTime:      time.Now(),
	}
	return factory.New().WithSync(c.sync).ResyncEvery(time.Minute).
		ToController("HubTimeoutController", recorder)
}

func (c *hubTimeoutController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	logger := klog.FromContext(ctx)
	if c.handleTimeout == nil {
		return nil
	}

	// If `startTime` within 60s, skip the timeout check.
	// This is to wait for the first lease update. Otherwise, if old lease is already expired, it will be treated as timeout immediately.
	if time.Since(c.startTime) < time.Minute {
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
