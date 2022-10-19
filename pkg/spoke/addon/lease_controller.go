package addon

import (
	"context"
	"fmt"
	"time"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	"open-cluster-management.io/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	coordv1client "k8s.io/client-go/kubernetes/typed/coordination/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"
)

const leaseDurationTimes = 5

// AddOnLeaseControllerLeaseDurationSeconds is exposed so that integration tests can crank up the lease update speed.
// TODO we may add this to ManagedClusterAddOn API to allow addon to adjust its own lease duration seconds
var AddOnLeaseControllerLeaseDurationSeconds = 60

// managedClusterAddOnLeaseController updates the managed cluster addons status on the hub cluster through checking the add-on
// lease on the managed/management cluster.
type managedClusterAddOnLeaseController struct {
	clusterName           string
	clock                 clock.Clock
	addOnClient           addonclient.Interface
	addOnLister           addonlisterv1alpha1.ManagedClusterAddOnLister
	managementLeaseClient coordv1client.CoordinationV1Interface
	spokeLeaseClient      coordv1client.CoordinationV1Interface
}

// NewManagedClusterAddOnLeaseController returns an instance of managedClusterAddOnLeaseController
func NewManagedClusterAddOnLeaseController(clusterName string,
	addOnClient addonclient.Interface,
	addOnInformer addoninformerv1alpha1.ManagedClusterAddOnInformer,
	managementLeaseClient coordv1client.CoordinationV1Interface,
	spokeLeaseClient coordv1client.CoordinationV1Interface,
	resyncInterval time.Duration,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterAddOnLeaseController{
		clusterName:           clusterName,
		clock:                 clock.RealClock{},
		addOnClient:           addOnClient,
		addOnLister:           addOnInformer.Lister(),
		managementLeaseClient: managementLeaseClient,
		spokeLeaseClient:      spokeLeaseClient,
	}

	// TODO We do not add leaser informer to support kubernetes version lower than 1.17. Lease v1 api
	// is introduced in v1.17, hence adding lease informer in this controller will cause the hang of
	// informer cache sync and result in fatal exit of this controller. The code will be factored
	// when we no longer support kubernetes version lower than 1.17.
	return factory.New().
		WithSync(c.sync).
		ResyncEvery(resyncInterval).
		ToController("ManagedClusterAddOnLeaseController", recorder)
}

func (c *managedClusterAddOnLeaseController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	queueKey := syncCtx.QueueKey()
	if queueKey == factory.DefaultQueueKey {
		addOns, err := c.addOnLister.ManagedClusterAddOns(c.clusterName).List(labels.Everything())
		if err != nil {
			return err
		}
		for _, addOn := range addOns {
			// enqueue the addon to reconcile
			syncCtx.Queue().Add(fmt.Sprintf("%s/%s", getAddOnInstallationNamespace(addOn), addOn.Name))
		}
		return nil
	}

	addOnNamespace, addOnName, err := cache.SplitMetaNamespaceKey(queueKey)
	if err != nil {
		// queue key is bad format, ignore it.
		return nil
	}

	addOn, err := c.addOnLister.ManagedClusterAddOns(c.clusterName).Get(addOnName)
	if errors.IsNotFound(err) {
		// addon is not found, could be deleted, ignore it.
		return nil
	}
	if err != nil {
		return err
	}

	// "Customized" mode health check is supposed to delegate the health checking
	// to the addon manager.
	if addOn.Status.HealthCheck.Mode == addonv1alpha1.HealthCheckModeCustomized {
		return nil
	}

	return c.syncSingle(ctx, addOnNamespace, addOn, syncCtx.Recorder())
}

func (c *managedClusterAddOnLeaseController) syncSingle(ctx context.Context,
	leaseNamespace string,
	addOn *addonv1alpha1.ManagedClusterAddOn,
	recorder events.Recorder) error {
	now := c.clock.Now()
	gracePeriod := time.Duration(leaseDurationTimes*AddOnLeaseControllerLeaseDurationSeconds) * time.Second

	// if the add-on agent is running on the managed cluster, try to fetch the add-on lease on the managed cluster,
	// otherwise (running outside of the managed cluster), fetch the add-on lease on the management cluster instead.
	leaseClient := c.spokeLeaseClient
	if isAddonRunningOutsideManagedCluster(addOn) {
		leaseClient = c.managementLeaseClient
	}

	// addon lease name should be same with the addon name.
	observedLease, err := leaseClient.Leases(leaseNamespace).Get(ctx, addOn.Name, metav1.GetOptions{})

	var condition metav1.Condition
	switch {
	case errors.IsNotFound(err):
		condition = metav1.Condition{
			Type:    addonv1alpha1.ManagedClusterAddOnConditionAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  "ManagedClusterAddOnLeaseNotFound",
			Message: fmt.Sprintf("The status of %s add-on is unknown.", addOn.Name),
		}
	case err != nil:
		return err
	case err == nil:
		if now.Before(observedLease.Spec.RenewTime.Add(gracePeriod)) {
			// the lease is constantly updated, update its addon status to available
			condition = metav1.Condition{
				Type:    addonv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  metav1.ConditionTrue,
				Reason:  "ManagedClusterAddOnLeaseUpdated",
				Message: fmt.Sprintf("%s add-on is available.", addOn.Name),
			}
			break
		}

		// the lease is not constantly updated, update its addon status to unavailable
		condition = metav1.Condition{
			Type:    addonv1alpha1.ManagedClusterAddOnConditionAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  "ManagedClusterAddOnLeaseUpdateStopped",
			Message: fmt.Sprintf("%s add-on is not available.", addOn.Name),
		}
	}

	if meta.IsStatusConditionPresentAndEqual(addOn.Status.Conditions, condition.Type, condition.Status) {
		// addon status is not changed, do nothing
		return nil
	}

	_, updated, err := helpers.UpdateManagedClusterAddOnStatus(
		ctx,
		c.addOnClient,
		c.clusterName,
		addOn.Name,
		helpers.UpdateManagedClusterAddOnStatusFn(condition),
	)
	if err != nil {
		return err
	}
	if updated {
		recorder.Eventf("ManagedClusterAddOnStatusUpdated",
			"update managed cluster addon %q available condition to %q with its lease %q/%q status",
			addOn.Name, condition.Status, leaseNamespace, addOn.Name)
	}

	return nil
}

func (c *managedClusterAddOnLeaseController) queueKeyFunc(lease runtime.Object) string {
	accessor, _ := meta.Accessor(lease)

	name := accessor.GetName()
	// addon lease name should be same with the addon name.
	addOn, err := c.addOnLister.ManagedClusterAddOns(c.clusterName).Get(name)
	if err != nil {
		// failed to get addon from hub, ignore this reconciliation.
		return ""
	}

	namespace := accessor.GetNamespace()
	if namespace != getAddOnInstallationNamespace(addOn) {
		// the lease namesapce is not same with its addon installation namespace, ignore it.
		return ""
	}

	return namespace + "/" + name
}
