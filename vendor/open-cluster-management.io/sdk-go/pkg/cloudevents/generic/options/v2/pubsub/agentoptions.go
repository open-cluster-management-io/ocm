package pubsub

import (
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
)

// NewAgentOptions creates a new CloudEventsAgentOptions for Pub/Sub.
func NewAgentOptions(pubsubOptions *PubSubOptions,
	clusterName, agentID string) *options.CloudEventsAgentOptions {
	return &options.CloudEventsAgentOptions{
		CloudEventsTransport: &pubsubTransport{
			PubSubOptions: *pubsubOptions,
			clusterName:   clusterName,
			errorChan:     make(chan error),
		},
		AgentID:     agentID,
		ClusterName: clusterName,
	}
}
