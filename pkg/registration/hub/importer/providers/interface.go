package providers

import (
	"context"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

// Interface is the interface that a cluster provider should implement
type Interface interface {
	// Clients returns the client to connect to the target cluster. The client should have the sufficient
	// permission to create CRDs/operator and klusterlet CR in the remote cluster.
	Clients(ctx context.Context, cluster *clusterv1.ManagedCluster) (*Clients, error)

	// IsManagedClusterOwner check if the provider is used to manage this cluster
	IsManagedClusterOwner(cluster *clusterv1.ManagedCluster) bool

	// Register registers the provider to the importer. The provider should enqueue the resource
	// into the queue with the name of the managed cluster
	Register(syncCtx factory.SyncContext)

	// Run starts the provider. The provider might need to watch the provider related resources
	// on the hub cluster, or start a periodic task.
	Run(ctx context.Context)
}

type Clients struct {
	KubeClient     kubernetes.Interface
	APIExtClient   apiextensionsclient.Interface
	OperatorClient operatorclient.Interface
	DynamicClient  dynamic.Interface
}

func NewClient(config *rest.Config) (*Clients, error) {
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	hubApiExtensionClient, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	operatorClient, err := operatorclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &Clients{
		APIExtClient:   hubApiExtensionClient,
		KubeClient:     kubeClient,
		OperatorClient: operatorClient,
		DynamicClient:  dynamicClient,
	}, nil
}
