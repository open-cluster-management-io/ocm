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

	if eventType.Action == types.ResyncRequestAction {
		// source publishes event to status resync topic to request to get resources status from all clusters
		return cloudeventscontext.WithTopic(ctx, strings.Replace(o.Topics.StatusResync, "+", o.sourceID, -1)), nil
	}

	clusterName, err := evtCtx.GetExtension(types.ExtensionClusterName)
	if err != nil {
		return nil, err
	}

	// source publishes event to spec topic to send the resource spec to a specified cluster
	specTopic := strings.Replace(o.Topics.Spec, "+", o.sourceID, 1)
	specTopic = strings.Replace(specTopic, "+", fmt.Sprintf("%s", clusterName), -1)
	return cloudeventscontext.WithTopic(ctx, specTopic), nil
}

func (o *mqttSourceOptions) Client(ctx context.Context) (cloudevents.Client, error) {
	receiver, err := o.GetCloudEventsClient(
		ctx,
		o.clientID,
		func(err error) {
			o.errorChan <- err
		},
		cloudeventsmqtt.WithPublish(&paho.Publish{QoS: byte(o.PubQoS)}),
		cloudeventsmqtt.WithSubscribe(
			&paho.Subscribe{
				Subscriptions: map[string]paho.SubscribeOptions{
					// receiving the resources status from agents with status topic
					strings.Replace(o.Topics.Status, "+", o.sourceID, 1): {QoS: byte(o.SubQoS)},
					// receiving the resources spec resync request from agents with spec resync topic
					o.Topics.SpecResync: {QoS: byte(o.SubQoS)},
				},
			},
		),
	)
	if err != nil {
		return nil, err
	}
	return receiver, nil
}

func (o *mqttSourceOptions) ErrorChan() <-chan error {
	return o.errorChan
}
