package work

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/internal"
	sourceclient "open-cluster-management.io/sdk-go/pkg/cloudevents/work/source/client"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/source/lister"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/store"
)

const defaultInformerResyncTime = 10 * time.Minute

// ClientHolder holds a manifestwork client that implements the ManifestWorkInterface based on different configuration
//
// ClientHolder also implements the ManifestWorksGetter interface.
type ClientHolder struct {
	workClientSet workclientset.Interface
}

var _ workv1client.ManifestWorksGetter = &ClientHolder{}

// WorkInterface returns a workclientset Interface
func (h *ClientHolder) WorkInterface() workclientset.Interface {
	return h.workClientSet
}

// ManifestWorks returns a ManifestWorkInterface
func (h *ClientHolder) ManifestWorks(namespace string) workv1client.ManifestWorkInterface {
	return h.workClientSet.WorkV1().ManifestWorks(namespace)
}

// ClientHolderBuilder builds the ClientHolder with different configuration.
type ClientHolderBuilder struct {
	config             any
	watcherStore       store.WorkClientWatcherStore
	codecs             []generic.Codec[*workv1.ManifestWork]
	informerOptions    []workinformers.SharedInformerOption
	informerResyncTime time.Duration
	sourceID           string
	clusterName        string
	clientID           string
	resync             bool
}

// NewClientHolderBuilder returns a ClientHolderBuilder with a given configuration.
//
// Available configurations:
//   - MQTTOptions (*mqtt.MQTTOptions): builds a manifestwork client based on cloudevents with MQTT
//   - GRPCOptions (*grpc.GRPCOptions): builds a manifestwork client based on cloudevents with GRPC
//   - KafkaOptions (*kafka.KafkaOptions): builds a manifestwork client based on cloudevents with Kafka
//
// TODO using a specified config instead of any
func NewClientHolderBuilder(config any) *ClientHolderBuilder {
	return &ClientHolderBuilder{
		config:             config,
		informerResyncTime: defaultInformerResyncTime,
		resync:             true,
	}
}

// WithClientID set the client ID for source/agent cloudevents client.
func (b *ClientHolderBuilder) WithClientID(clientID string) *ClientHolderBuilder {
	b.clientID = clientID
	return b
}

// WithSourceID set the source ID when building a manifestwork client for a source.
func (b *ClientHolderBuilder) WithSourceID(sourceID string) *ClientHolderBuilder {
	b.sourceID = sourceID
	return b
}

// WithClusterName set the managed cluster name when building a manifestwork client for an agent.
func (b *ClientHolderBuilder) WithClusterName(clusterName string) *ClientHolderBuilder {
	b.clusterName = clusterName
	return b
}

// WithCodecs add codecs when building a manifestwork client based on cloudevents.
func (b *ClientHolderBuilder) WithCodecs(codecs ...generic.Codec[*workv1.ManifestWork]) *ClientHolderBuilder {
	b.codecs = codecs
	return b
}

// WithInformerConfig set the ManifestWorkInformer configs. If the resync time is not set, the default time (10 minutes)
// will be used when building the ManifestWorkInformer.
func (b *ClientHolderBuilder) WithInformerConfig(
	resyncTime time.Duration, options ...workinformers.SharedInformerOption,
) *ClientHolderBuilder {
	b.informerResyncTime = resyncTime
	b.informerOptions = options
	return b
}

// WithWorkClientWatcherStore set the WorkClientWatcherStore. The client will use this store to caches the works and
// watch the work events.
func (b *ClientHolderBuilder) WithWorkClientWatcherStore(store store.WorkClientWatcherStore) *ClientHolderBuilder {
	b.watcherStore = store
	return b
}

// WithResyncEnabled control the client resync (Default is true), if it's true, the resync happens when
//  1. after the client's store is initiated
//  2. the client reconnected
func (b *ClientHolderBuilder) WithResyncEnabled(resync bool) *ClientHolderBuilder {
	b.resync = resync
	return b
}

// NewSourceClientHolder returns a ClientHolder for a source
func (b *ClientHolderBuilder) NewSourceClientHolder(ctx context.Context) (*ClientHolder, error) {
	if len(b.clientID) == 0 {
		return nil, fmt.Errorf("client id is required")
	}

	if len(b.sourceID) == 0 {
		return nil, fmt.Errorf("source id is required")
	}

	if b.watcherStore == nil {
		return nil, fmt.Errorf("a watcher store is required")
	}

	options, err := generic.BuildCloudEventsSourceOptions(b.config, b.clientID, b.sourceID)
	if err != nil {
		return nil, err
	}

	cloudEventsClient, err := generic.NewCloudEventSourceClient[*workv1.ManifestWork](
		ctx,
		options,
		lister.NewWatcherStoreLister(b.watcherStore),
		ManifestWorkStatusHash,
		b.codecs...,
	)
	if err != nil {
		return nil, err
	}

	// start to subscribe
	cloudEventsClient.Subscribe(ctx, b.watcherStore.HandleReceivedWork)

	manifestWorkClient := sourceclient.NewManifestWorkSourceClient(b.sourceID, cloudEventsClient, b.watcherStore)
	workClient := &internal.WorkV1ClientWrapper{ManifestWorkClient: manifestWorkClient}
	workClientSet := &internal.WorkClientSetWrapper{WorkV1ClientWrapper: workClient}

	if !b.resync {
		return &ClientHolder{workClientSet: workClientSet}, nil
	}

	// start a go routine to resync the works when this client reconnected
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-cloudEventsClient.ReconnectedChan():
				// when receiving a client reconnected signal, we resync all clusters for this source
				if err := cloudEventsClient.Resync(ctx, types.ClusterAll); err != nil {
					klog.Errorf("failed to send resync request, %v", err)
				}
			}
		}
	}()

	// start a go routine to resync the works after this client's store is initiated
	go func() {
		if store.WaitForStoreInit(ctx, b.watcherStore.HasInitiated) {
			if err := cloudEventsClient.Resync(ctx, types.ClusterAll); err != nil {
				klog.Errorf("failed to resync")
			}
		}
	}()

	return &ClientHolder{workClientSet: workClientSet}, nil
}
