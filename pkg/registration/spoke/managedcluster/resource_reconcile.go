package managedcluster

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/discovery"
	corev1lister "k8s.io/client-go/listers/core/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type resoureReconcile struct {
	managedClusterDiscoveryClient discovery.DiscoveryInterface
	nodeLister                    corev1lister.NodeLister
}

func (r *resoureReconcile) reconcile(ctx context.Context, cluster *clusterv1.ManagedCluster) (*clusterv1.ManagedCluster, reconcileState, error) {
	// check the kube-apiserver health on managed cluster.
	condition := r.checkKubeAPIServerStatus(ctx)

	// the managed cluster kube-apiserver is health, update its version and resources if necessary.
	if condition.Status == metav1.ConditionTrue {
		clusterVersion, err := r.getClusterVersion()
		if err != nil {
			return cluster, reconcileStop, fmt.Errorf("unable to get server version of managed cluster %q: %w", cluster.Name, err)
		}

		capacity, allocatable, err := r.getClusterResources()
		if err != nil {
			return cluster, reconcileStop, fmt.Errorf("unable to get capacity and allocatable of managed cluster %q: %w", cluster.Name, err)
		}

		cluster.Status.Capacity = capacity
		cluster.Status.Allocatable = allocatable
		cluster.Status.Version = *clusterVersion
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, condition)
	return cluster, reconcileContinue, nil
}

// using readyz api to check the status of kube apiserver
func (r *resoureReconcile) checkKubeAPIServerStatus(ctx context.Context) metav1.Condition {
	statusCode := 0
	condition := metav1.Condition{Type: clusterv1.ManagedClusterConditionAvailable}
	result := r.managedClusterDiscoveryClient.RESTClient().Get().AbsPath("/livez").Do(ctx).StatusCode(&statusCode)
	if statusCode == http.StatusOK {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "ManagedClusterAvailable"
		condition.Message = "Managed cluster is available"
		return condition
	}

	// for backward compatible, the livez endpoint is supported from Kubernetes 1.16, so if the livez is not found or
	// forbidden, the healthz endpoint will be used.
	if statusCode == http.StatusNotFound || statusCode == http.StatusForbidden {
		result = r.managedClusterDiscoveryClient.RESTClient().Get().AbsPath("/healthz").Do(ctx).StatusCode(&statusCode)
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

func (r *resoureReconcile) getClusterVersion() (*clusterv1.ManagedClusterVersion, error) {
	serverVersion, err := r.managedClusterDiscoveryClient.ServerVersion()
	if err != nil {
		return nil, err
	}
	return &clusterv1.ManagedClusterVersion{Kubernetes: serverVersion.String()}, nil
}

func (r *resoureReconcile) getClusterResources() (capacity, allocatable clusterv1.ResourceList, err error) {
	nodes, err := r.nodeLister.List(labels.Everything())
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
