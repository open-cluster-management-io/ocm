package managedcluster

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/discovery"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	kevents "k8s.io/client-go/tools/events"
	aboutv1alpha1informer "sigs.k8s.io/about-api/pkg/generated/informers/externalversions/apis/v1alpha1"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1alpha1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/features"
)

// managedClusterStatusController checks the kube-apiserver health on managed cluster to determine it whether is available
// and ensure that the managed cluster resources and version are up to date.
type managedClusterStatusController struct {
	clusterName      string
	reconcilers      []statusReconcile
	patcher          patcher.Patcher[*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus]
	hubClusterLister clusterv1listers.ManagedClusterLister
	hubEventRecorder kevents.EventRecorder
}

type statusReconcile interface {
	reconcile(ctx context.Context, syncCtx factory.SyncContext, cm *clusterv1.ManagedCluster) (*clusterv1.ManagedCluster, reconcileState, error)
}

type reconcileState int64

const (
	reconcileStop reconcileState = iota
	reconcileContinue
)

// NewManagedClusterStatusController creates a managed cluster status controller on managed cluster.
func NewManagedClusterStatusController(
	clusterName string,
	hubHash string,
	hubClusterClient clientset.Interface,
	spokeKubeClient kubernetes.Interface,
	hubClusterInformer clusterv1informer.ManagedClusterInformer,
	spokeNamespaceInformer corev1informers.NamespaceInformer,
	managedClusterDiscoveryClient discovery.DiscoveryInterface,
	claimInformer clusterv1alpha1informer.ClusterClaimInformer,
	propertyInformer aboutv1alpha1informer.ClusterPropertyInformer,
	nodeInformer corev1informers.NodeInformer,
	maxCustomClusterClaims int,
	reservedClusterClaimSuffixes []string,
	resyncInterval time.Duration,
	hubEventRecorder kevents.EventRecorder) factory.Controller {
	c := newManagedClusterStatusController(
		clusterName,
		hubHash,
		hubClusterClient,
		spokeKubeClient,
		hubClusterInformer,
		spokeNamespaceInformer,
		managedClusterDiscoveryClient,
		claimInformer,
		propertyInformer,
		nodeInformer,
		maxCustomClusterClaims,
		reservedClusterClaimSuffixes,
		hubEventRecorder,
	)

	controllerFactory := factory.New().
		WithInformers(hubClusterInformer.Informer(), nodeInformer.Informer(), spokeNamespaceInformer.Informer()).
		WithSync(c.sync).ResyncEvery(resyncInterval)

	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.ClusterClaim) {
		controllerFactory = controllerFactory.WithInformers(claimInformer.Informer())
	}
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.ClusterProperty) {
		controllerFactory = controllerFactory.WithInformers(propertyInformer.Informer())
	}

	return controllerFactory.ToController("ManagedClusterStatusController")
}

func newManagedClusterStatusController(
	clusterName string,
	hubHash string,
	hubClusterClient clientset.Interface,
	spokeKubeClient kubernetes.Interface,
	hubClusterInformer clusterv1informer.ManagedClusterInformer,
	spokeNamespaceInformer corev1informers.NamespaceInformer,
	managedClusterDiscoveryClient discovery.DiscoveryInterface,
	claimInformer clusterv1alpha1informer.ClusterClaimInformer,
	propertyInformer aboutv1alpha1informer.ClusterPropertyInformer,
	nodeInformer corev1informers.NodeInformer,
	maxCustomClusterClaims int,
	reservedClusterClaimSuffixes []string,
	hubEventRecorder kevents.EventRecorder) *managedClusterStatusController {
	return &managedClusterStatusController{
		clusterName: clusterName,
		patcher: patcher.NewPatcher[
			*clusterv1.ManagedCluster, clusterv1.ManagedClusterSpec, clusterv1.ManagedClusterStatus](
			hubClusterClient.ClusterV1().ManagedClusters()),
		reconcilers: []statusReconcile{
			&joiningReconcile{},
			&resoureReconcile{managedClusterDiscoveryClient: managedClusterDiscoveryClient, nodeLister: nodeInformer.Lister()},
			&claimReconcile{claimLister: claimInformer.Lister(),
				maxCustomClusterClaims:       maxCustomClusterClaims,
				reservedClusterClaimSuffixes: reservedClusterClaimSuffixes,
				aboutLister:                  propertyInformer.Lister(),
			},
			&managedNamespaceReconcile{
				hubClusterSetLabel:   GetHubClusterSetLabel(hubHash),
				spokeKubeClient:      spokeKubeClient,
				spokeNamespaceLister: spokeNamespaceInformer.Lister(),
			},
		},
		hubClusterLister: hubClusterInformer.Lister(),
		hubEventRecorder: hubEventRecorder,
	}
}

// sync updates managed cluster available condition by checking kube-apiserver health on managed cluster.
// if the kube-apiserver is health, it will ensure that managed cluster resources and version are up to date.
func (c *managedClusterStatusController) sync(ctx context.Context, syncCtx factory.SyncContext, _ string) error {
	cluster, err := c.hubClusterLister.Get(c.clusterName)
	if err != nil {
		return fmt.Errorf("unable to get managed cluster %q from hub: %w", c.clusterName, err)
	}

	newCluster := cluster.DeepCopy()
	var errs []error
	for _, reconciler := range c.reconcilers {
		var state reconcileState
		newCluster, state, err = reconciler.reconcile(ctx, syncCtx, newCluster)
		if err != nil {
			errs = append(errs, err)
		}
		if state == reconcileStop {
			break
		}
	}

	// check if managedcluster's clock is out of sync, if so, the agent will not be able to update the status of managed cluster.
	outOfSynced := meta.IsStatusConditionFalse(newCluster.Status.Conditions, clusterv1.ManagedClusterConditionClockSynced)
	if outOfSynced {
		syncCtx.Recorder().Eventf(ctx, "ClockOutOfSync", "The managed cluster's clock is out of sync, the agent will not be able to update the status of managed cluster.")
		return fmt.Errorf("the managed cluster's clock is out of sync, the agent will not be able to update the status of managed cluster")
	}

	changed, err := c.patcher.PatchStatus(ctx, newCluster, newCluster.Status, cluster.Status)
	if err != nil {
		errs = append(errs, err)
	}
	if changed {
		c.sendAvailableConditionEvent(cluster, newCluster)
	}
	return errors.NewAggregate(errs)
}

func (c *managedClusterStatusController) sendAvailableConditionEvent(
	cluster, newCluster *clusterv1.ManagedCluster) {
	condition := meta.FindStatusCondition(cluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
	newCondition := meta.FindStatusCondition(newCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
	if newCondition == nil {
		return
	}
	if condition != nil && condition.Status == newCondition.Status {

		return
	}

	// send event to hub cluster in cluster namespace
	newCluster.SetNamespace(newCluster.Name)
	switch newCondition.Status {
	case metav1.ConditionTrue:
		c.hubEventRecorder.Eventf(newCluster, nil, corev1.EventTypeNormal, "Available", "Available",
			"The %s is successfully imported, and it is managed by the hub cluster. Its apieserver is available",
			cluster.Name)

	case metav1.ConditionFalse:
		c.hubEventRecorder.Eventf(newCluster, nil, corev1.EventTypeWarning, "Unavailable", "Unavailable",
			"The %s is successfully imported. However, its Kube API server is unavailable", cluster.Name)
	}
}
