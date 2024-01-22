package work

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/rest"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/mqtt"
	agentclient "open-cluster-management.io/sdk-go/pkg/cloudevents/work/agent/client"
	agenthandler "open-cluster-management.io/sdk-go/pkg/cloudevents/work/agent/handler"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/internal"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/watcher"
)

const defaultInformerResyncTime = 10 * time.Minute

// ClientHolder holds a manifestwork client that implements the ManifestWorkInterface based on different configuration
// and a ManifestWorkInformer that is built with the manifestWork client.
//
// ClientHolder also implements the ManifestWorksGetter interface.
type ClientHolder struct {
	workClient           workv1client.WorkV1Interface
	manifestWorkInformer workv1informers.ManifestWorkInformer
}

var _ workv1client.ManifestWorksGetter = &ClientHolder{}

// ManifestWorks returns a ManifestWorkInterface
func (h *ClientHolder) ManifestWorks(namespace string) workv1client.ManifestWorkInterface {
	return h.workClient.ManifestWorks(namespace)
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
	clusterName        string
	clientID           string
}

// NewClientHolderBuilder returns a ClientHolderBuilder with a given configuration.
//
// Available configurations:
//   - Kubeconfig (*rest.Config): builds a manifestwork client with kubeconfig
//   - MQTTOptions (*mqtt.MQTTOptions): builds a manifestwork client based on cloudevents with MQTT
func NewClientHolderBuilder(clientID string, config any) *ClientHolderBuilder {
	return &ClientHolderBuilder{
		clientID:           clientID,
		config:             config,
		informerResyncTime: defaultInformerResyncTime,
	}
}

// WithClusterName set the managed cluster name when building a  manifestwork client for an agent.
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
	resyncTime time.Duration, options ...workinformers.SharedInformerOption) *ClientHolderBuilder {
	b.informerResyncTime = resyncTime
	b.informerOptions = options
	return b
}

// NewClientHolder returns a ClientHolder for works.
func (b *ClientHolderBuilder) NewClientHolder(ctx context.Context) (*ClientHolder, error) {
	switch config := b.config.(type) {
	case *rest.Config:
		kubeWorkClientSet, err := workclientset.NewForConfig(config)
		if err != nil {
			return nil, err
		}

		factory := workinformers.NewSharedInformerFactoryWithOptions(kubeWorkClientSet, b.informerResyncTime, b.informerOptions...)

		return &ClientHolder{
			workClient:           kubeWorkClientSet.WorkV1(),
			manifestWorkInformer: factory.Work().V1().ManifestWorks(),
		}, nil
	case *mqtt.MQTTOptions:
		if len(b.clusterName) != 0 {
			return b.newAgentClients(ctx, config)
		}

		//TODO build manifestwork clients for source
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported client configuration type %T", config)
	}
}

func (b *ClientHolderBuilder) newAgentClients(ctx context.Context, config *mqtt.MQTTOptions) (*ClientHolder, error) {
	workLister := &ManifestWorkLister{}
	watcher := watcher.NewManifestWorkWatcher()
	agentOptions := mqtt.NewAgentOptions(config, b.clusterName, b.clientID)
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

	return &ClientHolder{
		workClient:           workClient,
		manifestWorkInformer: informers,
	}, nil
}
