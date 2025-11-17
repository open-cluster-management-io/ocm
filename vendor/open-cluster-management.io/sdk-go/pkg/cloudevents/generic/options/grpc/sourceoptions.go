package grpc

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protocol"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type gRPCSourceTransport struct {
	GRPCOptions
	errorChan         chan error
	sourceID          string
	protocol          *protocol.Protocol
	cloudEventsClient cloudevents.Client
	dataType          types.CloudEventsDataType
}

func NewSourceOptions(gRPCOptions *GRPCOptions,
	sourceID string, dataType types.CloudEventsDataType) *options.CloudEventsSourceOptions {
	return &options.CloudEventsSourceOptions{
		CloudEventsTransport: &gRPCSourceTransport{
			GRPCOptions: *gRPCOptions,
			errorChan:   make(chan error),
			sourceID:    sourceID,
			dataType:    dataType,
		},
		SourceID: sourceID,
	}
}

func (o *gRPCSourceTransport) Connect(ctx context.Context) error {
	logger := klog.FromContext(ctx)
	opts := []protocol.Option{
		protocol.WithSubscribeOption(&protocol.SubscribeOption{
			Source:   o.sourceID,
			DataType: o.dataType.String(),
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
				logger.Error(err, "no error channel available to report error")
			}
		},
		opts...,
	)
	if err != nil {
		return err
	}

	o.protocol = receiver
	o.cloudEventsClient, err = cloudevents.NewClient(o.protocol)
	if err != nil {
		return err
	}

	return nil
}

func (o *gRPCSourceTransport) Send(ctx context.Context, evt cloudevents.Event) error {
	if err := o.cloudEventsClient.Send(ctx, evt); cloudevents.IsUndelivered(err) {
		return err
	}
	return nil
}

func (o *gRPCSourceTransport) Receive(ctx context.Context, fn options.ReceiveHandlerFn) error {
	return o.cloudEventsClient.StartReceiver(ctx, fn)
}

func (o *gRPCSourceTransport) Close(ctx context.Context) error {
	return o.protocol.Close(ctx)
}

func (o *gRPCSourceTransport) ErrorChan() <-chan error {
	return o.errorChan
}
