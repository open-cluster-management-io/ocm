package work

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	agentclient "open-cluster-management.io/sdk-go/pkg/cloudevents/work/agent/client"
	agenthandler "open-cluster-management.io/sdk-go/pkg/cloudevents/work/agent/handler"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/internal"
	sourceclient "open-cluster-management.io/sdk-go/pkg/cloudevents/work/source/client"
	sourcehandler "open-cluster-management.io/sdk-go/pkg/cloudevents/work/source/handler"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/watcher"
)

const defaultInformerResyncTime = 10 * time.Minute

// ClientHolder holds a manifestwork client that implements the ManifestWorkInterface based on different configuration
// and a ManifestWorkInformer that is built with the manifestWork client.
//
// ClientHolder also implements the ManifestWorksGetter interface.
type ClientHolder struct {
	workClientSet        workclientset.Interface
	manifestWorkInformer workv1informers.ManifestWorkInformer
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

// ManifestWorkInformer returns a ManifestWorkInformer
func (h *ClientHolder) ManifestWorkInformer() workv1informers.ManifestWorkInformer {
	return h.manifestWorkInformer
}

// ClientHolderBuilder builds the ClientHolder with different configuration.
type ClientHolderBuilder struct {
	config             any
	codecs             []generic.Codec[*workv1.ManifestWork]
	informerOptions    []workinformers.SharedInformerOption
	informerResyncTime time.Duration
	sourceID           string
	clusterName        string
	clientID           string
}

// NewClientHolderBuilder returns a ClientHolderBuilder with a given configuration.
//
// Available configurations:
//   - Kubeconfig (*rest.Config): builds a manifestwork client with kubeconfig
//   - MQTTOptions (*mqtt.MQTTOptions): builds a manifestwork client based on cloudevents with MQTT
//   - GRPCOptions (*grpc.GRPCOptions): builds a manifestwork client based on cloudevents with GRPC
//   - KafkaOptions (*kafka.KafkaOptions): builds a manifestwork client based on cloudevents with Kafka
//
// TODO using a specified config instead of any
func NewClientHolderBuilder(config any) *ClientHolderBuilder {
	return &ClientHolderBuilder{
		config:             config,
		informerResyncTime: defaultInformerResyncTime,
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

// NewSourceClientHolder returns a ClientHolder for source
func (b *ClientHolderBuilder) NewSourceClientHolder(ctx context.Context) (*ClientHolder, error) {
	switch config := b.config.(type) {
	case *rest.Config:
		return b.newKubeClients(config)
	default:
		options, err := generic.BuildCloudEventsSourceOptions(config, b.clientID, b.sourceID)
		if err != nil {
			return nil, err
		}
		return b.newSourceClients(ctx, options)
	}
}

// NewAgentClientHolder returns a ClientHolder for agent
func (b *ClientHolderBuilder) NewAgentClientHolder(ctx context.Context) (*ClientHolder, error) {
	switch config := b.config.(type) {
	case *rest.Config:
		return b.newKubeClients(config)
	default:
		options, err := generic.BuildCloudEventsAgentOptions(config, b.clusterName, b.clientID)
		if err != nil {
			return nil, err
		}
		return b.newAgentClients(ctx, options)
	}
}

func (b *ClientHolderBuilder) newAgentClients(ctx context.Context, agentOptions *options.CloudEventsAgentOptions) (*ClientHolder, error) {
	if len(b.clientID) == 0 {
		return nil, fmt.Errorf("client id is required")
	}

	if len(b.clusterName) == 0 {
		return nil, fmt.Errorf("cluster name is required")
	}

	workLister := &ManifestWorkLister{}
	watcher := watcher.NewManifestWorkWatcher()
	cloudEventsClient, err := generic.NewCloudEventAgentClient[*workv1.ManifestWork](
		ctx,
		agentOptions,
		workLister,
		ManifestWorkStatusHash,
		b.codecs...,
	)
	if err != nil {
		return nil, err
	}

	manifestWorkClient := agentclient.NewManifestWorkAgentClient(cloudEventsClient, watcher)
	workClient := &internal.WorkV1ClientWrapper{ManifestWorkClient: manifestWorkClient}
	workClientSet := &internal.WorkClientSetWrapper{WorkV1ClientWrapper: workClient}
	factory := workinformers.NewSharedInformerFactoryWithOptions(workClientSet, b.informerResyncTime, b.informerOptions...)
	informers := factory.Work().V1().ManifestWorks()
	manifestWorkLister := informers.Lister()
	namespacedLister := manifestWorkLister.ManifestWorks(b.clusterName)

	// Set informer lister back to work lister and client.
	workLister.Lister = manifestWorkLister
	// TODO the work client and informer share a same store in the current implementation, ideally, the store should be
	// only written from the server. we may need to revisit the implementation in the future.
	manifestWorkClient.SetLister(namespacedLister)

	cloudEventsClient.Subscribe(ctx, agenthandler.NewManifestWorkAgentHandler(namespacedLister, watcher))

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-cloudEventsClient.ReconnectedChan():
				// when receiving a client reconnected signal, we resync all sources for this agent
				// TODO after supporting multiple sources, we should only resync agent known sources
				if err := cloudEventsClient.Resync(ctx, types.SourceAll); err != nil {
					klog.Errorf("failed to send resync request, %v", err)
				}
			}
		}
	}()

	return &ClientHolder{
		workClientSet:        workClientSet,
		manifestWorkInformer: informers,
	}, nil
}

