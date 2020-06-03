package managedcluster

import (
	"context"
	"fmt"
	"time"

	"github.com/open-cluster-management/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog"

	clientset "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterv1informer "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	discovery "k8s.io/client-go/discovery"
)

// managedClusterController reconciles instances of ManagedCluster on the managed cluster.
type managedClusterController struct {
	clusterName          string
	hubClusterClient     clientset.Interface
	hubClusterLister     clusterv1listers.ManagedClusterLister
	spokeDiscoveryClient discovery.DiscoveryInterface
	spokeNodeLister      corev1lister.NodeLister
}

// NewManagedClusterController creates a new ManagedCluster controller on the managed cluster.
func NewManagedClusterController(
	clusterName string,
	hubClusterClient clientset.Interface,
	hubManagedClusterInformer clusterv1informer.ManagedClusterInformer,
	spokeDiscoveryClient discovery.DiscoveryInterface,
	spokeNodeInformer corev1informers.NodeInformer,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterController{
		clusterName:          clusterName,
		hubClusterClient:     hubClusterClient,
		hubClusterLister:     hubManagedClusterInformer.Lister(),
		spokeDiscoveryClient: spokeDiscoveryClient,
		spokeNodeLister:      spokeNodeInformer.Lister(),
	}

	return factory.New().
		// TODO need to have conditional node capacity recalculation based on number of nodes here and decoupling
		// the node capacity calculation to another controller.
		WithInformers(hubManagedClusterInformer.Informer(), spokeNodeInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(5*time.Minute).
		ToController("ManagedClusterController", recorder)
}

// sync maintains the spoke-side status of a ManagedCluster, it maintains the ManagedClusterJoined condition according to
// the value of the ManagedClusterHubAccepted condition and ensures resource and version are up to date.
func (c managedClusterController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	spokeCluster, err := c.hubClusterLister.Get(c.clusterName)
	if err != nil {
		return fmt.Errorf("unable to get managed cluster with name %q from hub: %w", c.clusterName, err)
	}

	// current managed cluster is not accepted, do nothing.
	acceptedCondition := helpers.FindManagedClusterCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted)
	if !helpers.IsConditionTrue(acceptedCondition) {
		syncCtx.Recorder().Eventf("ManagedClusterIsNotAccepted", "Managed cluster %q is not accepted by hub yet", c.clusterName)
		return nil
	}

	// current managed cluster is accepted, update its status if necessary.
	capacity, allocatable, err := c.getClusterResources()
	if err != nil {
		return fmt.Errorf("unable to get capacity and allocatable of managed cluster %q: %w", c.clusterName, err)
	}

	spokeVersion, err := c.getSpokeVersion()
	if err != nil {
		return fmt.Errorf("unable to get server version of managed cluster %q: %w", c.clusterName, err)
	}

	updateStatusFuncs := []helpers.UpdateManagedClusterStatusFunc{}
	joinedCondition := helpers.FindManagedClusterCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined)
	joined := helpers.IsConditionTrue(joinedCondition)
	// current managed cluster did not join the hub cluster, join it.
	if !joined {
		updateStatusFuncs = append(updateStatusFuncs, helpers.UpdateManagedClusterConditionFn(clusterv1.StatusCondition{
			Type:    clusterv1.ManagedClusterConditionJoined,
			Status:  metav1.ConditionTrue,
			Reason:  "ManagedClusterJoined",
			Message: "Managed cluster joined",
		}))
	}

	updateStatusFuncs = append(updateStatusFuncs, updateClusterResourcesFn(clusterv1.ManagedClusterStatus{
		Capacity:    capacity,
		Allocatable: allocatable,
		Version:     *spokeVersion,
	}))

	_, updated, err := helpers.UpdateManagedClusterStatus(ctx, c.hubClusterClient, c.clusterName, updateStatusFuncs...)
	if err != nil {
		return fmt.Errorf("unable to update status of managed cluster %q: %w", c.clusterName, err)
	}
	if updated {
		if !joined {
			syncCtx.Recorder().Eventf("ManagedClusterJoined", "Managed cluster %q joined hub", c.clusterName)
		}
		klog.V(4).Infof("The status of managed cluster %q has been updated", c.clusterName)
	}
	return nil
}

func (c *managedClusterController) getSpokeVersion() (*clusterv1.ManagedClusterVersion, error) {
	serverVersion, err := c.spokeDiscoveryClient.ServerVersion()
	if err != nil {
		return nil, err
	}
	return &clusterv1.ManagedClusterVersion{Kubernetes: serverVersion.String()}, nil
}

func (c *managedClusterController) getClusterResources() (capacity, allocatable clusterv1.ResourceList, err error) {
	nodes, err := c.spokeNodeLister.List(labels.Everything())
	if err != nil {
		return nil, nil, err
	}

	cpuCapacity := *resource.NewQuantity(int64(0), resource.DecimalSI)
	memoryCapacity := *resource.NewQuantity(int64(0), resource.BinarySI)
	cpuAllocatable := *resource.NewQuantity(int64(0), resource.DecimalSI)
	memoryAllocatable := *resource.NewQuantity(int64(0), resource.BinarySI)
	for _, node := range nodes {
		cpuCapacity.Add(*node.Status.Capacity.Cpu())
		memoryCapacity.Add(*node.Status.Capacity.Memory())
		cpuAllocatable.Add(*node.Status.Allocatable.Cpu())
		memoryAllocatable.Add(*node.Status.Allocatable.Memory())
	}

	return clusterv1.ResourceList{
			clusterv1.ResourceCPU:    cpuCapacity,
			clusterv1.ResourceMemory: formatQuantityToMi(memoryCapacity),
		},
		clusterv1.ResourceList{
			clusterv1.ResourceCPU:    cpuAllocatable,
			clusterv1.ResourceMemory: formatQuantityToMi(memoryAllocatable),
		}, nil
}

func formatQuantityToMi(q resource.Quantity) resource.Quantity {
	raw, _ := q.AsInt64()
	raw /= (1024 * 1024)
	rq, err := resource.ParseQuantity(fmt.Sprintf("%dMi", raw))
	if err != nil {
		return q
	}
	return rq
}

func updateClusterResourcesFn(status clusterv1.ManagedClusterStatus) helpers.UpdateManagedClusterStatusFunc {
	return func(oldStatus *clusterv1.ManagedClusterStatus) error {
		oldStatus.Capacity = status.Capacity
		oldStatus.Allocatable = status.Allocatable
		oldStatus.Version = status.Version
		return nil
	}
}
