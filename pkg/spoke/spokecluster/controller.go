package spokecluster

import (
	"context"
	"fmt"
	"time"

	"github.com/open-cluster-management/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
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

// spokeClusterController reconciles instances of SpokeCluster on the spoke cluster.
type spokeClusterController struct {
	clusterName          string
	spokeServerURL       string
	spokeCABundle        []byte
	hubClusterClient     clientset.Interface
	hubClusterLister     clusterv1listers.SpokeClusterLister
	spokeDiscoveryClient discovery.DiscoveryInterface
	spokeNodeLister      corev1lister.NodeLister
}

// NewSpokeClusterController creates a new spoke cluster controller on the spoke cluster.
func NewSpokeClusterController(
	clusterName, spokeServerURL string,
	spokeCABundle []byte,
	hubClusterClient clientset.Interface,
	hubSpokeClusterInformer clusterv1informer.SpokeClusterInformer,
	spokeDiscoveryClient discovery.DiscoveryInterface,
	spokeNodeInformer corev1informers.NodeInformer,
	recorder events.Recorder) factory.Controller {
	c := &spokeClusterController{
		clusterName:          clusterName,
		spokeServerURL:       spokeServerURL,
		spokeCABundle:        spokeCABundle,
		hubClusterClient:     hubClusterClient,
		hubClusterLister:     hubSpokeClusterInformer.Lister(),
		spokeDiscoveryClient: spokeDiscoveryClient,
		spokeNodeLister:      spokeNodeInformer.Lister(),
	}
	return factory.New().
		WithInformers(hubSpokeClusterInformer.Informer(), spokeNodeInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(5*time.Minute).
		ToController("SpokeClusterController", recorder)
}

func (c spokeClusterController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	spokeCluster, err := c.hubClusterLister.Get(c.clusterName)
	if errors.IsNotFound(err) {
		return c.createSpokeCluster(ctx, syncCtx)
	}
	if err != nil {
		return fmt.Errorf("unable to get spoke cluster with name %q from hub: %w", c.clusterName, err)
	}

	// current spoke cluster is not accepted, do nothing.
	acceptedCondition := helpers.FindSpokeClusterCondition(spokeCluster.Status.Conditions, clusterv1.SpokeClusterConditionHubAccepted)
	if !helpers.IsConditionTrue(acceptedCondition) {
		klog.V(4).Infof("Spoke cluster %q is not accepted by hub yet", c.clusterName)
		return nil
	}

	// current spoke cluster is accepted, update its status if necessary.
	capacity, allocatable, err := c.getClusterResources()
	if err != nil {
		return fmt.Errorf("unable to get capacity and allocatable of spoke cluster %q: %w", c.clusterName, err)
	}

	spokeVersion, err := c.getSpokeVersion()
	if err != nil {
		return fmt.Errorf("unable to get server version of spoke cluster %q: %w", c.clusterName, err)
	}

	updateStatusFuncs := []helpers.UpdateSpokeClusterStatusFunc{}
	joinedCondition := helpers.FindSpokeClusterCondition(spokeCluster.Status.Conditions, clusterv1.SpokeClusterConditionJoined)
	joined := helpers.IsConditionTrue(joinedCondition)
	// current spoke cluster did not join the hub cluster, join it.
	if !joined {
		updateStatusFuncs = append(updateStatusFuncs, helpers.UpdateSpokeClusterConditionFn(clusterv1.StatusCondition{
			Type:    clusterv1.SpokeClusterConditionJoined,
			Status:  metav1.ConditionTrue,
			Reason:  "SpokeClusterJoined",
			Message: "Spoke cluster joined",
		}))
	}

	updateStatusFuncs = append(updateStatusFuncs, updateClusterResourcesFn(clusterv1.SpokeClusterStatus{
		Capacity:    capacity,
		Allocatable: allocatable,
		Version:     *spokeVersion,
	}))

	_, updated, err := helpers.UpdateSpokeClusterStatus(ctx, c.hubClusterClient, c.clusterName, updateStatusFuncs...)
	if err != nil {
		return fmt.Errorf("unable to update status of spoke cluster %q: %w", c.clusterName, err)
	}
	if updated {
		if !joined {
			syncCtx.Recorder().Eventf("SpokeClusterJoined", "Spoke cluster %q joined hub", c.clusterName)
		}
		klog.V(4).Infof("The status of spoke cluster %q has been updated", c.clusterName)
	}
	return nil
}

func (c *spokeClusterController) createSpokeCluster(ctx context.Context, syncCtx factory.SyncContext) error {
	spokeCluster := &clusterv1.SpokeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: c.clusterName,
		},
		Spec: clusterv1.SpokeClusterSpec{
			SpokeClientConfig: clusterv1.ClientConfig{
				URL:      c.spokeServerURL,
				CABundle: c.spokeCABundle,
			},
		},
	}

	if _, err := c.hubClusterClient.ClusterV1().SpokeClusters().Create(ctx, spokeCluster, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("unable to create spoke cluster with name %q on hub: %w", c.clusterName, err)
	}

	syncCtx.Recorder().Eventf("SpokeClusterCreated", "Spoke cluster %q created on hub", c.clusterName)
	return nil
}

func (c *spokeClusterController) getSpokeVersion() (*clusterv1.SpokeVersion, error) {
	serverVersion, err := c.spokeDiscoveryClient.ServerVersion()
	if err != nil {
		return nil, err
	}
	return &clusterv1.SpokeVersion{Kubernetes: serverVersion.String()}, nil
}

func (c *spokeClusterController) getClusterResources() (capacity, allocatable clusterv1.ResourceList, err error) {
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
			clusterv1.ResourceMemory: formatQuatityToMi(memoryCapacity),
		},
		clusterv1.ResourceList{
			clusterv1.ResourceCPU:    cpuAllocatable,
			clusterv1.ResourceMemory: formatQuatityToMi(memoryAllocatable),
		}, nil
}

func formatQuatityToMi(q resource.Quantity) resource.Quantity {
	raw, _ := q.AsInt64()
	raw /= (1024 * 1024)
	rq, err := resource.ParseQuantity(fmt.Sprintf("%dMi", raw))
	if err != nil {
		return q
	}
	return rq
}

func updateClusterResourcesFn(status clusterv1.SpokeClusterStatus) helpers.UpdateSpokeClusterStatusFunc {
	return func(oldStatus *clusterv1.SpokeClusterStatus) error {
		oldStatus.Capacity = status.Capacity
		oldStatus.Allocatable = status.Allocatable
		oldStatus.Version = status.Version
		return nil
	}
}
