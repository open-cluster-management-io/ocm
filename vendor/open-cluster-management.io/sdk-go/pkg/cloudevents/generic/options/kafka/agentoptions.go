//go:build kafka

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
	eventType, err := types.ParseCloudEventsType(evtCtx.GetType())
	if err != nil {
		return nil, err
	}

	// agent publishes event to status topic to send the resource status from a specified cluster
	originalSource, err := evtCtx.GetExtension(types.ExtensionOriginalSource)
	if err != nil {
		return nil, err
	}

	if eventType.Action == types.ResyncRequestAction && originalSource == types.SourceAll {
		// TODO support multiple sources, agent may need a source list instead of the broadcast
		topic := strings.Replace(agentBroadcastTopic, "*", o.clusterName, 1)
		return confluent.WithMessageKey(cloudeventscontext.WithTopic(ctx, topic), o.clusterName), nil
	}

	topic := strings.Replace(agentEventsTopic, "*", fmt.Sprintf("%s", originalSource), 1)
	topic = strings.Replace(topic, "*", o.clusterName, 1)
	messageKey := fmt.Sprintf("%s@%s", originalSource, o.clusterName)
	return confluent.WithMessageKey(cloudeventscontext.WithTopic(ctx, topic), messageKey), nil
}

func (o *kafkaAgentOptions) Protocol(ctx context.Context) (options.CloudEventsProtocol, error) {
	protocol, err := confluent.New(confluent.WithConfigMap(&o.KafkaOptions.ConfigMap),
		confluent.WithReceiverTopics([]string{
			fmt.Sprintf("^%s", replaceLast(sourceEventsTopic, "*", o.clusterName)),
			fmt.Sprintf("^%s", sourceBroadcastTopic),
		}),
		confluent.WithSenderTopic("agentevents"),
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

func replaceLast(str, old, new string) string {
	last := strings.LastIndex(str, old)
	if last == -1 {
		return str
	}
	return str[:last] + new + str[last+len(old):]
}
