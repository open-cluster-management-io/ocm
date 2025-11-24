package util

import (
	"fmt"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/v2/pubsub"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

func NewPubSubSourceOptions(endpoint, projectID, sourceID string) *pubsub.PubSubOptions {
	return newPubSubOptions(endpoint, projectID, types.Topics{
		SourceEvents:    fmt.Sprintf("projects/%s/topics/sourceevents", projectID),
		SourceBroadcast: fmt.Sprintf("projects/%s/topics/sourcebroadcast", projectID),
	}, types.Subscriptions{
		AgentEvents:    fmt.Sprintf("projects/%s/subscriptions/agentevents-%s", projectID, sourceID),
		AgentBroadcast: fmt.Sprintf("projects/%s/subscriptions/agentbroadcast-%s", projectID, sourceID),
	})
}

func NewPubSubAgentOptions(endpoint, projectID, clusterName string) *pubsub.PubSubOptions {
	return newPubSubOptions(endpoint, projectID, types.Topics{
		AgentEvents:    fmt.Sprintf("projects/%s/topics/agentevents", projectID),
		AgentBroadcast: fmt.Sprintf("projects/%s/topics/agentbroadcast", projectID),
	}, types.Subscriptions{
		SourceEvents:    fmt.Sprintf("projects/%s/subscriptions/sourceevents-%s", projectID, clusterName),
		SourceBroadcast: fmt.Sprintf("projects/%s/subscriptions/sourcebroadcast-%s", projectID, clusterName),
	})
}

func newPubSubOptions(endpoint, projectID string, topics types.Topics, subscriptions types.Subscriptions) *pubsub.PubSubOptions {
	return &pubsub.PubSubOptions{
		Endpoint:      endpoint,
		ProjectID:     projectID,
		Topics:        topics,
		Subscriptions: subscriptions,
	}
}
