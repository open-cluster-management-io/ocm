package managedcluster

import (
	"context"
	"fmt"
	"time"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	discovery "k8s.io/client-go/discovery"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
)

// managedClusterJoiningController add the joined condition to a ManagedCluster on the managed cluster after it is accepted by hub cluster admin.
type managedClusterJoiningController struct {
	clusterName      string
	hubClusterClient clientset.Interface
	hubClusterLister clusterv1listers.ManagedClusterLister
	discoveryClient  discovery.DiscoveryInterface
	nodeLister       corev1lister.NodeLister
}

// NewManagedClusterJoiningController creates a new managed cluster joining controller on the managed cluster.
func NewManagedClusterJoiningController(
	clusterName string,
	hubClusterClient clientset.Interface,
	hubManagedClusterInformer clusterv1informer.ManagedClusterInformer,
	discoveryClient discovery.DiscoveryInterface,
	nodeInformer corev1informers.NodeInformer,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterJoiningController{
		clusterName:      clusterName,
		hubClusterClient: hubClusterClient,
		hubClusterLister: hubManagedClusterInformer.Lister(),
		discoveryClient:  discoveryClient,
		nodeLister:       nodeInformer.Lister(),
	}

	return factory.New().
		// TODO need to have conditional node capacity recalculation based on number of nodes here and decoupling
		// the node capacity calculation to another controller.
		WithInformers(hubManagedClusterInformer.Informer(), nodeInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(5*time.Minute).
		ToController("ManagedClusterController", recorder)
}

// sync maintains the managed cluster side status of a ManagedCluster, it maintains the ManagedClusterJoined condition according to
// the value of the ManagedClusterHubAccepted condition and ensures resource and version are up to date.
func (c managedClusterJoiningController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	managedCluster, err := c.hubClusterLister.Get(c.clusterName)
	if err != nil {
		return fmt.Errorf("unable to get managed cluster with name %q from hub: %w", c.clusterName, err)
	}

	// current managed cluster is not accepted, do nothing.
	if !meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
		syncCtx.Recorder().Eventf("ManagedClusterIsNotAccepted", "Managed cluster %q is not accepted by hub yet", c.clusterName)
		return nil
	}

	// current managed cluster is accepted, update its status if necessary.
	capacity, allocatable, err := c.getClusterResources()
	if err != nil {
		return fmt.Errorf("unable to get capacity and allocatable of managed cluster %q: %w", c.clusterName, err)
	}

	clusterVersion, err := c.getClusterVersion()
	if err != nil {
		return fmt.Errorf("unable to get server version of managed cluster %q: %w", c.clusterName, err)
	}

	updateStatusFuncs := []helpers.UpdateManagedClusterStatusFunc{}
	// current managed cluster did not join the hub cluster, join it.
	joined := meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined)
	if !joined {
		updateStatusFuncs = append(updateStatusFuncs, helpers.UpdateManagedClusterConditionFn(metav1.Condition{
			Type:    clusterv1.ManagedClusterConditionJoined,
			Status:  metav1.ConditionTrue,
			Reason:  "ManagedClusterJoined",
			Message: "Managed cluster joined",
		}))
	}

	updateStatusFuncs = append(updateStatusFuncs, updateClusterResourcesFn(clusterv1.ManagedClusterStatus{
		Capacity:    capacity,
		Allocatable: allocatable,
		Version:     *clusterVersion,
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

func (c *managedClusterJoiningController) getClusterVersion() (*clusterv1.ManagedClusterVersion, error) {
	serverVersion, err := c.discoveryClient.ServerVersion()
	if err != nil {
		return nil, err
	}
	return &clusterv1.ManagedClusterVersion{Kubernetes: serverVersion.String()}, nil
}

func (c *managedClusterJoiningController) getClusterResources() (capacity, allocatable clusterv1.ResourceList, err error) {
	nodes, err := c.nodeLister.List(labels.Everything())
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
		// merge the old capacity to new capacity, if one old capacity entry does not exist in new capacity,
		// we add it back to new capacity
		for key, val := range oldStatus.Capacity {
			if _, ok := status.Capacity[key]; !ok {
				status.Capacity[key] = val
				continue
			}
		}
		oldStatus.Capacity = status.Capacity
		oldStatus.Allocatable = status.Allocatable
		oldStatus.Version = status.Version
		return nil
	}
}
