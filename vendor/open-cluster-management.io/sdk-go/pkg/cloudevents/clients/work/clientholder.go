package work

import (
	"context"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	agentclient "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/agent/client"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/internal"
	sourceclient "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/source/client"
)

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

// NewSourceClientHolder returns a ClientHolder for a source
func NewSourceClientHolder(ctx context.Context, opt *options.GenericClientOptions[*workv1.ManifestWork]) (*ClientHolder, error) {
	sourceClient, err := opt.SourceClient(ctx)
	if err != nil {
		return nil, err
	}

	manifestWorkClient := sourceclient.NewManifestWorkSourceClient(opt.SourceID(), opt.WatcherStore(), sourceClient)
	workClient := &internal.WorkV1ClientWrapper{ManifestWorkClient: manifestWorkClient}
	workClientSet := &internal.WorkClientSetWrapper{WorkV1ClientWrapper: workClient}
	return &ClientHolder{workClientSet: workClientSet}, nil
}

// NewAgentClientHolder returns a ClientHolder for an agent
func NewAgentClientHolder(ctx context.Context, opt *options.GenericClientOptions[*workv1.ManifestWork]) (*ClientHolder, error) {
	agentClient, err := opt.AgentClient(ctx)
	if err != nil {
		return nil, err
	}

	manifestWorkClient := agentclient.NewManifestWorkAgentClient(opt.ClusterName(), opt.WatcherStore(), agentClient)
	workClient := &internal.WorkV1ClientWrapper{ManifestWorkClient: manifestWorkClient}
	workClientSet := &internal.WorkClientSetWrapper{WorkV1ClientWrapper: workClient}
	return &ClientHolder{workClientSet: workClientSet}, nil
}
