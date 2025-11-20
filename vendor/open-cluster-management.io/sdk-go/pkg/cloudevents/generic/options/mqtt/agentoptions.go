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

type mqttAgentTransport struct {
	MQTTOptions
	protocol          *cloudeventsmqtt.Protocol
	cloudEventsClient cloudevents.Client
	errorChan         chan error
	clusterName       string
	agentID           string
}

// Deprecated: use v2.mqtt.NewAgentOptions instead
func NewAgentOptions(mqttOptions *MQTTOptions, clusterName, agentID string) *options.CloudEventsAgentOptions {
	mqttAgentOptions := &mqttAgentTransport{
		MQTTOptions: *mqttOptions,
		errorChan:   make(chan error),
		clusterName: clusterName,
		agentID:     agentID,
	}

	return &options.CloudEventsAgentOptions{
		CloudEventsTransport: mqttAgentOptions,
		AgentID:              mqttAgentOptions.agentID,
		ClusterName:          mqttAgentOptions.clusterName,
	}
}

func (o *mqttAgentTransport) WithContext(ctx context.Context, evtCtx cloudevents.EventContext) (context.Context, error) {
	topic, err := getAgentPubTopic(ctx)
	if err != nil {
		return nil, err
	}

	if topic != nil {
		return cloudeventscontext.WithTopic(ctx, string(*topic)), nil
	}

	pubTopic, err := AgentPubTopic(ctx, &o.MQTTOptions, o.clusterName, evtCtx)
	if err != nil {
		return nil, err
	}

	return cloudeventscontext.WithTopic(ctx, pubTopic), nil
}

func (o *mqttAgentTransport) Connect(ctx context.Context) error {
	subscribe, err := AgentSubscribe(&o.MQTTOptions, o.clusterName)
	if err != nil {
		return err
	}

	protocol, err := o.GetCloudEventsProtocol(
		ctx,
		fmt.Sprintf("%s-client", o.agentID),
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

func (o *mqttAgentTransport) Send(ctx context.Context, evt cloudevents.Event) error {
	sendingCtx, err := o.WithContext(ctx, evt.Context)
	if err != nil {
		return err
	}
	if err := o.cloudEventsClient.Send(sendingCtx, evt); cloudevents.IsUndelivered(err) {
		return err
	}
	return nil
}

func (o *mqttAgentTransport) Subscribe(ctx context.Context) error {
	// Subscription is handled by the cloudevents client during receiver startup.
	// No action needed here.
	// TODO: consider implementing native subscription logic in v2 to decouple from
	// the CloudEvents SDK.
	return nil
}

func (o *mqttAgentTransport) Receive(ctx context.Context, fn options.ReceiveHandlerFn) error {
	return o.cloudEventsClient.StartReceiver(ctx, fn)
}

func (o *mqttAgentTransport) Close(ctx context.Context) error {
	return o.protocol.Close(ctx)
}

func (o *mqttAgentTransport) ErrorChan() <-chan error {
	return o.errorChan
}

func AgentPubTopic(ctx context.Context, o *MQTTOptions, clusterName string, evtCtx cloudevents.EventContext) (string, error) {
	logger := klog.FromContext(ctx)

	ceType := evtCtx.GetType()
	eventType, err := types.ParseCloudEventsType(ceType)
	if err != nil {
		return "", fmt.Errorf("unsupported event type %q, %v", ceType, err)
	}

	originalSourceVal, err := evtCtx.GetExtension(types.ExtensionOriginalSource)
	if err != nil {
		return "", err
	}

	originalSource, ok := originalSourceVal.(string)
	if !ok {
		return "", fmt.Errorf("originalsource extension must be a string, got %T", originalSourceVal)
	}

	// agent request to sync resource spec from all sources
	if eventType.Action == types.ResyncRequestAction && originalSource == types.SourceAll {
		if len(o.Topics.AgentBroadcast) == 0 {
			logger.Info("the agent broadcast topic not set, fall back to the agent events topic")

			// TODO after supporting multiple sources, we should list each source
			return replaceLast(o.Topics.AgentEvents, "+", clusterName), nil
		}

		return strings.Replace(o.Topics.AgentBroadcast, "+", clusterName, 1), nil
	}

	topicSource, err := getSourceFromEventsTopic(o.Topics.AgentEvents)
	if err != nil {
		return "", err
	}

	// agent publishes status events or spec resync events
	eventsTopic := replaceLast(o.Topics.AgentEvents, "+", clusterName)
	return replaceLast(eventsTopic, "+", topicSource), nil
}

func AgentSubscribe(o *MQTTOptions, clusterName string) (*paho.Subscribe, error) {
	subscribe := &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{
				// TODO support multiple sources, currently the client require the source events topic has a sourceID, in
				// the future, client may need a source list, it will subscribe to each source
				// receiving the sources events
				Topic: replaceLast(o.Topics.SourceEvents, "+", clusterName), QoS: byte(o.SubQoS),
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

	return subscribe, nil
}
