package grpc

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protocol"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type grpcAgentTransport struct {
	GRPCOptions
	errorChan         chan error
	clusterName       string
	protocol          *protocol.Protocol
	cloudEventsClient cloudevents.Client
	dataType          types.CloudEventsDataType
}

func NewAgentOptions(grpcOptions *GRPCOptions,
	clusterName, agentID string, dataType types.CloudEventsDataType) *options.CloudEventsAgentOptions {
	return &options.CloudEventsAgentOptions{
		CloudEventsTransport: &grpcAgentTransport{
			GRPCOptions: *grpcOptions,
			errorChan:   make(chan error),
			clusterName: clusterName,
			dataType:    dataType,
		},
		AgentID:     agentID,
		ClusterName: clusterName,
	}
}

func (o *grpcAgentTransport) Connect(ctx context.Context) error {
	logger := klog.FromContext(ctx)
	opts := []protocol.Option{
		protocol.WithSubscribeOption(&protocol.SubscribeOption{
			// TODO: Update this code to determine the subscription source for the agent client.
			// Currently, the grpc agent client is not utilized, and the 'Source' field serves
			// as a placeholder with all the sources.
			Source:      types.SourceAll,
			ClusterName: o.clusterName,
			DataType:    o.dataType.String(),
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

func (o *grpcAgentTransport) Send(ctx context.Context, evt cloudevents.Event) error {
	if err := o.cloudEventsClient.Send(ctx, evt); cloudevents.IsUndelivered(err) {
		return err
	}
	return nil
}

func (o *grpcAgentTransport) Receive(ctx context.Context, fn options.ReceiveHandlerFn) error {
	return o.cloudEventsClient.StartReceiver(ctx, fn)
}

func (o *grpcAgentTransport) Close(ctx context.Context) error {
	return o.protocol.Close(ctx)
}

func (o *grpcAgentTransport) ErrorChan() <-chan error {
	return o.errorChan
}
