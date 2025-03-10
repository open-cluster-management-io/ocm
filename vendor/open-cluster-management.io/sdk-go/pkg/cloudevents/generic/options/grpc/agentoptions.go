package grpc

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"

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
	// grpc agent client doesn't need to update topic in the context
	return ctx, nil
}

func (o *grpcAgentOptions) Protocol(ctx context.Context, dataType types.CloudEventsDataType) (options.CloudEventsProtocol, error) {
	receiver, err := o.GetCloudEventsProtocol(
		ctx,
		func(err error) {
			o.errorChan <- err
		},
		protocol.WithSubscribeOption(&protocol.SubscribeOption{
			// TODO: Update this code to determine the subscription source for the agent client.
			// Currently, the grpc agent client is not utilized, and the 'Source' field serves
			// as a placeholder with all the sources.
			Source:      types.SourceAll,
			ClusterName: o.clusterName,
			DataType:    dataType.String(),
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
