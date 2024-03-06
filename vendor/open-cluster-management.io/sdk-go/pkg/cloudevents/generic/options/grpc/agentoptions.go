package grpc

import (
	"context"
	"fmt"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cecontext "github.com/cloudevents/sdk-go/v2/context"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protocol"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type grpcAgentOptions struct {
	GRPCOptions
	errorChan   chan error // grpc client connection doesn't have error channel, it will handle reconnecting automatically
	clusterName string
}

func NewAgentOptions(grpcOptions *GRPCOptions, clusterName, agentID string) *options.CloudEventsAgentOptions {
	return &options.CloudEventsAgentOptions{
		CloudEventsOptions: &grpcAgentOptions{
			GRPCOptions: *grpcOptions,
			errorChan:   make(chan error),
			clusterName: clusterName,
		},
		AgentID:     agentID,
		ClusterName: clusterName,
	}
}

func (o *grpcAgentOptions) WithContext(ctx context.Context, evtCtx cloudevents.EventContext) (context.Context, error) {
	eventType, err := types.ParseCloudEventsType(evtCtx.GetType())
	if err != nil {
		return nil, fmt.Errorf("unsupported event type %s, %v", eventType, err)
	}

	if eventType.Action == types.ResyncRequestAction {
		// agent publishes event to spec resync topic to request to get resources spec from all sources
		topic := strings.Replace(SpecResyncTopic, "+", o.clusterName, -1)
		return cecontext.WithTopic(ctx, topic), nil
	}

	// agent publishes event to status topic to send the resource status from a specified cluster
	originalSource, err := evtCtx.GetExtension(types.ExtensionOriginalSource)
	if err != nil {
		return nil, err
	}

	statusTopic := strings.Replace(StatusTopic, "+", fmt.Sprintf("%s", originalSource), 1)
	statusTopic = strings.Replace(statusTopic, "+", o.clusterName, -1)
	return cecontext.WithTopic(ctx, statusTopic), nil
}

func (o *grpcAgentOptions) Client(ctx context.Context) (cloudevents.Client, error) {
	receiver, err := o.GetCloudEventsClient(
		ctx,
		func(err error) {
			o.errorChan <- err
		},
		protocol.WithPublishOption(&protocol.PublishOption{}),
		protocol.WithSubscribeOption(&protocol.SubscribeOption{
			Topics: []string{
				replaceNth(SpecTopic, "+", o.clusterName, 2), // receiving the resources spec from sources with spec topic
				StatusResyncTopic, // receiving the resources status resync request from sources with status resync topic
			},
		}),
	)
	if err != nil {
		return nil, err
	}
	return receiver, nil
}

func (o *grpcAgentOptions) ErrorChan() <-chan error {
	return o.errorChan
}
