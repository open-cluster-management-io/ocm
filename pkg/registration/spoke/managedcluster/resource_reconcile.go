package managedcluster

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/discovery"
	corev1lister "k8s.io/client-go/listers/core/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

type resoureReconcile struct {
	managedClusterDiscoveryClient discovery.DiscoveryInterface
	nodeLister                    corev1lister.NodeLister
}

func (r *resoureReconcile) reconcile(ctx context.Context, _ factory.SyncContext, cluster *clusterv1.ManagedCluster) (*clusterv1.ManagedCluster, reconcileState, error) {
	clusterVersion, err := r.getClusterVersion()
	if err != nil {
		return cluster, reconcileStop, fmt.Errorf("unable to get server version of managed cluster %q: %w", cluster.Name, err)
	}

	capacity, allocatable, err := r.getClusterResources()
	if err != nil {
		return cluster, reconcileStop, fmt.Errorf("unable to get capacity and allocatable of managed cluster %q: %w", cluster.Name, err)
	}

	// we allow other components update the cluster capacity, so we need merge the capacity to this updated, if
	// one current capacity entry does not exist in this updated capacity, we add it back.
	for key, val := range cluster.Status.Capacity {
		if _, ok := capacity[key]; !ok {
			capacity[key] = val
		}
	}

	cluster.Status.Capacity = capacity
	cluster.Status.Allocatable = allocatable
	cluster.Status.Version = *clusterVersion

	return cluster, reconcileContinue, nil
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
