package options

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/statushash"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type GenericClientOptions[T generic.ResourceObject] struct {
	config       any
	codec        generic.Codec[T]
	watcherStore store.ClientWatcherStore[T]
	clientID     string
	sourceID     string
	clusterName  string
	subscription bool
	resync       bool
}

// NewGenericClientOptions create a GenericClientOptions
//
//   - config, available configurations:
//
//     MQTTOptions (*mqtt.MQTTOptions): builds a generic cloudevents client with MQTT
//
//     GRPCOptions (*grpc.GRPCOptions): builds a generic cloudevents client with GRPC
//
//     KafkaOptions (*kafka.KafkaOptions): builds a generic cloudevents client with Kafka
//
//   - codec, the codec for resource
//
//   - clientID, the client ID for generic cloudevents client.
//
// TODO using a specified config instead of any
func NewGenericClientOptions[T generic.ResourceObject](config any,
	codec generic.Codec[T],
	clientID string) *GenericClientOptions[T] {
	return &GenericClientOptions[T]{
		config:       config,
		codec:        codec,
		clientID:     clientID,
		subscription: true,
		resync:       true,
	}
}

// WithClientWatcherStore set the ClientWatcherStore. The client uses this store to caches the resources and
// watch the resource events. For agent, the AgentInformerWatcherStore is used by default
//
// TODO provide a default ClientWatcherStore for source.
func (o *GenericClientOptions[T]) WithClientWatcherStore(store store.ClientWatcherStore[T]) *GenericClientOptions[T] {
	o.watcherStore = store
	return o
}

// WithSourceID set the source ID when building a client for a source.
func (o *GenericClientOptions[T]) WithSourceID(sourceID string) *GenericClientOptions[T] {
	o.sourceID = sourceID
	return o
}

// WithClusterName set the managed cluster name when building a client for an agent.
func (o *GenericClientOptions[T]) WithClusterName(clusterName string) *GenericClientOptions[T] {
	o.clusterName = clusterName
	return o
}

// WithSubscription control the client subscription (Default is true), if it's false, the client
// will not subscribe to source/consumer.
func (o *GenericClientOptions[T]) WithSubscription(enabled bool) *GenericClientOptions[T] {
	o.subscription = enabled
	return o
}

// WithResyncEnabled control the client resync (Default is true), if it's true, the resync happens when
//  1. after the client's store is initiated
//  2. the client reconnected
func (o *GenericClientOptions[T]) WithResyncEnabled(resync bool) *GenericClientOptions[T] {
	o.resync = resync
	return o
}

func (o *GenericClientOptions[T]) ClusterName() string {
	return o.clusterName
}

func (o *GenericClientOptions[T]) SourceID() string {
	return o.sourceID
}

func (o *GenericClientOptions[T]) WatcherStore() store.ClientWatcherStore[T] {
	return o.watcherStore
}

func (o *GenericClientOptions[T]) AgentClient(ctx context.Context) (*generic.CloudEventAgentClient[T], error) {
	if len(o.clientID) == 0 {
		return nil, fmt.Errorf("client id is required")
	}

	if len(o.clusterName) == 0 {
		return nil, fmt.Errorf("cluster name is required")
	}

	if o.watcherStore == nil {
		o.watcherStore = store.NewAgentInformerWatcherStore[T]()
	}

	options, err := generic.BuildCloudEventsAgentOptions(o.config, o.clusterName, o.clientID)
	if err != nil {
		return nil, err
	}

	cloudEventsClient, err := generic.NewCloudEventAgentClient(
		ctx,
		options,
		store.NewAgentWatcherStoreLister(o.watcherStore),
		statushash.StatusHash,
		o.codec,
	)
	if err != nil {
		return nil, err
	}

	if o.subscription {
		// start to subscribe
		cloudEventsClient.Subscribe(ctx, o.watcherStore.HandleReceivedResource)
	}

	// start a go routine to receive client reconnect signal
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-cloudEventsClient.ReconnectedChan():
				if !o.resync {
					klog.V(4).Infof("resync is disabled, do nothing")
					continue
				}

				// when receiving a client reconnected signal, we resync all sources for this agent
				// TODO after supporting multiple sources, we should only resync agent known sources
				if err := cloudEventsClient.Resync(ctx, types.SourceAll); err != nil {
					klog.Errorf("failed to send resync request, %v", err)
				}
			}
		}
	}()

	if !o.resync {
		return cloudEventsClient, nil
	}

	// start a go routine to resync the works after this client's store is initiated
	go func() {
		if store.WaitForStoreInit(ctx, o.watcherStore.HasInitiated) {
			if err := cloudEventsClient.Resync(ctx, types.SourceAll); err != nil {
				klog.Errorf("failed to send resync request, %v", err)
			}
		}
	}()

	return cloudEventsClient, nil
}

func (o *GenericClientOptions[T]) SourceClient(ctx context.Context) (*generic.CloudEventSourceClient[T], error) {
	if len(o.clientID) == 0 {
		return nil, fmt.Errorf("client id is required")
	}

	if len(o.sourceID) == 0 {
		return nil, fmt.Errorf("source id is required")
	}

	if o.watcherStore == nil {
		return nil, fmt.Errorf("a watcher store is required")
	}

	options, err := generic.BuildCloudEventsSourceOptions(o.config, o.clientID, o.sourceID)
	if err != nil {
		return nil, err
	}

	cloudEventsClient, err := generic.NewCloudEventSourceClient(
		ctx,
		options,
		store.NewSourceWatcherStoreLister(o.watcherStore),
		statushash.StatusHash,
		o.codec,
	)
	if err != nil {
		return nil, err
	}

	if o.subscription {
		// start to subscribe
		cloudEventsClient.Subscribe(ctx, o.watcherStore.HandleReceivedResource)
	}

	// start a go routine to receive client reconnect signal
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-cloudEventsClient.ReconnectedChan():
				if !o.resync {
					klog.V(4).Infof("resync is disabled, do nothing")
					continue
				}

				// when receiving a client reconnected signal, we resync all clusters for this source
				if err := cloudEventsClient.Resync(ctx, types.ClusterAll); err != nil {
					klog.Errorf("failed to send resync request, %v", err)
				}
			}
		}
	}()

	if !o.resync {
		return cloudEventsClient, nil
	}

	// start a go routine to resync the works after this client's store is initiated
	go func() {
		if store.WaitForStoreInit(ctx, o.watcherStore.HasInitiated) {
			if err := cloudEventsClient.Resync(ctx, types.ClusterAll); err != nil {
				klog.Errorf("failed to send resync request, %v", err)
			}
		}
	}()

	return cloudEventsClient, nil
}
