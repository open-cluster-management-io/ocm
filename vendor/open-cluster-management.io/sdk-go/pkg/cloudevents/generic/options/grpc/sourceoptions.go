package grpc

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protocol"
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
	// grpc source client doesn't need to update topic in the context
	return ctx, nil
}

func (o *gRPCSourceOptions) Client(ctx context.Context) (cloudevents.Client, error) {
	receiver, err := o.GetCloudEventsClient(
		ctx,
		func(err error) {
			o.errorChan <- err
		},
		protocol.WithSubscribeOption(&protocol.SubscribeOption{
			Source: o.sourceID,
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
