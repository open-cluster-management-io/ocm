package mqtt

import (
	"context"
	"fmt"
	"strings"

	cloudeventsmqtt "github.com/cloudevents/sdk-go/protocol/mqtt_paho/v2"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventscontext "github.com/cloudevents/sdk-go/v2/context"
	"github.com/eclipse/paho.golang/paho"
	"k8s.io/klog/v2"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type mqttAgentOptions struct {
	MQTTOptions
	errorChan   chan error
	clusterName string
	agentID     string
}

func NewAgentOptions(mqttOptions *MQTTOptions, clusterName, agentID string) *options.CloudEventsAgentOptions {
	mqttAgentOptions := &mqttAgentOptions{
		MQTTOptions: *mqttOptions,
		errorChan:   make(chan error),
		clusterName: clusterName,
		agentID:     agentID,
	}

	return &options.CloudEventsAgentOptions{
		CloudEventsOptions: mqttAgentOptions,
		AgentID:            mqttAgentOptions.agentID,
		ClusterName:        mqttAgentOptions.clusterName,
	}
}

func (o *mqttAgentOptions) WithContext(ctx context.Context, evtCtx cloudevents.EventContext) (context.Context, error) {
	topic, err := getAgentPubTopic(ctx)
	if err != nil {
		return nil, err
	}

	if topic != nil {
		return cloudeventscontext.WithTopic(ctx, string(*topic)), nil
	}

	eventType, err := types.ParseCloudEventsType(evtCtx.GetType())
	if err != nil {
		return nil, fmt.Errorf("unsupported event type %s, %v", eventType, err)
	}

	originalSource, err := evtCtx.GetExtension(types.ExtensionOriginalSource)
	if err != nil {
		return nil, err
	}

	// agent request to sync resource spec from all sources
	if eventType.Action == types.ResyncRequestAction && originalSource == types.SourceAll {
		if len(o.Topics.AgentBroadcast) == 0 {
			klog.Warningf("the agent broadcast topic not set, fall back to the agent events topic")

			// TODO after supporting multiple sources, we should list each source
			eventsTopic := replaceLast(o.Topics.AgentEvents, "+", o.clusterName)
			return cloudeventscontext.WithTopic(ctx, eventsTopic), nil
		}

		resyncTopic := strings.Replace(o.Topics.AgentBroadcast, "+", o.clusterName, 1)
		return cloudeventscontext.WithTopic(ctx, resyncTopic), nil
	}

	topicSource, err := getSourceFromEventsTopic(o.Topics.AgentEvents)
	if err != nil {
		return nil, err
	}

	// agent publishes status events or spec resync events
	eventsTopic := replaceLast(o.Topics.AgentEvents, "+", o.clusterName)
	eventsTopic = replaceLast(eventsTopic, "+", topicSource)
	return cloudeventscontext.WithTopic(ctx, eventsTopic), nil
}

func (o *mqttAgentOptions) Protocol(ctx context.Context, dataType types.CloudEventsDataType) (options.CloudEventsProtocol, error) {
	subscribe := &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{
				// TODO support multiple sources, currently the client require the source events topic has a sourceID, in
				// the future, client may need a source list, it will subscribe to each source
				// receiving the sources events
				Topic: replaceLast(o.Topics.SourceEvents, "+", o.clusterName), QoS: byte(o.SubQoS),
			},
		},
	}

	// receiving status resync events from all sources
	if len(o.Topics.SourceBroadcast) != 0 {
		subscribe.Subscriptions = append(subscribe.Subscriptions, paho.SubscribeOptions{
			Topic: o.Topics.SourceBroadcast,
			QoS:   byte(o.SubQoS),
		})
	}

	return o.GetCloudEventsProtocol(
		ctx,
		fmt.Sprintf("%s-client", o.agentID),
		func(err error) {
			o.errorChan <- err
		},
		cloudeventsmqtt.WithPublish(&paho.Publish{QoS: byte(o.PubQoS)}),
		cloudeventsmqtt.WithSubscribe(subscribe),
	)
}

func (o *mqttAgentOptions) ErrorChan() <-chan error {
	return o.errorChan
}
