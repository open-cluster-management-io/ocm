package managedcluster

import (
	"context"
	"fmt"
	"net/http"
	"time"

	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterv1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/helpers"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	discovery "k8s.io/client-go/discovery"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1lister "k8s.io/client-go/listers/core/v1"
)

// managedClusterStatusController checks the kube-apiserver health on managed cluster to determine it whether is available
// and ensure that the managed cluster resources and version are up to date.
type managedClusterStatusController struct {
	clusterName                   string
	hubClusterClient              clientset.Interface
	hubClusterLister              clusterv1listers.ManagedClusterLister
	managedClusterDiscoveryClient discovery.DiscoveryInterface
	nodeLister                    corev1lister.NodeLister
}

// NewManagedClusterStatusController creates a managed cluster status controller on managed cluster.
func NewManagedClusterStatusController(
	clusterName string,
	hubClusterClient clientset.Interface,
	hubClusterInformer clusterv1informer.ManagedClusterInformer,
	managedClusterDiscoveryClient discovery.DiscoveryInterface,
	nodeInformer corev1informers.NodeInformer,
	resyncInterval time.Duration,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterStatusController{
		clusterName:                   clusterName,
		hubClusterClient:              hubClusterClient,
		hubClusterLister:              hubClusterInformer.Lister(),
		managedClusterDiscoveryClient: managedClusterDiscoveryClient,
		nodeLister:                    nodeInformer.Lister(),
	}

	return factory.New().
		WithInformers(hubClusterInformer.Informer(), nodeInformer.Informer()).
		WithSync(c.sync).
		ResyncEvery(resyncInterval).
		ToController("ManagedClusterStatusController", recorder)
}

// sync updates managed cluster available condition by checking kube-apiserver health on managed cluster.
// if the kube-apiserver is health, it will ensure that managed cluster resources and version are up to date.
func (c *managedClusterStatusController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	if _, err := c.hubClusterLister.Get(c.clusterName); err != nil {
		return fmt.Errorf("unable to get managed cluster %q from hub: %w", c.clusterName, err)
	}

	updateStatusFuncs := []helpers.UpdateManagedClusterStatusFunc{}

	// check the kube-apiserver health on managed cluster.
	condition := c.checkKubeAPIServerStatus(ctx)

	// the managed cluster kube-apiserver is health, update its version and resources if necessary.
	if condition.Status == metav1.ConditionTrue {
		clusterVersion, err := c.getClusterVersion()
		if err != nil {
			return fmt.Errorf("unable to get server version of managed cluster %q: %w", c.clusterName, err)
		}

		capacity, allocatable, err := c.getClusterResources()
		if err != nil {
			return fmt.Errorf("unable to get capacity and allocatable of managed cluster %q: %w", c.clusterName, err)
		}

		updateStatusFuncs = append(updateStatusFuncs, updateClusterResourcesFn(clusterv1.ManagedClusterStatus{
			Capacity:    capacity,
			Allocatable: allocatable,
			Version:     *clusterVersion,
		}))
	}

	updateStatusFuncs = append(updateStatusFuncs, helpers.UpdateManagedClusterConditionFn(condition))
	_, updated, err := helpers.UpdateManagedClusterStatus(ctx, c.hubClusterClient, c.clusterName, updateStatusFuncs...)
	if err != nil {
		return fmt.Errorf("unable to update status of managed cluster %q: %w", c.clusterName, err)
	}
	if updated {
		syncCtx.Recorder().Eventf("ManagedClusterStatusUpdated", "the status of managed cluster %q has been updated, available condition is %q, due to %q",
			c.clusterName, condition.Status, condition.Message)
	}
	return nil
}

// using readyz api to check the status of kube apiserver
func (c *managedClusterStatusController) checkKubeAPIServerStatus(ctx context.Context) metav1.Condition {
	statusCode := 0
	condition := metav1.Condition{Type: clusterv1.ManagedClusterConditionAvailable}
	result := c.managedClusterDiscoveryClient.RESTClient().Get().AbsPath("/livez").Do(ctx).StatusCode(&statusCode)
	if statusCode == http.StatusOK {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "ManagedClusterAvailable"
		condition.Message = "Managed cluster is available"
		return condition
	}

	// for backward compatible, the livez endpoint is supported from Kubernetes 1.16, so if the livez is not found or
	// forbidden, the healthz endpoint will be used.
	if statusCode == http.StatusNotFound || statusCode == http.StatusForbidden {
		result = c.managedClusterDiscoveryClient.RESTClient().Get().AbsPath("/healthz").Do(ctx).StatusCode(&statusCode)
		if statusCode == http.StatusOK {
			condition.Status = metav1.ConditionTrue
			condition.Reason = "ManagedClusterAvailable"
			condition.Message = "Managed cluster is available"
			return condition
		}
	}

	condition.Status = metav1.ConditionFalse
	condition.Reason = "ManagedClusterKubeAPIServerUnavailable"
	body, err := result.Raw()
	if err == nil {
		condition.Message = fmt.Sprintf("The kube-apiserver is not ok, status code: %d, %v", statusCode, string(body))
		return condition
	}

	condition.Message = fmt.Sprintf("The kube-apiserver is not ok, status code: %d, %v", statusCode, err)
	return condition
}

func (c *managedClusterStatusController) getClusterVersion() (*clusterv1.ManagedClusterVersion, error) {
	serverVersion, err := c.managedClusterDiscoveryClient.ServerVersion()
	if err != nil {
		return nil, err
	}
	return &clusterv1.ManagedClusterVersion{Kubernetes: serverVersion.String()}, nil
}

func (c *managedClusterStatusController) getClusterResources() (capacity, allocatable clusterv1.ResourceList, err error) {
	nodes, err := c.nodeLister.List(labels.Everything())
	if err != nil {
		return nil, nil, err
	}

	capacityList := make(map[clusterv1.ResourceName]resource.Quantity)
	allocatableList := make(map[clusterv1.ResourceName]resource.Quantity)

	for _, node := range nodes {
		for key, value := range node.Status.Capacity {
			if capacity, exist := capacityList[clusterv1.ResourceName(key)]; exist {
				capacity.Add(value)
				capacityList[clusterv1.ResourceName(key)] = capacity
			} else {
				capacityList[clusterv1.ResourceName(key)] = value
			}
		}

		// the node is unschedulable, ignore its allocatable resources
		if node.Spec.Unschedulable {
			continue
		}

		for key, value := range node.Status.Allocatable {
			if allocatable, exist := allocatableList[clusterv1.ResourceName(key)]; exist {
				allocatable.Add(value)
				allocatableList[clusterv1.ResourceName(key)] = allocatable
			} else {
				allocatableList[clusterv1.ResourceName(key)] = value
			}
		}
	}

	return capacityList, allocatableList, nil
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
