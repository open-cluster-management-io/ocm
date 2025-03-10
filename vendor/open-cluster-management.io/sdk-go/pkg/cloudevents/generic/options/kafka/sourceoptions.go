//go:build kafka

package kafka

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	confluent "github.com/cloudevents/sdk-go/protocol/kafka_confluent/v2"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

type kafkaSourceOptions struct {
	KafkaOptions
	sourceID  string
	errorChan chan error
}

func NewSourceOptions(kafkaOptions *KafkaOptions, sourceID string) *options.CloudEventsSourceOptions {
	sourceOptions := &kafkaSourceOptions{
		KafkaOptions: *kafkaOptions,
		sourceID:     sourceID,
		errorChan:    make(chan error),
	}

	groupID, err := kafkaOptions.ConfigMap.Get("group.id", "")
	if groupID == "" || err != nil {
		_ = kafkaOptions.ConfigMap.SetKey("group.id", sourceID)
	}

	return &options.CloudEventsSourceOptions{
		CloudEventsOptions: sourceOptions,
		SourceID:           sourceID,
	}
}

func (o *kafkaSourceOptions) WithContext(ctx context.Context,
	evtCtx cloudevents.EventContext,
) (context.Context, error) {
	return ctx, nil
}

func (o *kafkaSourceOptions) Protocol(ctx context.Context, dataType types.CloudEventsDataType) (options.CloudEventsProtocol, error) {
	protocol, err := confluent.New(confluent.WithConfigMap(&o.KafkaOptions.ConfigMap),
		confluent.WithReceiverTopics([]string{agentEventsTopic}),
		confluent.WithSenderTopic(sourceEventsTopic),
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
