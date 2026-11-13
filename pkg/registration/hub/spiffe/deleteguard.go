// Package spiffe holds the hub controllers of the spiffe registration driver.
package spiffe

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

// RevocationFinalizer holds a deleted ManagedCluster until every SVID issued before the deletion
// has expired, so the cluster's agent can't recreate it with such an SVID.
const RevocationFinalizer = "open-cluster-management.io/spiffe-revocation"

// deleteGuard keeps RevocationFinalizer on a deleted cluster until
// deletionTimestamp + maxSVIDLifetime + maxClockSkew. While the object exists, its name can't be
// reused, and the gRPC server's cluster gate refuses the terminating cluster's agents.
//
// It guards the clusters that are pinned to a trust domain (the gRPC cluster service sets the
// open-cluster-management.io/spiffe-trust-domain annotation when a SPIFFE agent creates its
// cluster, and clusteradm accept sets it on clusters moved from another driver), and every
// cluster when spiffe is the only registration driver. A cluster of another driver isn't held
// for maxSVIDLifetime when it is deleted.
//
// It holds only the object: the other controllers clean up the cluster's RBAC and namespace as
// usual.
type deleteGuard struct {
	patcher     patcher.Patcher[*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus]
	lister      clusterlisterv1.ManagedClusterLister
	lifetime    time.Duration
	skew        time.Duration
	allClusters bool
	now         func() time.Time
}

// NewDeleteGuardController returns the delete guard. spiffeOnly is true when spiffe is the only
// enabled registration driver.
func NewDeleteGuardController(
	clusterClient clusterclient.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	maxSVIDLifetime, maxClockSkew time.Duration,
	spiffeOnly bool,
) factory.Controller {
	g := &deleteGuard{
		patcher: patcher.NewPatcher[
			*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
			clusterClient.ClusterV1().ManagedClusters()),
		lister:      clusterInformer.Lister(),
		lifetime:    maxSVIDLifetime,
		skew:        maxClockSkew,
		allClusters: spiffeOnly,
		now:         time.Now,
	}
	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, clusterInformer.Informer()).
		WithSync(g.sync).
		ToController("SPIFFEDeleteGuardController")
}

func (g *deleteGuard) guards(c *clusterv1.ManagedCluster) bool {
	if g.allClusters {
		return true
	}
	_, pinned := c.Annotations[clusterv1.SPIFFETrustDomainAnnotationKey]
	return pinned
}

func (g *deleteGuard) sync(ctx context.Context, syncCtx factory.SyncContext, name string) error {
	c, err := g.lister.Get(name)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if c.DeletionTimestamp.IsZero() {
		if !g.guards(c) {
			return nil
		}
		_, err := g.patcher.AddFinalizer(ctx, c, RevocationFinalizer)
		return err
	}

	release := c.DeletionTimestamp.Add(g.lifetime + g.skew)
	if wait := release.Sub(g.now()); wait > 0 {
		klog.FromContext(ctx).V(4).Info("holding deleted cluster until its SVIDs have expired",
			"cluster", name, "release", release)
		// Come back at release time; no polling.
		syncCtx.Queue().AddAfter(name, wait)
		return nil
	}
	return g.patcher.RemoveFinalizer(ctx, c, RevocationFinalizer)
}