func (b *ClientHolderBuilder) newSourceClients(ctx context.Context, sourceOptions *options.CloudEventsSourceOptions) (*ClientHolder, error) {
	if len(b.clientID) == 0 {
		return nil, fmt.Errorf("client id is required")
	}

	if len(b.sourceID) == 0 {
		return nil, fmt.Errorf("source id is required")
	}

	workLister := &ManifestWorkLister{}
	watcher := watcher.NewManifestWorkWatcher()
	cloudEventsClient, err := generic.NewCloudEventSourceClient[*workv1.ManifestWork](
		ctx,
		sourceOptions,
		workLister,
		ManifestWorkStatusHash,
		b.codecs...,
	)
	if err != nil {
		return nil, err
	}

	manifestWorkClient := sourceclient.NewManifestWorkSourceClient(b.sourceID, cloudEventsClient, watcher)
	workClient := &internal.WorkV1ClientWrapper{ManifestWorkClient: manifestWorkClient}
	workClientSet := &internal.WorkClientSetWrapper{WorkV1ClientWrapper: workClient}
	factory := workinformers.NewSharedInformerFactoryWithOptions(workClientSet, b.informerResyncTime, b.informerOptions...)
	informers := factory.Work().V1().ManifestWorks()
	manifestWorkLister := informers.Lister()
	// Set informer lister back to work lister and client.
	workLister.Lister = manifestWorkLister
	manifestWorkClient.SetLister(manifestWorkLister)

	sourceHandler := sourcehandler.NewManifestWorkSourceHandler(manifestWorkLister, watcher)
	cloudEventsClient.Subscribe(ctx, sourceHandler.HandlerFunc())

	go sourceHandler.Run(ctx.Done())
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

	return &ClientHolder{
		workClientSet:        workClientSet,
		manifestWorkInformer: informers,
	}, nil
}

func (b *ClientHolderBuilder) newKubeClients(config *rest.Config) (*ClientHolder, error) {
	kubeWorkClientSet, err := workclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	factory := workinformers.NewSharedInformerFactoryWithOptions(kubeWorkClientSet, b.informerResyncTime, b.informerOptions...)
	return &ClientHolder{
		workClientSet:        kubeWorkClientSet,
		manifestWorkInformer: factory.Work().V1().ManifestWorks(),
	}, nil
}
