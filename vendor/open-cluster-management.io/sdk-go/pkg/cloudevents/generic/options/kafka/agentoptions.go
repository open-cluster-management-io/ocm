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

type kafkaAgentOptions struct {
	KafkaOptions
	clusterName string
	agentID     string
	errorChan   chan error
}

func NewAgentOptions(kafkaOptions *KafkaOptions, clusterName, agentID string) *options.CloudEventsAgentOptions {
	kafkaAgentOptions := &kafkaAgentOptions{
		KafkaOptions: *kafkaOptions,
		clusterName:  clusterName,
		agentID:      agentID,
		errorChan:    make(chan error),
	}

	groupID, err := kafkaOptions.ConfigMap.Get("group.id", "")
	if groupID == "" || err != nil {
		_ = kafkaOptions.ConfigMap.SetKey("group.id", agentID)
	}

	return &options.CloudEventsAgentOptions{
		CloudEventsOptions: kafkaAgentOptions,
		AgentID:            agentID,
		ClusterName:        clusterName,
	}
}

// encode the source and agent to the message key
func (o *kafkaAgentOptions) WithContext(ctx context.Context, evtCtx cloudevents.EventContext) (context.Context, error) {
	return ctx, nil
}

func (o *kafkaAgentOptions) Protocol(ctx context.Context, dataType types.CloudEventsDataType) (options.CloudEventsProtocol, error) {
	protocol, err := confluent.New(confluent.WithConfigMap(&o.KafkaOptions.ConfigMap),
		confluent.WithReceiverTopics([]string{sourceEventsTopic}),
		confluent.WithSenderTopic(agentEventsTopic),
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

func (o *kafkaAgentOptions) ErrorChan() <-chan error {
	return o.errorChan
}
