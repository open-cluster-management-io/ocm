package mqtt

import (
	"context"
	"fmt"

	v2 "github.com/cloudevents/sdk-go/v2"
	"github.com/eclipse/paho.golang/paho"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/mqtt"
)

func NewAgentOptions(opts *mqtt.MQTTOptions, clusterName, agentID string) *options.CloudEventsAgentOptions {
	return &options.CloudEventsAgentOptions{
		CloudEventsTransport: newTransport(
			fmt.Sprintf("%s-client", agentID),
			opts,
			func(ctx context.Context, e v2.Event) (string, error) {
				return mqtt.AgentPubTopic(ctx, opts, clusterName, e.Context)
			},
			func() (*paho.Subscribe, error) {
				return mqtt.AgentSubscribe(opts, clusterName)
			},
		),
		AgentID:     agentID,
		ClusterName: clusterName,
	}
}

func NewSourceOptions(opts *mqtt.MQTTOptions, clientID, sourceID string) *options.CloudEventsSourceOptions {
	return &options.CloudEventsSourceOptions{
		CloudEventsTransport: newTransport(
			clientID,
			opts,
			func(ctx context.Context, e v2.Event) (string, error) {
				return mqtt.SourcePubTopic(ctx, opts, sourceID, e.Context)
			},
			func() (*paho.Subscribe, error) {
				return mqtt.SourceSubscribe(opts, sourceID)
			},
		),
		SourceID: sourceID,
	}
}
