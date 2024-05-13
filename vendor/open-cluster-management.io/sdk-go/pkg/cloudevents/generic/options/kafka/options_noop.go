//go:build !kafka

// This is the dummy code to pass the compile when Kafka is not enabled.
// Cannot enable Kafka by default due to confluent-kafka-go not supporting cross-compilation.
// Try adding -tags=kafka to build when you need Kafka.
package kafka

import (
	"fmt"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
)

type KafkaOptions struct {
	ConfigMap map[string]interface{}
}

func NewSourceOptions(configMap *KafkaOptions, sourceID string) *options.CloudEventsSourceOptions {
	return nil
}

func NewAgentOptions(configMap *KafkaOptions, clusterName, agentID string) *options.CloudEventsAgentOptions {
	return nil
}

func BuildKafkaOptionsFromFlags(configPath string) (*KafkaOptions, error) {
	return nil, fmt.Errorf("try adding -tags=kafka to build")
}
