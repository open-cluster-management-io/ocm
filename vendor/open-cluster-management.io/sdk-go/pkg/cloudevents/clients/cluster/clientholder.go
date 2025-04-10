package cluster

import (
	"context"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
)

// ClientHolder holds a ManagedCluster client that implements the ManagedClusterInterface based on different configuration
//
// ClientHolder also implements the ManagedClustersGetter interface.
type ClientHolder struct {
	clusterClientSet clusterclientset.Interface
}

var _ clusterv1client.ManagedClustersGetter = &ClientHolder{}

// ClusterInterface returns a clusterclientset Interface
func (h *ClientHolder) ClusterInterface() clusterclientset.Interface {
	return h.clusterClientSet
}

// ManagedClusters returns a ManagedClusterInterface
func (h *ClientHolder) ManagedClusters() clusterv1client.ManagedClusterInterface {
	return h.clusterClientSet.ClusterV1().ManagedClusters()
}

// NewClientHolder returns a ClientHolder for ManagedCluster
func NewClientHolder(ctx context.Context, opt *options.GenericClientOptions[*clusterv1.ManagedCluster]) (*ClientHolder, error) {

	// start to subscribe
	cloudEventsClient, err := opt.AgentClient(ctx)
	if err != nil {
		return nil, err
	}

	managedClusterClient := NewManagedClusterClient(cloudEventsClient, opt.WatcherStore(), opt.ClusterName())
	clusterClient := &ClusterV1ClientWrapper{ManagedClusterClient: managedClusterClient}
	clusterClientSet := &ClusterClientSetWrapper{ClusterV1ClientWrapper: clusterClient}

	return &ClientHolder{clusterClientSet: clusterClientSet}, nil
}
