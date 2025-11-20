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

type mqttSourceTransport struct {
	MQTTOptions
	protocol          *cloudeventsmqtt.Protocol
	cloudEventsClient cloudevents.Client
	errorChan         chan error
	sourceID          string
	clientID          string
}

// Deprecated: use v2.mqtt.NewSourceOptions instead
func NewSourceOptions(mqttOptions *MQTTOptions, clientID, sourceID string) *options.CloudEventsSourceOptions {
	mqttSourceOptions := &mqttSourceTransport{
		MQTTOptions: *mqttOptions,
		errorChan:   make(chan error),
		sourceID:    sourceID,
		clientID:    clientID,
	}

	return &options.CloudEventsSourceOptions{
		CloudEventsTransport: mqttSourceOptions,
		SourceID:             mqttSourceOptions.sourceID,
	}
}

func (o *mqttSourceTransport) WithContext(ctx context.Context, evtCtx cloudevents.EventContext) (context.Context, error) {
	topic, err := getSourcePubTopic(ctx)
	if err != nil {
		return nil, err
	}

	if topic != nil {
		return cloudeventscontext.WithTopic(ctx, string(*topic)), nil
	}

	pubTopic, err := SourcePubTopic(ctx, &o.MQTTOptions, o.sourceID, evtCtx)
	if err != nil {
		return nil, err
	}

	return cloudeventscontext.WithTopic(ctx, pubTopic), nil
}

func (o *mqttSourceTransport) Connect(ctx context.Context) error {
	subscribe, err := SourceSubscribe(&o.MQTTOptions, o.sourceID)
	if err != nil {
		return err
	}

	protocol, err := o.GetCloudEventsProtocol(
		ctx,
		o.clientID,
		func(err error) {
			o.errorChan <- err
		},
		cloudeventsmqtt.WithPublish(&paho.Publish{QoS: byte(o.PubQoS)}),
		cloudeventsmqtt.WithSubscribe(subscribe),
	)
	if err != nil {
		return err
	}

	o.protocol = protocol
	o.cloudEventsClient, err = cloudevents.NewClient(o.protocol)
	if err != nil {
		return err
	}
	return nil
}

func (o *mqttSourceTransport) Send(ctx context.Context, evt cloudevents.Event) error {
	sendingCtx, err := o.WithContext(ctx, evt.Context)
	if err != nil {
		return err
	}

	if err := o.cloudEventsClient.Send(sendingCtx, evt); cloudevents.IsUndelivered(err) {
		return err
	}
	return nil
}

func (o *mqttSourceTransport) Subscribe(ctx context.Context) error {
	// Subscription is handled by the cloudevents client during receiver startup.
	// No action needed here.
	// TODO: consider implementing native subscription logic in v2 to decouple from
	// the CloudEvents SDK.
	return nil
}

func (o *mqttSourceTransport) Receive(ctx context.Context, fn options.ReceiveHandlerFn) error {
	return o.cloudEventsClient.StartReceiver(ctx, fn)
}

func (o *mqttSourceTransport) Close(ctx context.Context) error {
	return o.protocol.Close(ctx)
}

func (o *mqttSourceTransport) ErrorChan() <-chan error {
	return o.errorChan
}

func SourcePubTopic(ctx context.Context, o *MQTTOptions, sourceID string, evtCtx cloudevents.EventContext) (string, error) {
	ceType := evtCtx.GetType()
	eventType, err := types.ParseCloudEventsType(ceType)
	if err != nil {
		return "", fmt.Errorf("unsupported event type %q, %v", ceType, err)
	}

	clusterNameVal, err := evtCtx.GetExtension(types.ExtensionClusterName)
	if err != nil {
		return "", err
	}

	clusterName, ok := clusterNameVal.(string)
	if !ok {
		return "", fmt.Errorf("clustername extension must be a string, got %T", clusterNameVal)
	}

	if eventType.Action == types.ResyncRequestAction && clusterName == types.ClusterAll {
		// source request to get resources status from all agents
		if len(o.Topics.SourceBroadcast) == 0 {
			return "", fmt.Errorf("the source broadcast topic not set")
		}

		resyncTopic := strings.Replace(o.Topics.SourceBroadcast, "+", sourceID, 1)
		return resyncTopic, nil
	}

	// source publishes spec events or status resync events
	eventsTopic := strings.Replace(o.Topics.SourceEvents, "+", clusterName, 1)
	return eventsTopic, nil
}

func SourceSubscribe(o *MQTTOptions, sourceID string) (*paho.Subscribe, error) {
	topicSource, err := getSourceFromEventsTopic(o.Topics.AgentEvents)
	if err != nil {
		return nil, err
	}

	if topicSource != sourceID {
		return nil, fmt.Errorf("the topic source %q does not match the client sourceID %q",
			topicSource, sourceID)
	}

	subscribe := &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{
				Topic: o.Topics.AgentEvents, QoS: byte(o.SubQoS),
			},
		},
	}

	if len(o.Topics.AgentBroadcast) != 0 {
		// receiving spec resync events from all agents
		subscribe.Subscriptions = append(subscribe.Subscriptions, paho.SubscribeOptions{
			Topic: o.Topics.AgentBroadcast,
			QoS:   byte(o.SubQoS),
		})
	}

	return subscribe, nil
}
