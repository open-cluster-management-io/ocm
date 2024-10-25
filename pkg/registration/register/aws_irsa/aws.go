package aws_irsa

import (
	"k8s.io/client-go/tools/cache"

	cluster "open-cluster-management.io/api/client/cluster/clientset/versioned"
	managedclusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1"
	managedclusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster"
	managedclusterv1lister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
)

type AWSIRSAControl interface {
	isApproved(name string) (bool, error)
	generateEKSKubeConfig(name string) ([]byte, error)

	// Informer is public so we can add indexer outside
	Informer() cache.SharedIndexInformer
}

var _ AWSIRSAControl = &v1AWSIRSAControl{}

type v1AWSIRSAControl struct {
	hubManagedClusterInformer cache.SharedIndexInformer
	hubManagedClusterLister   managedclusterv1lister.ManagedClusterLister
	hubManagedClusterClient   managedclusterv1client.ManagedClusterInterface
}

func (v *v1AWSIRSAControl) isApproved(name string) (bool, error) {
	// TODO: check if the managedclusuter cr on hub has required condition and is approved
	approved := false

	return approved, nil
}

func (v *v1AWSIRSAControl) generateEKSKubeConfig(name string) ([]byte, error) {
	// TODO: generate and return kubeconfig
	return nil, nil
}

func (v *v1AWSIRSAControl) Informer() cache.SharedIndexInformer {
	return v.hubManagedClusterInformer
}

//TODO: Uncomment the below once required in the aws irsa authentication implementation
/*
func (v *v1AWSIRSAControl) get(name string) (metav1.Object, error) {
	managedcluster, err := v.hubManagedClusterLister.Get(name)
	switch {
	case apierrors.IsNotFound(err):
		// fallback to fetching managedcluster from hub apiserver in case it is not cached by informer yet
		managedcluster, err = v.hubManagedClusterClient.Get(context.Background(), name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("unable to get managedcluster %q. It might have already been deleted", name)
		}
	case err != nil:
		return nil, err
	}
	return managedcluster, nil
}
*/

func NewAWSIRSAControl(hubManagedClusterInformer managedclusterinformers.Interface, hubManagedClusterClient cluster.Interface) (AWSIRSAControl, error) {
	return &v1AWSIRSAControl{
		hubManagedClusterInformer: hubManagedClusterInformer.V1().ManagedClusters().Informer(),
		hubManagedClusterLister:   hubManagedClusterInformer.V1().ManagedClusters().Lister(),
		hubManagedClusterClient:   hubManagedClusterClient.ClusterV1().ManagedClusters(),
	}, nil
}
