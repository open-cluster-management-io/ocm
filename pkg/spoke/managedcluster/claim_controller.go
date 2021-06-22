package managedcluster

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/selection"
	"sort"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1alpha1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1alpha1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	"open-cluster-management.io/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

const labelCustomizedOnly = "open-cluster-management.io/spoke-only"

// managedClusterClaimController exposes cluster claims created on managed cluster on hub after it joins the hub.
type managedClusterClaimController struct {
	clusterName            string
	hubClusterClient       clientset.Interface
	hubClusterLister       clusterv1listers.ManagedClusterLister
	claimLister            clusterv1alpha1listers.ClusterClaimLister
	maxCustomClusterClaims int
}

// NewManagedClusterClaimController creates a new managed cluster claim controller on the managed cluster.
func NewManagedClusterClaimController(
	clusterName string,
	maxCustomClusterClaims int,
	hubClusterClient clientset.Interface,
	hubManagedClusterInformer clusterv1informer.ManagedClusterInformer,
	claimInformer clusterv1alpha1informer.ClusterClaimInformer,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterClaimController{
		clusterName:            clusterName,
		maxCustomClusterClaims: maxCustomClusterClaims,
		hubClusterClient:       hubClusterClient,
		hubClusterLister:       hubManagedClusterInformer.Lister(),
		claimLister:            claimInformer.Lister(),
	}

	return factory.New().
		WithInformers(claimInformer.Informer()).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, hubManagedClusterInformer.Informer()).
		WithSync(c.sync).
		ToController("ClusterClaimController", recorder)
}

// sync maintains the cluster claims in status of the managed cluster on hub once it joins the hub.
func (c managedClusterClaimController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	managedCluster, err := c.hubClusterLister.Get(c.clusterName)
	if err != nil {
		return fmt.Errorf("unable to get managed cluster with name %q from hub: %w", c.clusterName, err)
	}

	// current managed cluster has not joined the hub yet, do nothing.
	if !meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
		syncCtx.Recorder().Eventf("ManagedClusterIsNotAccepted", "Managed cluster %q does not join the hub yet", c.clusterName)
		return nil
	}

	return c.exposeClaims(ctx, syncCtx, managedCluster)
}

// exposeClaims saves cluster claims fetched on managed cluster into status of the
// managed cluster on hub. Some of the customized claims might not be exposed once
// the total number of the claims exceeds the value of `cluster-claims-max`.
func (c managedClusterClaimController) exposeClaims(ctx context.Context, syncCtx factory.SyncContext,
	managedCluster *clusterv1.ManagedCluster) error {
	reservedClaims := []clusterv1.ManagedClusterClaim{}
	customClaims := []clusterv1.ManagedClusterClaim{}

	// clusterClaim with label `open-cluster-management.io/spoke-only` will not be synced to managedCluster.Status at hub.
	requirement, _ := labels.NewRequirement(labelCustomizedOnly, selection.DoesNotExist, []string{})
	selector := labels.NewSelector().Add(*requirement)
	clusterClaims, err := c.claimLister.List(selector)
	if err != nil {
		return fmt.Errorf("unable to list cluster claims: %w", err)
	}

	reservedClaimNames := sets.NewString(clusterv1alpha1.ReservedClusterClaimNames[:]...)
	for _, clusterClaim := range clusterClaims {
		managedClusterClaim := clusterv1.ManagedClusterClaim{
			Name:  clusterClaim.Name,
			Value: clusterClaim.Spec.Value,
		}
		if reservedClaimNames.Has(clusterClaim.Name) {
			reservedClaims = append(reservedClaims, managedClusterClaim)
			continue
		}
		customClaims = append(customClaims, managedClusterClaim)
	}

	// sort claims by name
	sort.SliceStable(reservedClaims, func(i, j int) bool {
		return reservedClaims[i].Name < reservedClaims[j].Name
	})

	sort.SliceStable(customClaims, func(i, j int) bool {
		return customClaims[i].Name < customClaims[j].Name
	})

	// truncate custom claims if the number exceeds `max-custom-cluster-claims`
	if n := len(customClaims); n > c.maxCustomClusterClaims {
		customClaims = customClaims[:c.maxCustomClusterClaims]
		syncCtx.Recorder().Eventf("CustomClusterClaimsTruncated", "%d cluster claims are found. It exceeds the max number of custom cluster claims (%d). %d custom cluster claims are not exposed.",
			n, c.maxCustomClusterClaims, n-c.maxCustomClusterClaims)
	}

	// merge reserved claims and custom claims
	claims := append(reservedClaims, customClaims...)

	// update the status of the managed cluster
	updateStatusFuncs := []helpers.UpdateManagedClusterStatusFunc{updateClusterClaimsFn(clusterv1.ManagedClusterStatus{
		ClusterClaims: claims,
	})}

	_, updated, err := helpers.UpdateManagedClusterStatus(ctx, c.hubClusterClient, c.clusterName, updateStatusFuncs...)
	if err != nil {
		return fmt.Errorf("unable to update status of managed cluster %q: %w", c.clusterName, err)
	}
	if updated {
		klog.V(4).Infof("The cluster claims in status of managed cluster %q has been updated", c.clusterName)
	}
	return nil
}

func updateClusterClaimsFn(status clusterv1.ManagedClusterStatus) helpers.UpdateManagedClusterStatusFunc {
	return func(oldStatus *clusterv1.ManagedClusterStatus) error {
		oldStatus.ClusterClaims = status.ClusterClaims
		return nil
	}
}
