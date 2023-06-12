package managedcluster

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/util/errors"
	discovery "k8s.io/client-go/discovery"
	corev1informers "k8s.io/client-go/informers/core/v1"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1alpha1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/pkg/common/patcher"
)

// managedClusterStatusController checks the kube-apiserver health on managed cluster to determine it whether is available
// and ensure that the managed cluster resources and version are up to date.
type managedClusterStatusController struct {
	clusterName      string
	reconcilers      []statusReconcile
	patcher          patcher.Patcher[*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus]
	hubClusterLister clusterv1listers.ManagedClusterLister
}

type statusReconcile interface {
	reconcile(ctx context.Context, cm *clusterv1.ManagedCluster) (*clusterv1.ManagedCluster, reconcileState, error)
}

type reconcileState int64

const (
	reconcileStop reconcileState = iota
	reconcileContinue
)

// NewManagedClusterStatusController creates a managed cluster status controller on managed cluster.
func NewManagedClusterStatusController(
	clusterName string,
	hubClusterClient clientset.Interface,
	hubClusterInformer clusterv1informer.ManagedClusterInformer,
	managedClusterDiscoveryClient discovery.DiscoveryInterface,
	claimInformer clusterv1alpha1informer.ClusterClaimInformer,
	nodeInformer corev1informers.NodeInformer,
	maxCustomClusterClaims int,
	resyncInterval time.Duration,
	recorder events.Recorder) factory.Controller {
	c := newManagedClusterStatusController(
		clusterName,
		hubClusterClient,
		hubClusterInformer,
		managedClusterDiscoveryClient,
		claimInformer,
		nodeInformer,
		maxCustomClusterClaims,
		recorder,
	)

	return factory.New().
		WithInformers(hubClusterInformer.Informer(), nodeInformer.Informer(), claimInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(resyncInterval).
		ToController("ManagedClusterStatusController", recorder)
}

func newManagedClusterStatusController(
	clusterName string,
	hubClusterClient clientset.Interface,
	hubClusterInformer clusterv1informer.ManagedClusterInformer,
	managedClusterDiscoveryClient discovery.DiscoveryInterface,
	claimInformer clusterv1alpha1informer.ClusterClaimInformer,
	nodeInformer corev1informers.NodeInformer,
	maxCustomClusterClaims int,
	recorder events.Recorder) *managedClusterStatusController {
	return &managedClusterStatusController{
		clusterName: clusterName,
		patcher: patcher.NewPatcher[
			*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
			hubClusterClient.ClusterV1().ManagedClusters()),
		reconcilers: []statusReconcile{
			&joiningReconcile{recorder: recorder},
			&resoureReconcile{managedClusterDiscoveryClient: managedClusterDiscoveryClient, nodeLister: nodeInformer.Lister()},
			&claimReconcile{claimLister: claimInformer.Lister(), recorder: recorder, maxCustomClusterClaims: maxCustomClusterClaims},
		},
		hubClusterLister: hubClusterInformer.Lister(),
	}
}

// sync updates managed cluster available condition by checking kube-apiserver health on managed cluster.
// if the kube-apiserver is health, it will ensure that managed cluster resources and version are up to date.
func (c *managedClusterStatusController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	cluster, err := c.hubClusterLister.Get(c.clusterName)
	if err != nil {
		return fmt.Errorf("unable to get managed cluster %q from hub: %w", c.clusterName, err)
	}

	newCluster := cluster.DeepCopy()
	var errs []error
	for _, reconciler := range c.reconcilers {
		var state reconcileState
		newCluster, state, err = reconciler.reconcile(ctx, newCluster)
		if err != nil {
			errs = append(errs, err)
		}
		if state == reconcileStop {
			break
		}
	}

	if _, err := c.patcher.PatchStatus(ctx, newCluster, newCluster.Status, cluster.Status); err != nil {
		errs = append(errs, err)
	}

	return errors.NewAggregate(errs)
}
