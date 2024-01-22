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
	eventType, err := types.ParseCloudEventsType(evtCtx.GetType())
	if err != nil {
		return nil, fmt.Errorf("unsupported event type %s, %v", eventType, err)
	}

	if eventType.Action == types.ResyncRequestAction {
		// agent publishes event to spec resync topic to request to get resources spec from all sources
		topic := strings.Replace(o.Topics.SpecResync, "+", o.clusterName, -1)
		return cloudeventscontext.WithTopic(ctx, topic), nil
	}

	// agent publishes event to status topic to send the resource status from a specified cluster
	originalSource, err := evtCtx.GetExtension(types.ExtensionOriginalSource)
	if err != nil {
		return nil, err
	}

	statusTopic := strings.Replace(o.Topics.Status, "+", fmt.Sprintf("%s", originalSource), 1)
	statusTopic = strings.Replace(statusTopic, "+", o.clusterName, -1)
	return cloudeventscontext.WithTopic(ctx, statusTopic), nil
}

func (o *mqttAgentOptions) Client(ctx context.Context) (cloudevents.Client, error) {
	receiver, err := o.GetCloudEventsClient(
		ctx,
		fmt.Sprintf("%s-client", o.agentID),
		func(err error) {
			o.errorChan <- err
		},
		cloudeventsmqtt.WithPublish(&paho.Publish{QoS: byte(o.PubQoS)}),
		cloudeventsmqtt.WithSubscribe(
			&paho.Subscribe{
				Subscriptions: map[string]paho.SubscribeOptions{
					// receiving the resources spec from sources with spec topic
					replaceNth(o.Topics.Spec, "+", o.clusterName, 2): {QoS: byte(o.SubQoS)},
					// receiving the resources status resync request from sources with status resync topic
					o.Topics.StatusResync: {QoS: byte(o.SubQoS)},
				},
			},
		),
	)
	if err != nil {
		return nil, err
	}
	return receiver, nil
}

func (o *mqttAgentOptions) ErrorChan() <-chan error {
	return o.errorChan
}
