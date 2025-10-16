package grpc

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"k8s.io/klog/v2"

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
	// grpc source client doesn't need to update topic in the context
	return ctx, nil
}

func (o *gRPCSourceOptions) Protocol(ctx context.Context, dataType types.CloudEventsDataType) (options.CloudEventsProtocol, error) {
	opts := []protocol.Option{
		protocol.WithSubscribeOption(&protocol.SubscribeOption{
			Source:   o.sourceID,
			DataType: dataType.String(),
		}),
		protocol.WithReconnectErrorChan(o.errorChan),
	}

	if o.ServerHealthinessTimeout != nil {
		opts = append(opts, protocol.WithServerHealthinessTimeout(o.ServerHealthinessTimeout))
	}

	receiver, err := o.GetCloudEventsProtocol(
		ctx,
		func(err error) {
			select {
			case o.errorChan <- err:
			default:
				klog.Errorf("no error channel available to report error: %v", err)
			}
		},
		opts...,
	)
	if err != nil {
		return nil, err
	}
	return receiver, nil
}

func (o *gRPCSourceOptions) ErrorChan() <-chan error {
	return o.errorChan
}
