package grpc

import (
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

func NewAgentOptions(grpcOptions *grpc.GRPCOptions,
	clusterName, agentID string, dataType types.CloudEventsDataType) *options.CloudEventsAgentOptions {
	return &options.CloudEventsAgentOptions{
		CloudEventsTransport: newTransport(grpcOptions, func() *pbv1.SubscriptionRequest {
			return &pbv1.SubscriptionRequest{
				// TODO: Update this code to determine the subscription source for the agent client.
				// Currently, the grpc agent client is not utilized, and the 'Source' field serves
				// as a placeholder with all the sources.
				Source:      types.SourceAll,
				ClusterName: clusterName,
				DataType:    dataType.String(),
			}
		}),
		AgentID:     agentID,
		ClusterName: clusterName,
	}
}

func NewSourceOptions(gRPCOptions *grpc.GRPCOptions,
	sourceID string, dataType types.CloudEventsDataType) *options.CloudEventsSourceOptions {
	return &options.CloudEventsSourceOptions{
		CloudEventsTransport: newTransport(gRPCOptions, func() *pbv1.SubscriptionRequest {
			return &pbv1.SubscriptionRequest{
				Source:   sourceID,
				DataType: dataType.String(),
			}
		}),
		SourceID: sourceID,
	}
}
