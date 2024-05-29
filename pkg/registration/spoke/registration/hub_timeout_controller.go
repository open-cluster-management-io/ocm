package registration

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const leaseName = "managed-cluster-lease"

// The timeout controller is used to handle the case that the hub is not connectable.
// TODO: it should finally be part of the lease controller. @xuezhaojun
type hubTimeoutController struct {
	clusterName        string
	hubClient          clientset.Interface
	timeoutSeconds     int32
	lastLeaseRenewTime time.Time
	handleTimeout      func(ctx context.Context) error
}

func NewHubTimeoutController(
	clusterName string,
	hubClient clientset.Interface,
	timeoutSeconds int32,
	handleTimeout func(ctx context.Context) error,
	recorder events.Recorder,
) factory.Controller {
	c := &hubTimeoutController{
		clusterName:    clusterName,
		timeoutSeconds: timeoutSeconds,
		handleTimeout:  handleTimeout,
		hubClient:      hubClient,
	}
	return factory.New().WithSync(c.sync).ResyncEvery(time.Minute).
		ToController("HubTimeoutController", recorder)
}

func (c *hubTimeoutController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	logger := klog.FromContext(ctx)
	if c.handleTimeout == nil {
		return nil
	}

	lease, err := c.hubClient.CoordinationV1().Leases(c.clusterName).Get(ctx, leaseName, metav1.GetOptions{})
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
