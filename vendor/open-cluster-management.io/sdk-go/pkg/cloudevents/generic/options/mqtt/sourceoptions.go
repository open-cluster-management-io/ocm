package mqtt

import (
	"context"
	"fmt"
	"strings"

	cloudeventsmqtt "github.com/cloudevents/sdk-go/protocol/mqtt_paho/v2"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventscontext "github.com/cloudevents/sdk-go/v2/context"
	"github.com/eclipse/paho.golang/paho"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type mqttSourceOptions struct {
	MQTTOptions
	errorChan chan error
	sourceID  string
	clientID  string
}

func NewSourceOptions(mqttOptions *MQTTOptions, clientID, sourceID string) *options.CloudEventsSourceOptions {
	mqttSourceOptions := &mqttSourceOptions{
		MQTTOptions: *mqttOptions,
		errorChan:   make(chan error),
		sourceID:    sourceID,
		clientID:    clientID,
	}

	return &options.CloudEventsSourceOptions{
		CloudEventsOptions: mqttSourceOptions,
		SourceID:           mqttSourceOptions.sourceID,
	}
}

func (o *mqttSourceOptions) WithContext(ctx context.Context, evtCtx cloudevents.EventContext) (context.Context, error) {
	eventType, err := types.ParseCloudEventsType(evtCtx.GetType())
	if err != nil {
		return nil, fmt.Errorf("unsupported event type %s, %v", eventType, err)
	}

	clusterName, err := evtCtx.GetExtension(types.ExtensionClusterName)
	if err != nil {
		return nil, err
	}

	if eventType.Action == types.ResyncRequestAction && clusterName == types.ClusterAll {
		// source request to get resources status from all sources
		if len(o.Topics.SourceBroadcast) == 0 {
			return nil, fmt.Errorf("the source wild card resync topic not set")
		}

		resyncTopic := strings.Replace(o.Topics.SourceBroadcast, "+", o.sourceID, 1)
		return cloudeventscontext.WithTopic(ctx, resyncTopic), nil
	}

	// source publishes spec events or status resync events
	eventsTopic := strings.Replace(o.Topics.SourceEvents, "+", fmt.Sprintf("%s", clusterName), 1)
	return cloudeventscontext.WithTopic(ctx, eventsTopic), nil
}

func (o *mqttSourceOptions) Client(ctx context.Context) (cloudevents.Client, error) {
	topicSource, err := getSourceFromEventsTopic(o.Topics.AgentEvents)
	if err != nil {
		return nil, err
	}

	if topicSource != o.sourceID {
		return nil, fmt.Errorf("the topic source %q does not match with the client sourceID %q",
			o.Topics.AgentEvents, o.sourceID)
	}

	subscribe := &paho.Subscribe{
		Subscriptions: map[string]paho.SubscribeOptions{
			// receiving the agent events
			o.Topics.AgentEvents: {QoS: byte(o.SubQoS)},
		},
	}

	if len(o.Topics.AgentBroadcast) != 0 {
		// receiving spec resync events from all agents
		subscribe.Subscriptions[o.Topics.AgentBroadcast] = paho.SubscribeOptions{QoS: byte(o.SubQoS)}
	}

	receiver, err := o.GetCloudEventsClient(
		ctx,
		o.clientID,
		func(err error) {
			o.errorChan <- err
		},
		cloudeventsmqtt.WithPublish(&paho.Publish{QoS: byte(o.PubQoS)}),
		cloudeventsmqtt.WithSubscribe(subscribe),
	)
	if err != nil {
		return nil, err
	}
	return receiver, nil
}

func (o *mqttSourceOptions) ErrorChan() <-chan error {
	return o.errorChan
}
