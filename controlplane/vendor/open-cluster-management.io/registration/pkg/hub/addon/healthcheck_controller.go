package addon

import (
	"context"

	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// managedClusterAddonHealthCheckController udpates managed cluster addons status through watching the managed cluster status on
// the hub cluster.
type managedClusterAddOnHealthCheckController struct {
	addOnClient   addonclient.Interface
	addOnLister   addonlisterv1alpha1.ManagedClusterAddOnLister
	clusterLister clusterlisterv1.ManagedClusterLister
}

// NewManagedClusterAddOnHealthCheckController returns an instance of managedClusterAddOnHealthCheckController
func NewManagedClusterAddOnHealthCheckController(addOnClient addonclient.Interface,
	addOnInformer addoninformerv1alpha1.ManagedClusterAddOnInformer,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterAddOnHealthCheckController{
		addOnClient:   addOnClient,
		addOnLister:   addOnInformer.Lister(),
		clusterLister: clusterInformer.Lister(),
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterAddonHealthCheckController", recorder)
}

func (c *managedClusterAddOnHealthCheckController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	managedClusterName := syncCtx.QueueKey()
	managedCluster, err := c.clusterLister.Get(managedClusterName)
	if errors.IsNotFound(err) {
		// Managed cluster is not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	managedClusterAvailableCondition := meta.FindStatusCondition(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
	if managedClusterAvailableCondition == nil {
		// Managed cluster may not be accepted yet or managed cluster status may not be updated yet, do nothing.
		return nil
	}

	// For addons, we focus the managed cluster unknown status, managed cluster available condition has three status:
	// - True, the registration agent updates its lease constantly and the managed cluster kubeapi server is healthy
	// - False, the managed cluster kubeapi server is unhealthy
	// - Unknown, the registration agent stop to update its lease, the agent does not work now
	//
	// On the managed cluster, the registration agent is responsible for updating the status of addons, if the agent dose not
	// work (managed cluster is unknown), we set all of addons to unknown. If the managed cluster kubeapi server is unhealthy,
	// each managed cluster addon should handle this situation by itself.
	if managedClusterAvailableCondition.Status != metav1.ConditionUnknown {
		return nil
	}

	// Managed cluster is unknown, update its addons status
	addOns, err := c.addOnLister.ManagedClusterAddOns(managedClusterName).List(labels.Everything())
	if err != nil {
		return err
	}

	errs := []error{}
	for _, addOn := range addOns {
		_, updated, err := helpers.UpdateManagedClusterAddOnStatus(
			ctx,
			c.addOnClient,
			addOn.Namespace,
			addOn.Name,
			helpers.UpdateManagedClusterAddOnStatusFn(metav1.Condition{
				Type:    addonv1alpha1.ManagedClusterAddOnConditionAvailable,
				Status:  managedClusterAvailableCondition.Status,
				Reason:  managedClusterAvailableCondition.Reason,
				Message: managedClusterAvailableCondition.Message,
			}),
		)
		if err != nil {
			errs = append(errs, err)
		}
		if updated {
			syncCtx.Recorder().Eventf("ManagedClusterAddOnStatusUpdated", "update addon %q status to unknown on managed cluster %q",
				addOn.Name, managedClusterName)
		}
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}
