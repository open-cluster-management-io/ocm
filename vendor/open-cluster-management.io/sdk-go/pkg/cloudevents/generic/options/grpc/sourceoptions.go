package grpc

import (
	"context"
	"fmt"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventscontext "github.com/cloudevents/sdk-go/v2/context"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protocol"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type gRPCSourceOptions struct {
	GRPCOptions
	errorChan chan error // grpc client connection doesn't have error channel, it will handle reconnecting automatically
	sourceID  string
}

func NewSourceOptions(gRPCOptions *GRPCOptions, sourceID string) *options.CloudEventsSourceOptions {
	return &options.CloudEventsSourceOptions{
		CloudEventsOptions: &gRPCSourceOptions{
			GRPCOptions: *gRPCOptions,
			errorChan:   make(chan error),
			sourceID:    sourceID,
		},
		SourceID: sourceID,
	}
}

func (o *gRPCSourceOptions) WithContext(ctx context.Context, evtCtx cloudevents.EventContext) (context.Context, error) {
	eventType, err := types.ParseCloudEventsType(evtCtx.GetType())
	if err != nil {
		return nil, fmt.Errorf("unsupported event type %s, %v", eventType, err)
	}

	if eventType.Action == types.ResyncRequestAction {
		// source publishes event to status resync topic to request to get resources status from all clusters
		return cloudeventscontext.WithTopic(ctx, strings.Replace(StatusResyncTopic, "+", o.sourceID, -1)), nil
	}

	clusterName, err := evtCtx.GetExtension(types.ExtensionClusterName)
	if err != nil {
		return nil, err
	}

	// source publishes event to spec topic to send the resource spec to a specified cluster
	specTopic := strings.Replace(SpecTopic, "+", o.sourceID, 1)
	specTopic = strings.Replace(specTopic, "+", fmt.Sprintf("%s", clusterName), -1)
	return cloudeventscontext.WithTopic(ctx, specTopic), nil
}

func (o *gRPCSourceOptions) Client(ctx context.Context) (cloudevents.Client, error) {
	receiver, err := o.GetCloudEventsClient(
		ctx,
		func(err error) {
			o.errorChan <- err
		},
		protocol.WithPublishOption(&protocol.PublishOption{}),
		protocol.WithSubscribeOption(&protocol.SubscribeOption{
			Topics: []string{
				strings.Replace(StatusTopic, "+", o.sourceID, 1), // receiving the resources status from agents with status topic
				SpecResyncTopic, // receiving the resources spec resync request from agents with spec resync topic
			},
		}),
	)
	if err != nil {
		return nil, err
	}
	return receiver, nil
}

func (o *gRPCSourceOptions) ErrorChan() <-chan error {
	return o.errorChan
}
