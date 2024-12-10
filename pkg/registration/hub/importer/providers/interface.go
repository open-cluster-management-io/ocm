package providers

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/factory"
	"k8s.io/client-go/rest"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// Interface is the interface that a cluster provider should implement
type Interface interface {
	// KubeConfig is to return the config to connect to the target cluster.
	KubeConfig(cluster *clusterv1.ManagedCluster) (*rest.Config, error)

	// IsManagedClusterOwner check if the provider is used to manage this cluster
	IsManagedClusterOwner(cluster *clusterv1.ManagedCluster) bool

	// Register registers the provider to the importer. The provider should enqueue the resource
	// into the queue with the name of the managed cluster
	Register(syncCtx factory.SyncContext)

	// Run starts the provider
	Run(ctx context.Context)
}
