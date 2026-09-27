package azure_auth

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	cluster "open-cluster-management.io/api/client/cluster/clientset/versioned"
	managedclusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1"
	clusterv1informer "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	managedclusterv1lister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
)

// AzureAuthControl exposes the subset of hub ManagedCluster state the spoke driver
// needs to decide whether registration has completed.
type AzureAuthControl interface {
	isApproved(name string) (bool, error)

	// Informer is public so an indexer can be added outside.
	Informer() cache.SharedIndexInformer
}

var _ AzureAuthControl = &v1AzureAuthControl{}

type v1AzureAuthControl struct {
	hubManagedClusterInformer cache.SharedIndexInformer
	hubManagedClusterLister   managedclusterv1lister.ManagedClusterLister
	hubManagedClusterClient   managedclusterv1client.ManagedClusterInterface
}

// isApproved reports whether the hub has accepted this managed cluster and granted its
// Azure identity the RBAC permissions it needs. HubAccepted must be True: the hub sets
// it False both when an admin denies the cluster and when
// AzureAuthHubDriver.CreatePermissions fails, e.g. because the identity is already
// bound to a different managed cluster.
func (v *v1AzureAuthControl) isApproved(name string) (bool, error) {
	managedCluster, err := v.get(name)
	if err != nil {
		return false, err
	}
	return meta.IsStatusConditionTrue(managedCluster.Status.Conditions, v1.ManagedClusterConditionHubAccepted), nil
}

func (v *v1AzureAuthControl) Informer() cache.SharedIndexInformer {
	return v.hubManagedClusterInformer
}

func (v *v1AzureAuthControl) get(name string) (*v1.ManagedCluster, error) {
	managedCluster, err := v.hubManagedClusterLister.Get(name)
	switch {
	case apierrors.IsNotFound(err):
		// fallback to fetching managedcluster from hub apiserver in case it is not cached by informer yet
		managedCluster, err = v.hubManagedClusterClient.Get(context.Background(), name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("unable to get managedcluster %q. It might have already been deleted", name)
		}
		if err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	}
	return managedCluster, nil
}

func NewAzureAuthControl(
	hubManagedClusterInformer clusterv1informer.ManagedClusterInformer,
	hubManagedClusterClient cluster.Interface) (AzureAuthControl, error) {
	return &v1AzureAuthControl{
		hubManagedClusterInformer: hubManagedClusterInformer.Informer(),
		hubManagedClusterLister:   hubManagedClusterInformer.Lister(),
		hubManagedClusterClient:   hubManagedClusterClient.ClusterV1().ManagedClusters(),
	}, nil
}
