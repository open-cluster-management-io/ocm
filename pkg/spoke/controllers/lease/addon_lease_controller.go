package lease

import (
	"context"
	"time"

	addonv1alpha1client "github.com/open-cluster-management/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "github.com/open-cluster-management/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "github.com/open-cluster-management/api/client/addon/listers/addon/v1alpha1"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	coordinformers "k8s.io/client-go/informers/coordination/v1"
	coordlisters "k8s.io/client-go/listers/coordination/v1"
)

const (
	addonLeaseDurationTimes   = 5
	AddonLeaseDurationSeconds = 60
)

// leaseController checks the lease of managed clusters on hub cluster to determine whether a managed cluster is available.
type addonLeaseController struct {
	clusterName string
	addonClient addonv1alpha1client.Interface
	addonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	leaseLister coordlisters.LeaseLister
}

// NewClusterLeaseController creates a cluster lease controller on hub cluster.
func NewAddonLeaseController(
	clusterName string,
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	leaseInformer coordinformers.LeaseInformer,
	resyncInterval time.Duration,
	recorder events.Recorder) factory.Controller {
	c := &addonLeaseController{
		clusterName: clusterName,
		addonClient: addonClient,
		addonLister: addonInformers.Lister(),
		leaseLister: leaseInformer.Lister(),
	}
	return factory.New().
		WithInformers(addonInformers.Informer(), leaseInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(resyncInterval).
		ToController("ManagedClusterLeaseController", recorder)
}

// sync checks the lease of each accepted cluster on hub to determine whether a managed cluster is available.
func (c *addonLeaseController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	//TODO: implement the reconciliation logic
	return nil
}
