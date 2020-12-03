package managedcluster

import (
	"context"
	"fmt"
	"sort"

	clientset "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterv1informer "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1alpha1informer "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterv1listers "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1"
	clusterv1alpha1listers "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1alpha1"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	clusterv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
	"github.com/open-cluster-management/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

// managedClusterClaimController exposes cluster claims created on managed cluster on hub after it joins the hub.
type managedClusterClaimController struct {
	clusterName      string
	hubClusterClient clientset.Interface
	hubClusterLister clusterv1listers.ManagedClusterLister
	claimLister      clusterv1alpha1listers.ClusterClaimLister
	maxClusterClaims int
}

// NewManagedClusterClaimController creates a new managed cluster claim controller on the managed cluster.
func NewManagedClusterClaimController(
	clusterName string,
	maxClusterClaims int,
	hubClusterClient clientset.Interface,
	hubManagedClusterInformer clusterv1informer.ManagedClusterInformer,
	claimInformer clusterv1alpha1informer.ClusterClaimInformer,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterClaimController{
		clusterName:      clusterName,
		maxClusterClaims: maxClusterClaims,
		hubClusterClient: hubClusterClient,
		hubClusterLister: hubManagedClusterInformer.Lister(),
		claimLister:      claimInformer.Lister(),
	}

	return factory.New().
		WithInformers(claimInformer.Informer()).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, hubManagedClusterInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterClaimController", recorder)
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
	customizedClaims := []clusterv1.ManagedClusterClaim{}
	clusterClaims, err := c.claimLister.List(labels.Everything())
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
		customizedClaims = append(customizedClaims, managedClusterClaim)
	}

	// sort claims by name
	sort.SliceStable(reservedClaims, func(i, j int) bool {
		return reservedClaims[i].Name < reservedClaims[j].Name
	})

	sort.SliceStable(customizedClaims, func(i, j int) bool {
		return customizedClaims[i].Name < customizedClaims[j].Name
	})

	// merge and truncated claims
	claims := append(reservedClaims, customizedClaims...)
	if total := len(claims); total > c.maxClusterClaims {
		claims = claims[:c.maxClusterClaims]
		syncCtx.Recorder().Eventf("ExposedClusterClaimsTruncated", "%d cluster claims are found. It exceeds the max cluster claims number (%d). %d cluster claims are not exposed.",
			total, c.maxClusterClaims, total-c.maxClusterClaims)
	}

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
