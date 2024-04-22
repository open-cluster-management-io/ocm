package kafka

import (
	"context"
	"fmt"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	cloudeventscontext "github.com/cloudevents/sdk-go/v2/context"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	confluent "github.com/cloudevents/sdk-go/protocol/kafka_confluent/v2"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type kafkaSourceOptions struct {
	configMap *kafka.ConfigMap
	sourceID  string
	errorChan chan error
}

func NewSourceOptions(configMap *kafka.ConfigMap, sourceID string) *options.CloudEventsSourceOptions {
	sourceOptions := &kafkaSourceOptions{
		configMap: configMap,
		sourceID:  sourceID,
		errorChan: make(chan error),
	}

	groupID, err := configMap.Get("group.id", "")
	if groupID == "" || err != nil {
		_ = configMap.SetKey("group.id", sourceID)
	}

	return &options.CloudEventsSourceOptions{
		CloudEventsOptions: sourceOptions,
		SourceID:           sourceID,
	}
}

func (o *kafkaSourceOptions) WithContext(ctx context.Context,
	evtCtx cloudevents.EventContext,
) (context.Context, error) {
	eventType, err := types.ParseCloudEventsType(evtCtx.GetType())
	if err != nil {
		return nil, err
	}

	clusterName, err := evtCtx.GetExtension(types.ExtensionClusterName)
	if err != nil {
		return nil, err
	}

	if eventType.Action == types.ResyncRequestAction && clusterName == types.ClusterAll {
		// source request to get resources status from all agents
		topic := strings.Replace(sourceBroadcastTopic, "*", o.sourceID, 1)
		return confluent.WithMessageKey(cloudeventscontext.WithTopic(ctx, topic), o.sourceID), nil
	}

	// source publishes event to source topic to send the resource spec to a specified cluster
	messageKey := fmt.Sprintf("%s@%s", o.sourceID, clusterName)
	topic := strings.Replace(sourceEventsTopic, "*", o.sourceID, 1)
	topic = strings.Replace(topic, "*", fmt.Sprintf("%s", clusterName), 1)
	return confluent.WithMessageKey(cloudeventscontext.WithTopic(ctx, topic), messageKey), nil
}

func (o *kafkaSourceOptions) Protocol(ctx context.Context) (options.CloudEventsProtocol, error) {
	protocol, err := confluent.New(confluent.WithConfigMap(o.configMap),
		confluent.WithReceiverTopics([]string{
			fmt.Sprintf("^%s", strings.Replace(agentEventsTopic, "*", o.sourceID, 1)),
			fmt.Sprintf("^%s", agentBroadcastTopic),
		}),
		confluent.WithSenderTopic("sourceevents"),
		confluent.WithErrorHandler(func(ctx context.Context, err kafka.Error) {
			o.errorChan <- err
		}))
	if err != nil {
		return nil, err
	}
	producerEvents, _ := protocol.Events()
	handleProduceEvents(producerEvents, o.errorChan)
	return protocol, nil
}

func (o *kafkaSourceOptions) ErrorChan() <-chan error {
	return o.errorChan
}
