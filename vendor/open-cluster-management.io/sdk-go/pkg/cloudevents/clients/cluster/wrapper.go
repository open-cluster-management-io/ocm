package cluster

import (
	discovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1"
	clusterv1alpha1client "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1alpha1"
	clusterv1beta1client "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1beta1"
	clusterv1beta2client "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1beta2"
)

// ClusterClientSetWrapper wraps a cluster client that has a ManagedCluster client to a cluster
// clientset interface, this wrapper will helps us to build ManagedCluster informer factory easily.
type ClusterClientSetWrapper struct {
	ClusterV1ClientWrapper *ClusterV1ClientWrapper
}

var _ clusterclientset.Interface = &ClusterClientSetWrapper{}

func (c *ClusterClientSetWrapper) ClusterV1() clusterv1client.ClusterV1Interface {
	return c.ClusterV1ClientWrapper
}

func (c *ClusterClientSetWrapper) ClusterV1alpha1() clusterv1alpha1client.ClusterV1alpha1Interface {
	return nil
}

func (c *ClusterClientSetWrapper) ClusterV1beta1() clusterv1beta1client.ClusterV1beta1Interface {
	return nil
}

func (c *ClusterClientSetWrapper) ClusterV1beta2() clusterv1beta2client.ClusterV1beta2Interface {
	return nil
}

func (c *ClusterClientSetWrapper) Discovery() discovery.DiscoveryInterface {
	return nil
}

// ClusterV1ClientWrapper wraps a ManagedCluster client to a ClusterV1Interface
type ClusterV1ClientWrapper struct {
	ManagedClusterClient clusterv1client.ManagedClusterInterface
}

var _ clusterv1client.ClusterV1Interface = &ClusterV1ClientWrapper{}

func (c *ClusterV1ClientWrapper) ManagedClusters() clusterv1client.ManagedClusterInterface {
	if agentManagedClusterClient, ok := c.ManagedClusterClient.(*ManagedClusterClient); ok {
		return agentManagedClusterClient
	}

	return nil
}

func (c *ClusterV1ClientWrapper) RESTClient() rest.Interface {
	return nil
}
