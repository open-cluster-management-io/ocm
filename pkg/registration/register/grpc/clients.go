package grpc

import (
	"context"
	"time"

	coordv1 "k8s.io/api/coordination/v1"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	cloudeventsaddon "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	cloudeventsaddonv1beta1 "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon/v1beta1"
	cloudeventscluster "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	cloudeventsevent "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	cloudeventslease "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	cloudeventsoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	cloudeventsstore "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

// NewCloudEventsClients builds the registration agent's hub clients over CloudEvents from
// config (*grpc.GRPCOptions). With clusterOnly, only the ManagedCluster client and informer are
// built, as the bootstrap phase needs; the other clients need permissions an agent gets only
// once its cluster is accepted. The grpc and spiffe drivers share it.
func NewCloudEventsClients(ctx context.Context, config any, clusterName string, clusterOnly bool) (*register.Clients, error) {
	clusterWatchStore := cloudeventsstore.NewAgentInformerWatcherStore[*clusterv1.ManagedCluster]()
	clusterClientHolder, err := cloudeventscluster.NewClientHolder(
		ctx,
		cloudeventsoptions.NewGenericClientOptions(
			config,
			cloudeventscluster.NewManagedClusterCodec(),
			clusterName,
		).
			WithClusterName(clusterName).
			WithClientWatcherStore(clusterWatchStore))
	if err != nil {
		return nil, err
	}
	clusterClient := clusterClientHolder.ClusterInterface()
	clusterInformers := clusterinformers.NewSharedInformerFactory(
		clusterClient, 10*time.Minute).Cluster().V1().ManagedClusters()

	if clusterOnly {
		return &register.Clients{
			ClusterClient:   clusterClient,
			ClusterInformer: clusterInformers,
		}, nil
	}

	leaseWatchStore := cloudeventsstore.NewSimpleStore[*coordv1.Lease]()
	leaseClient, err := cloudeventslease.NewLeaseClient(
		ctx,
		cloudeventsoptions.NewGenericClientOptions(
			config,
			cloudeventslease.NewLeaseCodec(),
			clusterName,
		).WithClusterName(clusterName).WithClientWatcherStore(leaseWatchStore),
		clusterName,
	)
	if err != nil {
		return nil, err
	}

	eventClient, err := cloudeventsevent.NewClientHolder(
		ctx,
		cloudeventsoptions.NewGenericClientOptions(
			config,
			cloudeventsevent.NewEventCodec(),
			clusterName,
		).WithClusterName(clusterName).WithSubscription(false).WithResyncEnabled(false),
	)
	if err != nil {
		return nil, err
	}

	addonWatchStore := cloudeventsstore.NewAgentInformerWatcherStore[*addonv1beta1.ManagedClusterAddOn]()
	addonClient, err := cloudeventsaddon.ManagedClusterAddOnInterface(
		ctx,
		cloudeventsoptions.NewGenericClientOptions(
			config,
			cloudeventsaddonv1beta1.NewManagedClusterAddOnCodec(),
			clusterName,
		).WithClusterName(clusterName).WithClientWatcherStore(addonWatchStore))
	if err != nil {
		return nil, err
	}
	addonInformer := addoninformers.NewSharedInformerFactoryWithOptions(
		addonClient, 10*time.Minute, addoninformers.WithNamespace(clusterName)).
		Addon().V1beta1().ManagedClusterAddOns()

	return &register.Clients{
		ClusterClient:   clusterClient,
		ClusterInformer: clusterInformers,
		AddonClient:     addonClient,
		AddonInformer:   addonInformer,
		LeaseClient:     leaseClient,
		EventsClient:    eventClient,
	}, nil
}
